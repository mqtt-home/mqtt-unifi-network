package unifi

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mqtt-home/mqtt-unifi-network/config"
)

// Tracker turns a sequence of controller snapshots into stable presence state.
//
// The only subtle part is the away delay. A phone that idles powers its radio
// down and disappears from `stat/sta` for minutes at a time; publishing that
// verbatim produces a false "left the house" every night. So: appearing in the
// client list marks a device online immediately, and disappearing only marks it
// offline once it has been missing for longer than AwayDelay.
type Tracker struct {
	awayDelay time.Duration
	floors    map[string]string // normalized AP mac OR lowercased AP name -> floor
	devices   []trackedDevice

	mu    sync.RWMutex
	state map[string]*deviceState // slug -> state
	snap  Snapshot
}

type trackedDevice struct {
	name string
	slug string
	mac  string
}

type deviceState struct {
	online   bool
	lastSeen time.Time // last time the controller reported it associated
	since    time.Time // when the current online/offline value was adopted
	last     APIClient
	assoc    bool
	apName   string
}

func NewTracker(cfg config.UniFiConfig) *Tracker {
	t := &Tracker{
		awayDelay: time.Duration(cfg.AwayDelay) * time.Second,
		floors:    make(map[string]string, len(cfg.Floors)),
		state:     make(map[string]*deviceState),
	}

	// Floor keys may be an AP MAC or an AP name; normalize both ways so a
	// lookup can try either without the caller knowing which was configured.
	for key, floor := range cfg.Floors {
		t.floors[NormalizeMAC(key)] = floor
		t.floors[strings.ToLower(strings.TrimSpace(key))] = floor
	}

	for _, d := range cfg.Devices {
		slug := d.Slug
		if slug == "" {
			slug = Slugify(d.Name)
		}
		t.devices = append(t.devices, trackedDevice{
			name: d.Name,
			slug: slug,
			mac:  NormalizeMAC(d.MAC),
		})
		t.state[slug] = &deviceState{}
	}

	// Seed the snapshot so the API and the UI show the configured devices as
	// offline from the first request, rather than an empty list, even if the
	// controller cannot be reached at startup.
	initial := make([]ClientState, 0, len(t.devices))
	for _, dev := range t.devices {
		initial = append(initial, t.buildClientState(dev, t.state[dev.slug]))
	}
	sort.Slice(initial, func(i, j int) bool { return initial[i].Slug < initial[j].Slug })
	t.snap = Snapshot{
		Clients: initial,
		Floors:  map[string]FloorState{},
	}

	return t
}

// Update folds one controller poll into the tracked state and returns the new
// snapshot. `aps` is MAC -> AP name; pass nil if the device list is unavailable
// (floors then fall back to matching on the raw AP MAC).
func (t *Tracker) Update(clients []APIClient, aps map[string]string, now time.Time) Snapshot {
	byMAC := make(map[string]APIClient, len(clients))
	for _, c := range clients {
		byMAC[NormalizeMAC(c.MAC)] = c
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	for _, dev := range t.devices {
		st := t.state[dev.slug]

		if api, found := byMAC[dev.mac]; found {
			st.assoc = true
			st.last = api
			st.lastSeen = now
			st.apName = aps[NormalizeMAC(api.APMac)]
			if !st.online {
				st.online = true
				st.since = now
			}
		} else {
			st.assoc = false
			// Missing from the list. Stay online until the away delay elapses.
			if st.online && now.Sub(st.lastSeen) > t.awayDelay {
				st.online = false
				st.since = now
			}
			if st.since.IsZero() {
				// Never seen at all, not even by Seed — offline from the start.
				st.since = now
			}
		}
	}

	return t.buildSnapshotLocked(now, true)
}

func (t *Tracker) buildClientState(dev trackedDevice, st *deviceState) ClientState {
	cs := ClientState{
		Name:   dev.name,
		Slug:   dev.slug,
		MAC:    dev.mac,
		Online: st.online,
		Assoc:  st.assoc,
	}

	if !st.lastSeen.IsZero() {
		cs.LastSeen = st.lastSeen.UTC().Format(time.RFC3339)
	}
	if !st.since.IsZero() {
		cs.Since = st.since.UTC().Format(time.RFC3339)
	}

	// Radio details only make sense while the client is actually associated;
	// keeping the last known RSSI around would read as live data in the UI.
	if st.assoc {
		cs.AP = st.apName
		cs.APMac = NormalizeMAC(st.last.APMac)
		cs.RSSI = st.last.RSSI
		cs.Signal = st.last.Signal
		cs.ESSID = st.last.ESSID
		cs.IP = st.last.IP
		cs.Wired = st.last.IsWired
		cs.Floor = t.floorFor(st.last.APMac, st.apName)
	}

	return cs
}

// floorFor resolves the floor label for an AP, trying its MAC first and then
// its configured name.
func (t *Tracker) floorFor(apMAC, apName string) string {
	if apMAC != "" {
		if floor, ok := t.floors[NormalizeMAC(apMAC)]; ok {
			return floor
		}
	}
	if apName != "" {
		if floor, ok := t.floors[strings.ToLower(strings.TrimSpace(apName))]; ok {
			return floor
		}
	}
	return ""
}

// Seed adopts the controller's own last-seen times before the first poll.
//
// Without it a restart is indistinguishable from everyone leaving: stat/sta
// only lists *associated* clients, so every phone with its radio asleep would be
// published as away, flipping anyone_home and firing leaving-the-house
// automations on every deploy. The controller remembers when it last saw each
// client, so use that as the starting point instead of assuming the worst.
func (t *Tracker) Seed(lastSeen map[string]time.Time, now time.Time) Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, dev := range t.devices {
		ls, ok := lastSeen[dev.mac]
		if !ok {
			continue
		}
		st := t.state[dev.slug]
		st.lastSeen = ls
		st.since = ls
		// Same rule as a normal poll: recently seen means still here.
		st.online = now.Sub(ls) <= t.awayDelay
	}

	return t.buildSnapshotLocked(now, false)
}

// buildSnapshotLocked assembles the published view. Caller must hold t.mu.
func (t *Tracker) buildSnapshotLocked(now time.Time, connected bool) Snapshot {
	states := make([]ClientState, 0, len(t.devices))
	for _, dev := range t.devices {
		states = append(states, t.buildClientState(dev, t.state[dev.slug]))
	}
	sort.Slice(states, func(i, j int) bool { return states[i].Slug < states[j].Slug })

	snap := Snapshot{
		Clients:    states,
		Floors:     buildFloors(states),
		AnyoneHome: anyOnline(states),
		Connected:  connected,
		UpdatedAt:  now,
	}
	t.snap = snap
	return snap
}

// MarkDisconnected records that the controller could not be reached. Presence
// values are left untouched — a failed poll is not evidence that anyone left.
//
// `forbidden` distinguishes a permissions failure (HTTP 403) from a transient
// one. The liveness probe uses it to decide whether a pod restart could
// plausibly help: for a missing Network role it cannot, and restarting every
// few minutes would only destroy the logs that explain the problem.
func (t *Tracker) MarkDisconnected(forbidden bool) Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.snap.Connected = false
	t.snap.Forbidden = forbidden
	return t.snap
}

func (t *Tracker) Snapshot() Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.snap
}

// Floors returns every floor label that is configured, so the service can seed
// a retained empty state for each one at startup rather than leaving gaps.
func (t *Tracker) Floors() []string {
	seen := map[string]bool{}
	var out []string
	for _, floor := range t.floors {
		if !seen[floor] {
			seen[floor] = true
			out = append(out, floor)
		}
	}
	sort.Strings(out)
	return out
}

func buildFloors(states []ClientState) map[string]FloorState {
	floors := map[string]FloorState{}
	for _, cs := range states {
		if !cs.Online || cs.Floor == "" {
			continue
		}
		fs := floors[cs.Floor]
		fs.Count++
		fs.Clients = append(fs.Clients, cs.Slug)
		floors[cs.Floor] = fs
	}
	return floors
}

func anyOnline(states []ClientState) bool {
	for _, cs := range states {
		if cs.Online {
			return true
		}
	}
	return false
}

// NormalizeMAC lowercases a MAC and strips the separators, so "AA:BB:CC:DD:EE:FF",
// "aa-bb-cc-dd-ee-ff" and "aabbccddeeff" all compare equal.
func NormalizeMAC(mac string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(mac) {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Slugify turns a display name into a topic-safe segment.
func Slugify(name string) string {
	var b strings.Builder
	lastDash := true // leading dashes are suppressed
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		case r == 'ä':
			b.WriteString("ae")
			lastDash = false
		case r == 'ö':
			b.WriteString("oe")
			lastDash = false
		case r == 'ü':
			b.WriteString("ue")
			lastDash = false
		case r == 'ß':
			b.WriteString("ss")
			lastDash = false
		default:
			if !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
