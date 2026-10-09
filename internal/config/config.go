package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	Repos           []string      `json:"repos"`             // paths to watch
	FetchInterval   time.Duration `json:"fetch_interval"`    // how often to fetch
	NotifyMaxPerEvent int         `json:"notify_max"`        // max notifications per drift event
	NotifyInterval  time.Duration `json:"notify_interval"`   // min time between escalating notifications
	IPCPort         int           `json:"ipc_port"`          // localhost port for CLI <-> daemon
	HaikuAPIKey     string        `json:"haiku_api_key"`     // claude haiku key (optional)
	EscalateAfter   time.Duration `json:"escalate_after"`    // time before high-urgency escalation
}

func Default() *Config {
	return &Config{
		Repos:             []string{},
		FetchInterval:     5 * time.Minute,
		NotifyMaxPerEvent: 3,
		NotifyInterval:    15 * time.Minute,
		IPCPort:           7849,
		HaikuAPIKey:       "",
		EscalateAfter:     3 * time.Hour,
	}
}

// ConfigDir returns ~/.vigil, creating it if needed.
func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".vigil")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

// Load reads ~/.vigil/config.json, falling back to defaults.
func Load() (*Config, error) {
	cfg := Default()
	dir, err := ConfigDir()
	if err != nil {
		return cfg, nil
	}
	path := filepath.Join(dir, "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save writes config to ~/.vigil/config.json.
func (c *Config) Save() error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config.json"), data, 0644)
}
