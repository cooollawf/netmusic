// Package config persists the small amount of state the player needs: the
// login session, last volume, play mode, EQ and window positions.
//
// Two files, on purpose:
//
//	config.json  普通设置，明文，可读可改
//	session.dat  登录 Cookie，DPAPI 加密，绑定当前 Windows 用户
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// EQ band count; keep in sync with audio.EQBands.
const EQBands = 10

type Config struct {
	// Cookies are never serialised into config.json; they live in session.dat
	// protected by DPAPI (see secret_windows.go).
	Cookies  map[string]string `json:"-"`
	UID      int64             `json:"uid,omitempty"`
	Nickname string            `json:"nickname,omitempty"`
	Volume   int               `json:"volume"`
	Mode     int               `json:"mode"`
	Quality  int               `json:"quality"` // requested bitrate: 128000/192000/320000
	Proxy    string            `json:"proxy,omitempty"`

	EQEnabled bool     `json:"eqEnabled"`
	EQGains   [EQBands]int8 `json:"eqGains"` // dB, -12..+12

	DownloadDir string `json:"downloadDir,omitempty"`

	MainX int32 `json:"mainX,omitempty"`
	MainY int32 `json:"mainY,omitempty"`

	LastPlaylist int64 `json:"lastPlaylistId,omitempty"`

	mu       sync.Mutex
	path     string
	sessPath string
	dirty    bool
}

// defaults returns the initial configuration (a fresh value, never a copy of a
// struct that holds a lock).
func defaults() Config {
	return Config{
		Volume:  70,
		Quality: 128000,
		Cookies: map[string]string{},
	}
}

// Dir returns the per user data directory.
func Dir() string {
	if d := os.Getenv("APPDATA"); d != "" {
		return filepath.Join(d, "netmusic")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".netmusic")
}

// DefaultDownloadDir keeps downloads out of the config directory.
func DefaultDownloadDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, "Music", "netmusic")
	}
	return filepath.Join(Dir(), "downloads")
}

// Load reads the config, returning defaults when it does not exist yet.
func Load() *Config {
	c := defaults()
	c.path = filepath.Join(Dir(), "config.json")
	c.sessPath = filepath.Join(Dir(), "session.dat")

	var legacyCookies map[string]string
	if b, err := os.ReadFile(c.path); err == nil {
		// Two decodes of one small file: the first fills c in place (copying a
		// struct that owns a mutex is a vet error and a real footgun), the
		// second only picks up the legacy plaintext cookie block.
		_ = json.Unmarshal(b, &c)
		var legacy struct {
			Cookies map[string]string `json:"cookies"`
		}
		if json.Unmarshal(b, &legacy) == nil {
			legacyCookies = legacy.Cookies
		}
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
	if c.DownloadDir == "" {
		c.DownloadDir = DefaultDownloadDir()
	}
	c.path = filepath.Join(Dir(), "config.json")
	c.sessPath = filepath.Join(Dir(), "session.dat")
	c.dirty = false

	// Session: DPAPI file first, then migrate a plaintext cookie block out of
	// older config.json files (and scrub it from the JSON).
	if cs, err := c.loadSession(); err == nil && len(cs) > 0 {
		c.Cookies = cs
	} else if len(legacyCookies) > 0 {
		c.Cookies = legacyCookies
		_ = c.saveSession()
		_ = c.Save() // rewrites config.json without the cookies
	}
	return &c
}

// Save writes the config atomically.
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.path == "" {
		c.path = filepath.Join(Dir(), "config.json")
	}
	return c.saveLocked()
}

func (c *Config) saveLocked() error {
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
		if err2 := os.WriteFile(c.path, b, 0o600); err2 != nil {
			return err2
		}
	}
	c.dirty = false
	return nil
}

// SaveDebounced coalesces bursts of changes (volume drags, EQ moves).
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
		_ = c.Save()
	}()
}

// ---------------------------------------------------------------------------
// session (DPAPI)
// ---------------------------------------------------------------------------

// SecretStorage describes how the login cookie is kept, for the settings UI.
func SecretStorage() string {
	if runtime.GOOS == "windows" {
		return "Windows DPAPI（绑定当前用户，密文落盘）"
	}
	return "plain file"
}

func (c *Config) loadSession() (map[string]string, error) {
	cipher, err := os.ReadFile(c.sessPath)
	if err != nil {
		return nil, err
	}
	plain, err := unprotect(cipher)
	if err != nil {
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(plain, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *Config) saveSession() error {
	if err := os.MkdirAll(filepath.Dir(c.sessPath), 0o755); err != nil {
		return err
	}
	plain, err := json.Marshal(c.Cookies)
	if err != nil {
		return err
	}
	cipher, err := protect(plain)
	if err != nil {
		return err
	}
	tmp := c.sessPath + ".tmp"
	if err := os.WriteFile(tmp, cipher, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, c.sessPath); err != nil {
		return os.WriteFile(c.sessPath, cipher, 0o600)
	}
	return nil
}

// SetCookies replaces the stored session cookies and persists them encrypted.
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
	err := c.saveSession()
	c.mu.Unlock()
	_ = err
}

// ClearSession forgets the login cookie (logout).
func (c *Config) ClearSession() {
	c.SetCookies(nil)
	c.mu.Lock()
	c.UID = 0
	c.Nickname = ""
	c.mu.Unlock()
	_ = c.Save()
}

// LoggedIn reports whether a usable session cookie is present.
func (c *Config) LoggedIn() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.Cookies["MUSIC_U"]
	return ok
}
