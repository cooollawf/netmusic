package app

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"netmusic/internal/config"
	"netmusic/internal/netease"
	"netmusic/internal/w32"
)

// This file holds the settings-driven features: quality, equaliser, downloads
// and the account panel. Keeping them out of app.go keeps the orchestration
// code readable.

// openSettings shows the settings panel, refreshed with current state.
func (a *App) openSettings() {
	if a.settings.Visible {
		a.settings.Hide()
		return
	}
	a.refreshSettings()
	a.settings.Place(a.ref())
	a.settings.Show()
}

func (a *App) refreshSettings() {
	login := "未登录（扫码或手机号登录）"
	switch {
	case a.profile != nil:
		login = fmt.Sprintf("%s · uid %d · %s", a.profile.Nickname, a.profile.UserID, config.SecretStorage())
	case a.cfg.LoggedIn():
		login = "会话已保存，等待校验 · " + config.SecretStorage()
	}
	a.settings.SetState(a.cfg.Quality, a.cfg.EQEnabled, a.cfg.EQGains, a.cfg.DownloadDir, login)
}

// applyEQ pushes the persisted EQ state into the audio thread.
func (a *App) applyEQ() {
	if a.pl != nil {
		a.pl.SetEQ(a.cfg.EQGains, a.cfg.EQEnabled)
	}
}

// toast shows a short status line in the main window (download results,
// quality changes, ...).
func (a *App) toast(msg string) {
	a.status = msg
	a.statusAt = time.Now()
	a.Main.Invalidate()
	Logf("status: %s", msg)
}

func (a *App) wireSettings() {
	a.settings.OnQuality = func(bps int) {
		if bps == a.cfg.Quality {
			return
		}
		a.cfg.Quality = bps
		a.cfg.SaveDebounced()
		Logf("quality -> %d", bps)
		a.toast(fmt.Sprintf("音质已设为 %dkbps，下一首生效", bps/1000))
	}
	a.settings.OnEQToggle = func(on bool) {
		a.cfg.EQEnabled = on
		a.cfg.SaveDebounced()
		a.applyEQ()
		Logf("eq enabled=%v", on)
	}
	a.settings.OnEQGain = func(band int, db int8) {
		if band < 0 || band >= config.EQBands {
			return
		}
		a.cfg.EQGains[band] = db
		a.cfg.SaveDebounced()
		a.applyEQ()
	}
	a.settings.OnOpenDownload = func() {
		dir := a.cfg.DownloadDir
		if err := os.MkdirAll(dir, 0o755); err != nil {
			a.toast("无法创建下载目录：" + err.Error())
			return
		}
		if err := w32.ShellOpen(dir); err != nil {
			a.toast("打开目录失败：" + err.Error())
		}
	}
	a.settings.OnLogout = func() {
		a.cfg.ClearSession()
		a.cl.SetCookies(nil)
		a.profile = nil
		a.playlists = nil
		a.queue = nil
		a.updateAccountLabel()
		a.refreshSettings()
		a.toast("已退出登录")
		a.openLogin()
	}
}

// downloadTrack streams a song to the download folder.
//
// It streams straight to the file instead of buffering the whole track: the
// player already holds the decoded source, and a second full copy would double
// the per-track memory for no benefit.
func (a *App) downloadTrack(t netease.Track) {
	if t.ID == 0 {
		return
	}
	name := t.Artists + " - " + t.Name
	Logf("download: id=%d %q", t.ID, name)
	a.toast("开始下载：" + name)

	go func() {
		u, _, err := a.cl.SongURL(t.ID, a.cfg.Quality)
		if err != nil {
			a.post(func() { a.toast("下载失败：" + friendly(err)) })
			return
		}
		dir := a.cfg.DownloadDir
		if dir == "" {
			dir = config.DefaultDownloadDir()
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			a.post(func() { a.toast("下载失败：无法创建目录") })
			return
		}
		ext := ".mp3"
		if strings.HasSuffix(strings.ToLower(stripQuery(u)), ".flac") {
			ext = ".flac"
		}
		final := filepath.Join(dir, safeFileName(name)+ext)
		part := final + ".part"

		req, err := http.NewRequest("GET", u, nil)
		if err != nil {
			a.post(func() { a.toast("下载失败：" + err.Error()) })
			return
		}
		req.Header.Set("User-Agent", "Mozilla/5.0")
		hc := &http.Client{Transport: a.http.Transport, Timeout: 10 * time.Minute}
		resp, err := hc.Do(req)
		if err != nil {
			a.post(func() { a.toast("下载失败：" + friendly(err)) })
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			a.post(func() { a.toast(fmt.Sprintf("下载失败：HTTP %d", resp.StatusCode)) })
			return
		}
		f, err := os.Create(part)
		if err != nil {
			a.post(func() { a.toast("下载失败：" + err.Error()) })
			return
		}
		n, cerr := io.Copy(f, resp.Body)
		syncErr := f.Close()
		if cerr != nil || syncErr != nil || n == 0 {
			os.Remove(part)
			a.post(func() { a.toast("下载中断，已清理临时文件") })
			Logf("download failed: %v %v bytes=%d", cerr, syncErr, n)
			return
		}
		if err := os.Rename(part, final); err != nil {
			os.Remove(part)
			a.post(func() { a.toast("下载失败：无法写入目标文件") })
			return
		}
		Logf("downloaded %s (%d bytes)", final, n)
		a.post(func() { a.toast(fmt.Sprintf("已下载：%s（%.1fMB）", filepath.Base(final), float64(n)/1048576)) })
	}()
}

// safeFileName strips characters Windows refuses in file names.
func safeFileName(s string) string {
	rep := strings.NewReplacer(
		"\\", "_", "/", "_", ":", "_", "*", "_", "?", "_",
		"\"", "_", "<", "_", ">", "_", "|", "_", "\n", " ", "\r", " ",
	)
	out := strings.TrimSpace(rep.Replace(s))
	if len(out) > 120 {
		out = out[:120]
	}
	if out == "" {
		out = "track"
	}
	return out
}
