package config

import (
	"encoding/json"
	"os"

	"github.com/philipparndt/go-logger"
	"github.com/philipparndt/mqtt-gateway/config"
)

var cfg Config

type Config struct {
	MQTT     config.MQTTConfig `json:"mqtt"`
	UniFi    UniFiConfig       `json:"unifi"`
	Web      WebConfig         `json:"web"`
	LogLevel string            `json:"loglevel,omitempty"`
}

type UniFiConfig struct {
	Host      string `json:"host"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	VerifySSL *bool  `json:"verify-ssl,omitempty"`
	Site      string `json:"site,omitempty"`

	PollingInterval int `json:"polling_interval,omitempty"`

	// AwayDelay is how many seconds a device may be missing from the
	// controller's client list before it is published as offline. Phones power
	// their radio down when idle and vanish for minutes at a time, so without
	// this every night produces a false "left the house" event. Going online is
	// never delayed.
	AwayDelay int `json:"away_delay,omitempty"`

	// Floors maps an access point to a floor label ("ug", "eg", "og"). Keys may
	// be either the AP's MAC or its name in the controller, matched
	// case-insensitively. A client's floor is the floor of the AP it is
	// currently associated with — WiFi bleeds between floors, so read `rssi`
	// alongside `floor` before trusting a borderline reading.
	Floors map[string]string `json:"floors,omitempty"`

	// Devices are the clients to track. Anything not listed here is ignored.
	Devices []DeviceConfig `json:"devices"`
}

type DeviceConfig struct {
	Name string `json:"name"`
	MAC  string `json:"mac"`
	// Slug overrides the topic segment otherwise derived from Name.
	Slug string `json:"slug,omitempty"`
}

type WebConfig struct {
	Enabled bool `json:"enabled"`
	Port    int  `json:"port"`
	// LivenessGraceSeconds is how long the bridge may stay unable to reach the
	// controller before /api/livez starts failing. Defaults to 240.
	LivenessGraceSeconds int `json:"liveness_grace_seconds,omitempty"`
}

// ShouldVerifySSL defaults to false: the gateway serves a self-signed
// certificate, which is why the sibling unifi-access service disables it too.
func (u UniFiConfig) ShouldVerifySSL() bool {
	if u.VerifySSL == nil {
		return false
	}
	return *u.VerifySSL
}

func LoadConfig(file string) (Config, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		logger.Error("Error reading config file", "error", err)
		return Config{}, err
	}

	data = config.ReplaceEnvVariables(data)

	err = json.Unmarshal(data, &cfg)
	if err != nil {
		logger.Error("Unmarshaling JSON", "error", err)
		return Config{}, err
	}

	// Defaults
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.UniFi.Site == "" {
		cfg.UniFi.Site = "default"
	}
	if cfg.UniFi.PollingInterval == 0 {
		cfg.UniFi.PollingInterval = 30
	}
	if cfg.UniFi.AwayDelay == 0 {
		cfg.UniFi.AwayDelay = 300
	}
	if cfg.Web.Port == 0 {
		cfg.Web.Port = 8080
	}

	return cfg, nil
}

func Get() Config {
	return cfg
}
