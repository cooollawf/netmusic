// Package app — file logging support.
//
// The shipped binary is a GUI subsystem executable, so on the Windows 7 test
// box there is no console and stderr goes nowhere. -log <path> gives the remote
// runs a reliable channel for diagnosis.
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	logMu sync.Mutex
	logF  *os.File
)

// SetLogFile starts appending diagnostics to path.
func SetLogFile(path string) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	logMu.Lock()
	logF = f
	logMu.Unlock()
	Logf("=== netmusic start %s ===", time.Now().Format("2006-01-02 15:04:05"))
	return nil
}

// CloseLog flushes and closes the log.
func CloseLog() {
	logMu.Lock()
	if logF != nil {
		logF.Sync()
		logF.Close()
		logF = nil
	}
	logMu.Unlock()
}

// Logf appends one timestamped line. Safe from any goroutine.
func Logf(format string, args ...any) {
	logMu.Lock()
	defer logMu.Unlock()
	if logF == nil {
		return
	}
	fmt.Fprintf(logF, "%s %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
	logF.Sync()
}
