package unifi

import (
	"testing"
	"time"

	"github.com/mqtt-home/mqtt-unifi-network/config"
)

func testConfig() config.UniFiConfig {
	return config.UniFiConfig{
		AwayDelay: 300, // 5 min
		Floors: map[string]string{
			"aa:bb:cc:00:00:01": "eg",
			"OG-Flur":           "og",
		},
		Devices: []config.DeviceConfig{
			{Name: "Philipp", MAC: "11:22:33:44:55:66"},
			{Name: "Anna", MAC: "77:88:99:aa:bb:cc"},
		},
	}
}

// associated builds a client as the controller reports it: `rssi` is UniFi's
// positive signal-above-noise figure, `signal` the real dBm.
func associated(mac, apMAC string, rssi int) APIClient {
	return APIClient{MAC: mac, APMac: apMAC, RSSI: rssi, Signal: -100 + rssi, ESSID: "home", IP: "10.0.0.5"}
}

func TestTracker_GoesOnlineImmediately(t *testing.T) {
	tr := NewTracker(testConfig())
	now := time.Now()

	snap := tr.Update([]APIClient{associated("11:22:33:44:55:66", "aa:bb:cc:00:00:01", -55)}, nil, now)

	philipp := snap.ClientBySlug("philipp")
	if philipp == nil {
		t.Fatal("philipp missing from snapshot")
	}
	if !philipp.Online {
		t.Error("a device present in the client list must be online at once")
	}
	if !snap.AnyoneHome {
		t.Error("anyone_home must be true when a device is online")
	}
}

func TestTracker_StaysOnlineWithinAwayDelay(t *testing.T) {
	tr := NewTracker(testConfig())
	start := time.Now()

	tr.Update([]APIClient{associated("11:22:33:44:55:66", "aa:bb:cc:00:00:01", -55)}, nil, start)
	// Phone idles and drops off the list, but only for 4 of the 5 allowed minutes.
	snap := tr.Update(nil, nil, start.Add(4*time.Minute))

	philipp := snap.ClientBySlug("philipp")
	if !philipp.Online {
		t.Error("a sleeping phone must stay online inside the away delay")
	}
	if philipp.Assoc {
		t.Error("associated must be false while the device is missing from the list")
	}
}

func TestTracker_GoesOfflineAfterAwayDelay(t *testing.T) {
	tr := NewTracker(testConfig())
	start := time.Now()

	tr.Update([]APIClient{associated("11:22:33:44:55:66", "aa:bb:cc:00:00:01", -55)}, nil, start)
	snap := tr.Update(nil, nil, start.Add(6*time.Minute))

	if snap.ClientBySlug("philipp").Online {
		t.Error("a device gone for longer than the away delay must go offline")
	}
	if snap.AnyoneHome {
		t.Error("anyone_home must be false once every device is offline")
	}
}

func TestTracker_AwayDelayRestartsOnReappearance(t *testing.T) {
	tr := NewTracker(testConfig())
	start := time.Now()
	client := associated("11:22:33:44:55:66", "aa:bb:cc:00:00:01", -55)

	tr.Update([]APIClient{client}, nil, start)
	tr.Update(nil, nil, start.Add(4*time.Minute))
	// Wakes up briefly — this must reset the countdown, not just pause it.
	tr.Update([]APIClient{client}, nil, start.Add(5*time.Minute))
	snap := tr.Update(nil, nil, start.Add(9*time.Minute))

	if !snap.ClientBySlug("philipp").Online {
		t.Error("a device seen 4 minutes ago must still be online; the delay restarts on each sighting")
	}
}

func TestTracker_UnseenDeviceStartsOffline(t *testing.T) {
	tr := NewTracker(testConfig())

	snap := tr.Update(nil, nil, time.Now())

	if snap.ClientBySlug("anna").Online {
		t.Error("a device never seen since startup must be offline, not held online by the away delay")
	}
}

func TestTracker_SeedKeepsRecentlySeenDeviceHome(t *testing.T) {
	tr := NewTracker(testConfig())
	now := time.Now()

	// A restart while the phone's radio is asleep: absent from stat/sta, but the
	// controller saw it a minute ago.
	tr.Seed(map[string]time.Time{
		NormalizeMAC("11:22:33:44:55:66"): now.Add(-1 * time.Minute),
	}, now)
	snap := tr.Update(nil, nil, now)

	if !snap.ClientBySlug("philipp").Online {
		t.Error("a restart must not report a sleeping phone as away — seed from the controller's last_seen")
	}
	if !snap.AnyoneHome {
		t.Error("anyone_home must survive a restart while someone is home")
	}
}

func TestTracker_SeedLeavesLongGoneDeviceAway(t *testing.T) {
	tr := NewTracker(testConfig())
	now := time.Now()

	tr.Seed(map[string]time.Time{
		NormalizeMAC("11:22:33:44:55:66"): now.Add(-3 * time.Hour),
	}, now)
	snap := tr.Update(nil, nil, now)

	if snap.ClientBySlug("philipp").Online {
		t.Error("a device last seen hours ago must stay away after a restart")
	}
}

func TestTracker_SeededDeviceGoesAwayAfterDelay(t *testing.T) {
	tr := NewTracker(testConfig())
	now := time.Now()

	// Seeded as home, then never reappears — must still expire normally.
	tr.Seed(map[string]time.Time{
		NormalizeMAC("11:22:33:44:55:66"): now.Add(-1 * time.Minute),
	}, now)
	snap := tr.Update(nil, nil, now.Add(6*time.Minute))

	if snap.ClientBySlug("philipp").Online {
		t.Error("a seeded device that never reappears must go away once the delay elapses")
	}
}

func TestTracker_SeedIgnoresUnknownDevices(t *testing.T) {
	tr := NewTracker(testConfig())
	now := time.Now()

	tr.Seed(map[string]time.Time{NormalizeMAC("de:ad:be:ef:00:00"): now}, now)
	snap := tr.Update(nil, nil, now)

	if snap.AnyoneHome {
		t.Error("last-seen data for untracked MACs must not make anyone home")
	}
}

func TestTracker_FloorFromAPMac(t *testing.T) {
	tr := NewTracker(testConfig())

	snap := tr.Update([]APIClient{associated("11:22:33:44:55:66", "AA-BB-CC-00-00-01", -55)}, nil, time.Now())

	philipp := snap.ClientBySlug("philipp")
	if philipp.Floor != "eg" {
		t.Errorf("floor should resolve from the AP mac regardless of separators, got %q", philipp.Floor)
	}
	if snap.Floors["eg"].Count != 1 {
		t.Errorf("eg floor should hold 1 client, got %d", snap.Floors["eg"].Count)
	}
}

func TestTracker_FloorFromAPName(t *testing.T) {
	tr := NewTracker(testConfig())
	aps := map[string]string{NormalizeMAC("ff:ee:dd:00:00:09"): "OG-Flur"}

	snap := tr.Update([]APIClient{associated("11:22:33:44:55:66", "ff:ee:dd:00:00:09", -60)}, aps, time.Now())

	if got := snap.ClientBySlug("philipp").Floor; got != "og" {
		t.Errorf("floor should resolve from the AP name when the mac is not configured, got %q", got)
	}
}

func TestTracker_OfflineClientLeavesFloors(t *testing.T) {
	tr := NewTracker(testConfig())
	start := time.Now()

	tr.Update([]APIClient{associated("11:22:33:44:55:66", "aa:bb:cc:00:00:01", -55)}, nil, start)
	snap := tr.Update(nil, nil, start.Add(6*time.Minute))

	if _, present := snap.Floors["eg"]; present {
		t.Error("an offline client must not keep occupying a floor")
	}
	if got := snap.ClientBySlug("philipp"); got.RSSI != 0 || got.Signal != 0 || got.AP != "" {
		t.Error("stale radio details must be cleared when the client is no longer associated")
	}
}

func TestTracker_FailedPollDoesNotChangePresence(t *testing.T) {
	tr := NewTracker(testConfig())
	now := time.Now()
	tr.Update([]APIClient{associated("11:22:33:44:55:66", "aa:bb:cc:00:00:01", -55)}, nil, now)

	snap := tr.MarkDisconnected(false)

	if snap.Connected {
		t.Error("snapshot must report the controller as unreachable")
	}
	if snap.Forbidden {
		t.Error("a transient failure must not be reported as a permissions problem")
	}
	if !snap.ClientBySlug("philipp").Online {
		t.Error("a failed poll is not evidence that anyone left — presence must be preserved")
	}
}

func TestTracker_ForbiddenIsDistinctFromUnreachable(t *testing.T) {
	tr := NewTracker(testConfig())

	snap := tr.MarkDisconnected(true)

	if !snap.Forbidden {
		t.Error("a 403 must be flagged so the liveness probe can skip a pointless restart")
	}
	if snap.Connected {
		t.Error("a 403 still means we have no live data")
	}
}

func TestTracker_SuccessfulPollClearsForbidden(t *testing.T) {
	tr := NewTracker(testConfig())
	tr.MarkDisconnected(true)

	// Someone granted the Network role; the next poll must recover on its own.
	snap := tr.Update([]APIClient{associated("11:22:33:44:55:66", "aa:bb:cc:00:00:01", -55)}, nil, time.Now())

	if snap.Forbidden {
		t.Error("a successful poll must clear the forbidden flag")
	}
	if !snap.Connected {
		t.Error("a successful poll must report the controller as reachable")
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Philipp":           "philipp",
		"Philipp's iPhone":  "philipp-s-iphone",
		"Anna Müller":       "anna-mueller",
		"  Küche  Tablet  ": "kueche-tablet",
		"Straße":            "strasse",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeMAC(t *testing.T) {
	want := "aabbccddeeff"
	for _, in := range []string{"AA:BB:CC:DD:EE:FF", "aa-bb-cc-dd-ee-ff", "aabbccddeeff", "AA BB CC DD EE FF"} {
		if got := NormalizeMAC(in); got != want {
			t.Errorf("NormalizeMAC(%q) = %q, want %q", in, got, want)
		}
	}
}
