// netmusic — a deliberately tiny NetEase Cloud Music desktop player.
//
// Guiding rule: resident memory first, features second. Everything below the
// UI is hand rolled on top of Win32 so the process never carries a webview, a
// media framework or a widget toolkit.
package main

import (
	"flag"
	"os"
	"runtime"
	"runtime/debug"

	"netmusic/internal/app"
	"netmusic/internal/audio"
	"netmusic/internal/ui"
	"netmusic/internal/w32"
)

func main() {
	selftest := flag.Bool("selftest", false, "play a free track and open every window (no login needed)")
	stage := flag.Int("stage", 5, "selftest depth: 0=idle 1=playlist 2=playing 3=lyrics 4=comments 5=comments page 2")
	soak := flag.Int("soak", 0, "play N consecutive tracks with all windows open, logging memory each round")
	noGCLimit := flag.Bool("no-gc-limit", false, "disable the 64 MB soft memory limit (for measurement)")
	audioFallback := flag.Bool("force-audio-fallback", false,
		"convert samples in software instead of relying on WASAPI AUTOCONVERTPCM (try this if there is no sound)")
	logPath := flag.String("log", "", "append diagnostics to this file (the GUI build has no console)")
	query := flag.String("query", "", "selftest search keyword (use a foreign song to check bilingual lyrics)")
		shotDir := flag.String("shot", "", "dump every window to <dir>/NN-<title>.png at each measurement point")
	flag.Parse()

	// One OS thread drives all of Win32; goroutines handle IO and audio.
	runtime.LockOSThread()

	// Keep the Go heap on a short leash so RSS stays far below 100 MB. The
	// soft limit only makes the collector more eager; it never fails an alloc.
	if !*noGCLimit {
		debug.SetMemoryLimit(64 << 20)
	}
	if p := runtime.NumCPU(); p > 4 {
		runtime.GOMAXPROCS(4)
	}

	if *audioFallback {
		audio.ForceMixFormat = true
	}

	// Remote diagnosis: a GUI subsystem process has no console, so every piece
	// of evidence (log lines + window snapshots) has to go to files.
	if *logPath != "" {
		if err := app.SetLogFile(*logPath); err != nil {
			os.Exit(3)
		}
		defer app.CloseLog()
	}

	w32.SetProcessDPIAware()
	w32.InitCommonControls()
	ui.InitTheme()

	a := app.New()
	a.SelfTest = *selftest
	a.SelfTestStage = *stage
	a.SoakRounds = *soak
		a.SelfTestQuery = *query
	a.SetShotDir(*shotDir)
	os.Exit(a.Run())
}
