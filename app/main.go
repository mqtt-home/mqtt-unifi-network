package main

import (
	"encoding/json"
	"errors"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/mqtt-home/mqtt-unifi-network/config"
	"github.com/mqtt-home/mqtt-unifi-network/unifi"
	"github.com/mqtt-home/mqtt-unifi-network/version"
	"github.com/mqtt-home/mqtt-unifi-network/web"
	"github.com/philipparndt/go-logger"
	"github.com/philipparndt/mqtt-gateway/mqtt"
)

var (
	client    *unifi.Client
	tracker   *unifi.Tracker
	webServer *web.WebServer

	inventoryLogged bool

	// published is the last state written to MQTT, so each poll only republishes
	// what actually changed. Presence is read by rules that trigger on every
	// message, so a republished identical payload is not free.
	published struct {
		clients    map[string]string
		floors     map[string]string
		anyoneHome *bool
	}
)

func publishAvailability(online bool) {
	cfg := config.Get()
	payload := "offline"
	if online {
		payload = "online"
	}
	mqtt.PublishAbsolute(cfg.MQTT.Topic+"/availability", payload, true)
}

// publishSnapshot writes the changed parts of a snapshot to MQTT.
func publishSnapshot(snap unifi.Snapshot) {
	cfg := config.Get()
	base := cfg.MQTT.Topic

	for _, cs := range snap.Clients {
		data, err := json.Marshal(cs)
		if err != nil {
			logger.Error("Failed to marshal client state", "client", cs.Slug, "error", err)
			continue
		}
		if published.clients[cs.Slug] == string(data) {
			continue
		}
		published.clients[cs.Slug] = string(data)
		mqtt.PublishAbsolute(base+"/clients/"+cs.Slug, string(data), cfg.MQTT.Retain)
		logger.Info("Presence changed", "client", cs.Slug, "online", cs.Online, "floor", cs.Floor, "rssi", cs.RSSI)
	}

	// Publish every known floor, including the ones that just emptied — a floor
	// topic that stops updating reads as "still occupied" to a consumer.
	for _, floor := range knownFloors(snap) {
		fs, ok := snap.Floors[floor]
		if !ok {
			fs = unifi.FloorState{Count: 0, Clients: []string{}}
		}
		if fs.Clients == nil {
			fs.Clients = []string{}
		}
		data, err := json.Marshal(fs)
		if err != nil {
			logger.Error("Failed to marshal floor state", "floor", floor, "error", err)
			continue
		}
		if published.floors[floor] == string(data) {
			continue
		}
		published.floors[floor] = string(data)
		mqtt.PublishAbsolute(base+"/floors/"+floor, string(data), cfg.MQTT.Retain)
		logger.Debug("Floor changed", "floor", floor, "count", fs.Count)
	}

	if published.anyoneHome == nil || *published.anyoneHome != snap.AnyoneHome {
		value := snap.AnyoneHome
		published.anyoneHome = &value
		// Raw boolean, not JSON — mqtt-rules compares this literally.
		mqtt.PublishAbsolute(base+"/anyone_home", strconv.FormatBool(value), true)
		logger.Info("anyone_home changed", "value", value)
	}

	if webServer != nil {
		webServer.BroadcastSnapshot(snap)
	}
}

// knownFloors is every floor from config plus any seen in this snapshot, so a
// floor configured but currently empty is still published.
func knownFloors(snap unifi.Snapshot) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range tracker.Floors() {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	for f := range snap.Floors {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

// seedPresence adopts the controller's last-seen times before the first poll,
// so a restart does not publish everyone whose phone is asleep as away. Best
// effort: if it fails, the first poll simply starts from "nobody seen yet".
func seedPresence() {
	lastSeen, err := client.GetKnownClients()
	if err != nil {
		logger.Warn("Could not seed presence from the controller's last-seen times; "+
			"sleeping devices may briefly read as away", "error", err)
		return
	}

	snap := tracker.Seed(lastSeen, time.Now())
	online := 0
	for _, cs := range snap.Clients {
		if cs.Online {
			online++
		}
	}
	logger.Info("Seeded presence from controller history", "known", len(lastSeen), "online", online)
}

// poll fetches the controller state once. A failure is logged and reported, not
// fatal: the network blips, and nobody left the house because an HTTP call
// timed out.
func poll() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Panic during poll", "panic", r)
		}
	}()

	clients, err := client.GetClients()
	if err != nil {
		if errors.Is(err, unifi.ErrForbidden) {
			// Permanent until someone changes the account's role, so say exactly
			// what to do instead of burying it in a generic failure.
			logger.Error("UniFi rejected the request with 403. The account authenticates to UniFi OS "+
				"but has no role in the Network application — grant it 'Network → View Only', or use a "+
				"dedicated account. Not restarting: a restart cannot fix this.", "error", err)
		} else {
			logger.Error("Failed to fetch clients", "error", err)
		}
		publishSnapshotConnectivity(false, errors.Is(err, unifi.ErrForbidden))
		return
	}

	// The AP list only changes when hardware does, but it is one cheap call and
	// keeps floor names correct after an AP is renamed.
	aps, err := client.GetAccessPoints()
	if err != nil {
		logger.Warn("Failed to fetch access points, floors fall back to AP mac", "error", err)
		aps = nil
	}

	logInventory(clients, aps)

	snap := tracker.Update(clients, aps, time.Now())
	publishAvailability(true)
	publishSnapshot(snap)
}

// logInventory dumps what the controller can see, once, after the first
// successful poll. There is no other practical way to learn a phone's MAC or an
// AP's name in order to fill in `devices` and `floors` — so when nothing is
// configured yet this logs at info, and once it is, it drops to debug.
func logInventory(clients []unifi.APIClient, aps map[string]string) {
	if inventoryLogged {
		return
	}
	inventoryLogged = true

	level := logger.Debug
	if len(config.Get().UniFi.Devices) == 0 {
		level = logger.Info
		logger.Info("No devices configured. The clients below are what the controller sees — " +
			"copy the MACs of the ones to track into unifi.devices.")
	}

	for mac, name := range aps {
		level("Access point", "name", name, "mac", mac)
	}
	for _, c := range clients {
		level("Client seen",
			"name", c.DisplayName(),
			"mac", unifi.NormalizeMAC(c.MAC),
			"ap", aps[unifi.NormalizeMAC(c.APMac)],
			"essid", c.ESSID,
			"rssi", c.RSSI,
			"wired", c.IsWired)
	}
}

func publishSnapshotConnectivity(online, forbidden bool) {
	publishAvailability(online)
	snap := tracker.MarkDisconnected(forbidden)
	if webServer != nil {
		webServer.BroadcastSnapshot(snap)
	}
}

func startPolling(interval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			poll()
		case <-stop:
			logger.Info("Polling stopped")
			return
		}
	}
}

func main() {
	logger.Init("info", logger.Logger())
	logger.Info("mqtt-unifi-network", "version", version.Info())
	initPprof()

	if len(os.Args) < 2 {
		logger.Error("No configuration file specified")
		os.Exit(1)
	}

	configFile := os.Args[1]
	logger.Info("Configuration file", "path", configFile)

	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		logger.Error("Failed to load configuration", "error", err)
		return
	}

	logger.SetLevel(cfg.LogLevel)

	if len(cfg.UniFi.Devices) == 0 {
		logger.Warn("No devices configured — nothing will be tracked")
	}

	published.clients = make(map[string]string)
	published.floors = make(map[string]string)

	mqtt.Start(cfg.MQTT, "unifi_network")

	// Seed a retained offline before connecting, so the availability topic is
	// never absent and consumers start from a safe default.
	publishAvailability(false)

	tracker = unifi.NewTracker(cfg.UniFi)
	client = unifi.NewClient(
		cfg.UniFi.Host,
		cfg.UniFi.Username,
		cfg.UniFi.Password,
		cfg.UniFi.Site,
		cfg.UniFi.ShouldVerifySSL(),
	)

	// Start the web server BEFORE talking to the controller. Login and the first
	// poll each block for up to the HTTP timeout, and an unreachable controller
	// would otherwise leave /api/livez refusing connections — which k8s reads as
	// a failed probe and answers with a restart, forever, without the bridge
	// ever serving a request.
	if !cfg.Web.Enabled {
		logger.Info("Web interface is disabled in the configuration")
	} else {
		webServer = web.NewWebServer(tracker)
		go func() {
			logger.Info("Web interface available", "url", "http://localhost:"+strconv.Itoa(cfg.Web.Port))
			if err := webServer.Start(cfg.Web.Port); err != nil {
				logger.Error("Failed to start web server", "error", err)
			}
		}()
	}

	stopPolling := make(chan struct{})
	go func() {
		if err := client.Login(); err != nil {
			// Not fatal: poll() re-authenticates on 401 and the liveness probe
			// restarts the pod if the controller stays unreachable.
			logger.Error("Initial login failed, will retry on next poll", "error", err)
		}
		seedPresence()
		poll()
		startPolling(time.Duration(cfg.UniFi.PollingInterval)*time.Second, stopPolling)
	}()

	logger.Info("Application ready", "devices", len(cfg.UniFi.Devices), "awayDelay", cfg.UniFi.AwayDelay)

	quitChannel := make(chan os.Signal, 1)
	signal.Notify(quitChannel, syscall.SIGINT, syscall.SIGTERM)
	<-quitChannel

	close(stopPolling)

	// Say goodbye explicitly. mqtt-gateway's last will only covers
	// `<topic>/bridge/state`; without this a graceful stop would leave a
	// retained `availability: online` behind, and consumers would trust stale
	// presence until the bridge came back.
	publishAvailability(false)

	logger.Info("Shutdown complete")
}

// initPprof exposes pprof + expvar on :6060. Every bridge does this; the chart
// opens the port so `kubectl port-forward … 6060` works without a redeploy.
func initPprof() {
	go func() {
		http.ListenAndServe(":6060", nil)
	}()
}
