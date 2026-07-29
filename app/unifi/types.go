package unifi

import "time"

// APIClient is one entry from `/proxy/network/api/s/<site>/stat/sta` — a client
// currently associated with the network. Only the fields presence needs are
// modelled; the controller returns far more.
type APIClient struct {
	MAC      string `json:"mac"`
	Hostname string `json:"hostname"`
	Name     string `json:"name"`
	IP       string `json:"ip"`
	IsWired  bool   `json:"is_wired"`
	ESSID    string `json:"essid"`
	APMac    string `json:"ap_mac"`
	// RSSI is UniFi's signal-above-noise figure: a positive number where higher
	// is better, typically 10–60. It is NOT dBm, despite the name.
	RSSI int `json:"rssi"`
	// Signal is the real received power in dBm, and therefore negative.
	Signal   int   `json:"signal"`
	Uptime   int64 `json:"uptime"`
	LastSeen int64 `json:"last_seen"`
}

// DisplayName is the friendliest label the controller knows for a client, used
// only for logging and for the "unknown device" hints in the UI.
func (c APIClient) DisplayName() string {
	if c.Name != "" {
		return c.Name
	}
	if c.Hostname != "" {
		return c.Hostname
	}
	return c.MAC
}

// APIDevice is one entry from `/proxy/network/api/s/<site>/stat/device` — a
// UniFi device (access point, switch, gateway). Used to turn the `ap_mac` on a
// client into a readable AP name, which is in turn mapped to a floor.
type APIDevice struct {
	MAC  string `json:"mac"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type clientsResponse struct {
	Data []APIClient `json:"data"`
}

type devicesResponse struct {
	Data []APIDevice `json:"data"`
}

// APIUser is one entry from `/proxy/network/api/s/<site>/rest/user` — a client
// the controller *knows*, whether or not it is associated right now. Unlike
// stat/sta it carries `last_seen` for absent clients, which is what lets the
// bridge recover real presence after a restart instead of assuming everyone
// asleep has left.
type APIUser struct {
	MAC      string `json:"mac"`
	LastSeen int64  `json:"last_seen"`
}

type usersResponse struct {
	Data []APIUser `json:"data"`
}

// ClientState is the published presence state of one tracked device. This is
// the payload on `<topic>/clients/<slug>` and is a contract other services
// parse — keep the keys snake_case and stable.
type ClientState struct {
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	MAC    string `json:"mac"`
	Online bool   `json:"online"`
	Floor  string `json:"floor,omitempty"`
	AP     string `json:"ap,omitempty"`
	APMac  string `json:"ap_mac,omitempty"`
	// RSSI is UniFi's signal-above-noise value (higher is better, not dBm).
	RSSI int `json:"rssi,omitempty"`
	// Signal is received power in dBm (negative). Use this to judge whether a
	// floor reading is trustworthy — a weak client may be attached to an AP on
	// another floor.
	Signal   int    `json:"signal,omitempty"`
	ESSID    string `json:"essid,omitempty"`
	IP       string `json:"ip,omitempty"`
	Wired    bool   `json:"wired"`
	Assoc    bool   `json:"associated"` // seen in the controller's client list right now
	LastSeen string `json:"last_seen,omitempty"`
	Since    string `json:"since,omitempty"` // when the current online/offline state began
}

// FloorState is the payload on `<topic>/floors/<floor>`.
type FloorState struct {
	Count   int      `json:"count"`
	Clients []string `json:"clients"`
}

// Snapshot is the whole published world at one instant: what the web UI renders
// and what main.go diffs to decide which topics to republish.
type Snapshot struct {
	Clients    []ClientState         `json:"clients"`
	Floors     map[string]FloorState `json:"floors"`
	AnyoneHome bool                  `json:"anyone_home"`
	Connected  bool                  `json:"connected"`
	// Forbidden means the controller rejected us with 403 — the account is
	// missing the Network role. A configuration problem, not an outage.
	Forbidden bool      `json:"forbidden"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ClientBySlug returns the tracked client with this slug, or nil.
func (s Snapshot) ClientBySlug(slug string) *ClientState {
	for i := range s.Clients {
		if s.Clients[i].Slug == slug {
			return &s.Clients[i]
		}
	}
	return nil
}
