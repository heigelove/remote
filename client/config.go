package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type Config struct {
	ServerURL    string `json:"server_url"`
	DeviceToken  string `json:"device_token"`
	DeviceUID    string `json:"device_uid"`
	DeviceName   string `json:"device_name"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	AutoStart    bool   `json:"auto_start"`
	AutoConnect  bool   `json:"auto_connect"`
	UIPort       int    `json:"ui_port"`
}

var (
	cfgMu   sync.RWMutex
	cfgPath = "remote.json"
)

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil && os.IsNotExist(err) {
		legacy := filepath.Join(filepath.Dir(path), "remoto.json")
		if filepath.Clean(path) != filepath.Clean(legacy) {
			data, err = os.ReadFile(legacy)
		}
	}
	if err != nil {
		return &Config{
			ServerURL:   "http://127.0.0.1:8080",
			AutoConnect: true,
			UIPort:      17989,
		}, nil
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	if c.ServerURL == "" {
		c.ServerURL = "http://127.0.0.1:8080"
	}
	if c.UIPort == 0 {
		c.UIPort = 17989
	}
	return &c, nil
}

func saveConfig(path string, c *Config) error {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0755)
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(path, b, 0600)
}

func getConfig() Config {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	// return copy via JSON-ish - we store pointer
	if currentCfg == nil {
		return Config{ServerURL: "http://127.0.0.1:8080", UIPort: 17989}
	}
	return *currentCfg
}

func setConfig(c *Config) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	currentCfg = c
}

var currentCfg *Config
