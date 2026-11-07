// Package netx resolves the HTTP proxy configuration and builds the shared
// *http.Client. NetEase works from mainland China without a proxy, so the
// default is DIRECT; "system" reads the WinINET setting (what Clash keeps up
// to date) and "custom" takes an explicit URL.
package netx

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// ProxyMode is one of "", "system", "env" or a URL like "http://127.0.0.1:7890".
type ProxyMode string

var (
	resolvedOnce sync.Once
	resolved     string
)

// ResolveProxyMode returns the effective mode (used by nmprobe for display).
func ResolveProxyMode() string {
	m := ProxyMode(os.Getenv("NETMUSIC_PROXY"))
	if m == "" {
		return "direct"
	}
	return string(m)
}

func ProxyURL() string {
	u := proxyURL(ProxyMode(os.Getenv("NETMUSIC_PROXY")))
	if u == nil {
		return "(none)"
	}
	return u.String()
}

// ProxyFunc returns a transport-compatible proxy function.
func ProxyFunc() func(*http.Request) (*url.URL, error) {
	var once sync.Once
	var u *url.URL
	return func(req *http.Request) (*url.URL, error) {
		once.Do(func() { u = proxyURL(ProxyMode(os.Getenv("NETMUSIC_PROXY"))) })
		return u, nil
	}
}

func proxyURL(mode ProxyMode) *url.URL {
	resolvedOnce.Do(func() { resolved = string(mode) })
	switch {
	case strings.HasPrefix(string(mode), "http://"), strings.HasPrefix(string(mode), "https://"),
		strings.HasPrefix(string(mode), "socks5://"):
		u, err := url.Parse(string(mode))
		if err == nil {
			return u
		}
	case mode == "env":
		for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
			if v := os.Getenv(k); v != "" {
				if u, err := url.Parse(v); err == nil {
					return u
				}
			}
		}
	case mode == "system":
		if s := winINETProxy(); s != "" {
			if u := parseWinINET(s); u != nil {
				return u
			}
		}
	}
	return nil
}

// parseWinINET turns "127.0.0.1:7890" or "http=host:port;https=host:port" into
// a URL.
func parseWinINET(s string) *url.URL {
	hostport := s
	if strings.Contains(s, "=") {
		hostport = ""
		for _, part := range strings.Split(s, ";") {
			if strings.HasPrefix(strings.TrimSpace(part), "https=") {
				hostport = strings.TrimPrefix(strings.TrimSpace(part), "https=")
				break
			}
		}
		if hostport == "" {
			for _, part := range strings.Split(s, ";") {
				if strings.HasPrefix(strings.TrimSpace(part), "http=") {
					hostport = strings.TrimPrefix(strings.TrimSpace(part), "http=")
					break
				}
			}
		}
		if hostport == "" {
			return nil
		}
	}
	if !strings.Contains(hostport, "://") {
		hostport = "http://" + hostport
	}
	u, err := url.Parse(hostport)
	if err != nil {
		return nil
	}
	return u
}

// NewHTTPClient builds the client used for API traffic.
func NewHTTPClient(mode ProxyMode) *http.Client {
	tr := &http.Transport{
		Proxy:                 fixedProxy(mode),
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
		ForceAttemptHTTP2:     true,
	}
	return &http.Client{Transport: tr, Timeout: 25 * time.Second}
}

func fixedProxy(mode ProxyMode) func(*http.Request) (*url.URL, error) {
	u := proxyURL(mode)
	return func(*http.Request) (*url.URL, error) { return u, nil }
}
