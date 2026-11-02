// Package config loads and persists BRAHM Drop settings.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Fixed ports (same across all BRAHM Drop installs so devices find each other).
const (
	DiscoveryPort = 53331 // UDP announce/listen
	DefaultHTTP   = 53330 // TCP web + transfer
	AppName       = "BRAHM Drop"
	Version       = "1.1"
	Company       = "Brahm InnerBrain Technologies Pvt. Ltd."
	Developer     = "Sandeep Kushwah"
)

// Config is the on-disk settings file.
type Config struct {
	DeviceID   string `json:"device_id"`   // stable random id (fingerprint)
	DeviceName string `json:"device_name"` // shown to other devices
	DeviceKind string `json:"device_kind"` // windows / mac / linux / android / web
	SaveDir    string `json:"save_dir"`    // where received files land
	HTTPPort   int    `json:"http_port"`
	AutoAccept bool   `json:"auto_accept"` // accept incoming without a prompt

	mu   sync.Mutex `json:"-"`
	path string     `json:"-"`
}

// Dir returns the per-user config directory for BRAHM Drop.
func Dir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "BRAHMDrop")
}

func defaultSaveDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	d := filepath.Join(home, "Downloads", "BRAHM Drop")
	if _, err := os.Stat(filepath.Join(home, "Downloads")); err != nil {
		d = filepath.Join(home, "BRAHM Drop")
	}
	return d
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Load reads config from disk, creating it with sane defaults on first run.
func Load() (*Config, error) {
	dir := Dir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "config.json")

	c := &Config{path: path}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, c)
		c.path = path
	}

	// Fill defaults for any missing field.
	changed := false
	if c.DeviceID == "" {
		c.DeviceID = newID()
		changed = true
	}
	if c.DeviceName == "" {
		host, _ := os.Hostname()
		if host == "" {
			host = "My Device"
		}
		c.DeviceName = host
		changed = true
	}
	if c.DeviceKind == "" {
		c.DeviceKind = detectKind()
		changed = true
	}
	if c.SaveDir == "" {
		c.SaveDir = defaultSaveDir()
		changed = true
	}
	if c.HTTPPort == 0 {
		c.HTTPPort = DefaultHTTP
		changed = true
	}
	// AutoAccept defaults to true for a friction-free first experience.
	if _, err := os.Stat(path); err != nil {
		c.AutoAccept = true
		changed = true
	}

	_ = os.MkdirAll(c.SaveDir, 0o755)
	if changed {
		_ = c.Save()
	}
	return c, nil
}

// Save persists the config atomically.
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}
