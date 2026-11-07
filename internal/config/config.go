// Package config persists the small amount of state the player needs: the
// login cookies, last volume, play mode and window positions. One JSON file,
// written atomically, no database.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Config struct {
	Cookies  map[string]string `json:"cookies,omitempty"`
	UID      int64             `json:"uid,omitempty"`
	Nickname string            `json:"nickname,omitempty"`
	Volume   int               `json:"volume"`
	Mode     int               `json:"mode"`
	Quality  int               `json:"quality"` // requested bitrate, e.g. 128000
	Proxy    string            `json:"proxy,omitempty"`

	MainX int32 `json:"mainX,omitempty"`
	MainY int32 `json:"mainY,omitempty"`
	LastPlaylist int64 `json:"lastPlaylistId,omitempty"`

	mu    sync.Mutex
	path  string
	dirty bool
}

// defaults returns the initial configuration (a fresh value, never a copy of a
// struct that holds a lock).
func defaults() Config {
	return Config{Volume: 70, Quality: 128000, Cookies: map[string]string{}}
}

// Dir returns the per user data directory.
func Dir() string {
	if d := os.Getenv("APPDATA"); d != "" {
		return filepath.Join(d, "netmusic")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".netmusic")
}

// Load reads the config, returning defaults when it does not exist yet.
func Load() *Config {
	c := defaults()
	c.path = filepath.Join(Dir(), "config.json")
	if b, err := os.ReadFile(c.path); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.Cookies == nil {
		c.Cookies = map[string]string{}
	}
	if c.Volume < 0 || c.Volume > 100 {
		c.Volume = 70
	}
	if c.Quality == 0 {
		c.Quality = 128000
	}
	c.path = filepath.Join(Dir(), "config.json")
	c.dirty = false
	return &c
}

// Save writes the config atomically.
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.path == "" {
		c.path = filepath.Join(Dir(), "config.json")
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, c.path); err != nil {
		// Rename can fail if the target is locked; fall back to a direct write.
		return os.WriteFile(c.path, b, 0o600)
	}
	c.dirty = false
	return nil
}

// SaveDebounced coalesces bursts of changes (volume drags, mode clicks).
func (c *Config) SaveDebounced() {
	c.mu.Lock()
	if c.dirty {
		c.mu.Unlock()
		return
	}
	c.dirty = true
	c.mu.Unlock()
	go func() {
		time.Sleep(1500 * time.Millisecond)
		c.mu.Lock()
		c.dirty = false
		c.mu.Unlock()
		_ = c.Save()
	}()
}

// SetCookies replaces the stored session cookies.
func (c *Config) SetCookies(m map[string]string) {
	c.mu.Lock()
	if len(m) == 0 {
		c.Cookies = map[string]string{}
	} else {
		c.Cookies = make(map[string]string, len(m))
		for k, v := range m {
			c.Cookies[k] = v
		}
	}
	c.mu.Unlock()
	c.SaveDebounced()
}

// LoggedIn reports whether a usable session cookie is present.
func (c *Config) LoggedIn() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.Cookies["MUSIC_U"]
	return ok
}
