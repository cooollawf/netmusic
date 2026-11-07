package ui

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"netmusic/internal/w32"
)

// Snapshot support.
//
// On a remote machine (the Windows 7 test box) there is no way to see the
// screen, and PrintWindow is unreliable there: PW_RENDERFULLCONTENT only exists
// on Windows 8+, and windows that draw everything in WM_PAINT return blank for
// a plain PrintWindow. So each window dumps its own freshly painted back buffer
// to a PNG instead. That is exactly what the user sees, minus native child
// controls (the EDIT boxes), and it cannot be occluded.

var (
	captureMu   sync.Mutex
	captureDir  string
	captureTag  string
	captureLeft int
)

// Snapshot asks every window to write its next paint to dir/tag-<title>.png.
func Snapshot(dir, tag string) {
	if dir == "" {
		return
	}
	_ = os.MkdirAll(dir, 0o755)
	captureMu.Lock()
	captureDir = dir
	captureTag = tag
	captureLeft = 32
	captureMu.Unlock()
}

func tryCapture(title string, dib *w32.DIB) {
	captureMu.Lock()
	if captureDir == "" || captureLeft <= 0 || dib == nil {
		captureMu.Unlock()
		return
	}
	captureLeft--
	dir, tag := captureDir, captureTag
	captureMu.Unlock()

	name := sanitizeFile(title)
	if name == "" {
		name = "win"
	}
	if tag != "" {
		name = tag + "-" + name
	}
	_ = dib.SavePNG(filepath.Join(dir, name+".png"))
}

func sanitizeFile(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune(r)
		case r > 0x7F:
			b.WriteRune(r) // keep CJK readable; the filesystem handles it
		}
	}
	out := strings.TrimSpace(b.String())
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}
