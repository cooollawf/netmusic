// Package app wires the Win32 UI, the NetEase client and the audio player
// together. All UI mutation happens on the UI thread; background goroutines
// hand results back through post(), which posts a single message.
package app

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"io"
	"image/jpeg"
	"image/png"
	"math/rand"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"netmusic/internal/audio"
	"netmusic/internal/config"
	"netmusic/internal/lrc"
	"netmusic/internal/netease"
	"netmusic/internal/netx"
	"netmusic/internal/ui"
	"netmusic/internal/w32"
)

type App struct {
	cfg  *config.Config
	cl   *netease.Client
	pl   *audio.Player
	http *http.Client

	Main  *ui.MainWindow
	list  *ui.PlaylistWindow
	lyr   *ui.LyricsWindow
	comm  *ui.CommentsWindow
	login *ui.LoginWindow

	uiQ chan func()

	// playback state
	queue     []netease.Track
	queueAt   int
	cur       netease.Track
	curCover  *w32.DIB
	mode      int
	loading   bool
	loadNote  string
	playError string

	// browsed view
	viewTracks []netease.Track
	viewIsQueue bool
	viewHeader string
	playlists  []netease.Playlist

	lyricDoc *lrc.Doc
	lyricFor int64

	commSong int64
	loginGen atomic.Int32
	profile  *netease.Profile

	// SelfTest makes the app play a known free track and pop every window, so
	// the whole pipeline can be verified without (and before) logging in.
	SelfTest      bool
	SelfTestStage int
	SoakRounds    int
}

// New builds the app and all of its windows.
func New() *App {
	a := &App{
		cfg: config.Load(),
		uiQ: make(chan func(), 64),
		mode: 0,
	}
	a.mode = a.cfg.Mode
	a.http = netx.NewHTTPClient(netx.ProxyMode(a.cfg.Proxy))
	a.cl = netease.NewClient(a.http)
	a.cl.SetCookies(a.cfg.Cookies)
	a.cl.OnCookiesChanged = func(m map[string]string) {
		a.cfg.SetCookies(m)
	}

	a.pl = audio.NewPlayer()
	a.pl.SetVolume(a.cfg.Volume)

	a.Main = ui.NewMainWindow(a)
	a.Main.OnQuit = a.shutdown
	a.Main.OnTick = a.onTick
	a.Main.OnDispatch = a.drain
	a.Main.OnOpen = a.onAction

	a.list = ui.NewPlaylistWindow()
	a.lyr = ui.NewLyricsWindow()
	a.comm = ui.NewCommentsWindow()
	a.login = ui.NewLoginWindow()
	a.list.Cascade = 0
	a.lyr.Cascade = 1
	a.comm.Cascade = 2
	a.login.Cascade = 3

	a.wire()

	a.pl.SetOnEnd(func() { a.post(a.advanceAuto) })
	a.pl.SetOnError(func(err error) {
		a.post(func() {
			a.playError = err.Error()
			a.Main.Invalidate()
		})
	})
	return a
}

// Run shows the main window, restores the session and pumps messages.
func (a *App) Run() int {
	a.Main.Show()
	a.updateAccountLabel()
	go a.bootstrap()
	if a.SelfTest {
		go a.runSelfTest()
	}
	if a.SoakRounds > 0 {
		go a.runSoak(a.SoakRounds)
	}
	return w32.RunMessageLoop()
}

// runSelfTest exercises search -> resolve -> download -> decode -> WASAPI plus
// the lyrics and comments windows, without needing a logged in account. It is
// also the harness behind the memory report: each stage prints measured RSS.
func (a *App) runSelfTest() {
	stage := a.SelfTestStage
	time.Sleep(700 * time.Millisecond)
	a.memLog("stage0: 主窗口就绪")

	tracks, err := a.cl.Search("卡农 钢琴", 8)
	if err != nil || len(tracks) == 0 {
		a.memLog("search failed: " + friendly(err))
		return
	}
	// Prefer a track that actually yields a playable URL.
	var pick int = -1
	for i := range tracks {
		if _, _, err := a.cl.SongURL(tracks[i].ID, a.cfg.Quality); err == nil {
			pick = i
			break
		}
	}
	if pick < 0 {
		a.memLog("no playable track in results")
		return
	}
	chosen := tracks[pick]

	if stage >= 1 {
		a.post(func() {
			a.viewTracks = tracks
			a.viewIsQueue = false
			a.viewHeader = fmt.Sprintf("搜索结果 · %d 首", len(tracks))
			a.list.SetTracks(a.trackVMs(tracks), a.viewHeader, false)
			a.togglePlaylist()
		})
		time.Sleep(600 * time.Millisecond)
		a.memLog("stage1: 播放列表窗口 + 搜索结果")
	}

	if stage >= 2 {
		a.post(func() {
			a.queue = tracks
			a.queueAt = pick
			a.play(chosen)
		})
		time.Sleep(3500 * time.Millisecond)
		a.memLog("stage2: 正在播放（音频+封面+歌词已加载）")
	}

	if stage >= 3 {
		a.post(func() { a.toggleLyrics() })
		time.Sleep(900 * time.Millisecond)
		a.memLog("stage3: 歌词窗口")
	}

	if stage >= 4 {
		a.post(func() { a.toggleComments() })
		time.Sleep(1800 * time.Millisecond)
		a.memLog("stage4: 评论窗口（热门 15 条）")

		// second page of the newest comments
		a.post(func() {
			a.comm.ShowLatest(1)
		})
		time.Sleep(1800 * time.Millisecond)
		a.memLog("stage5: 评论窗口（最新第 2 页）")
	}
}

// runSoak plays a run of tracks with every window open and samples memory
// after each one. It exists to prove two things in the memory report: RSS is
// flat across track changes, and no GDI/USER handles leak.
func (a *App) runSoak(rounds int) {
	time.Sleep(600 * time.Millisecond)
	tracks, err := a.cl.Search("钢琴", 40)
	if err != nil || len(tracks) == 0 {
		a.memLog("soak: search failed " + friendly(err))
		return
	}
	playable := make([]netease.Track, 0, len(tracks))
	for _, t := range tracks {
		if u, _, err := a.cl.SongURL(t.ID, a.cfg.Quality); err == nil && u != "" {
			playable = append(playable, t)
			if len(playable) >= rounds+2 {
				break
			}
		}
	}
	if len(playable) == 0 {
		a.memLog("soak: no playable tracks")
		return
	}
	a.post(func() {
		a.queue = playable
		a.queueAt = 0
		a.playCurrent()
		a.togglePlaylist()
		a.toggleLyrics()
		a.toggleComments()
	})
	time.Sleep(6 * time.Second)
	a.memLog("soak 0: 首曲播放 + 全部窗口")

	for i := 0; i < rounds; i++ {
		a.post(func() { a.advance(1) })
		time.Sleep(5 * time.Second)
		a.memLog(fmt.Sprintf("soak %d: 切歌后", i+1))
	}
	a.memLog("soak done")
}
func (a *App) memLog(label string) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	ws, priv := w32.Memory()
	gdi, user := w32.GuiResources(w32.CurrentProcess())
	fmt.Fprintf(os.Stderr, "MEM %-42s WS=%6.2fMB  Priv=%6.2fMB  Heap=%6.2fMB  Sys=%6.2fMB  GDI=%d USER=%d\n",
		label,
		float64(ws)/1048576,
		float64(priv)/1048576,
		float64(ms.HeapAlloc)/1048576,
		float64(ms.Sys)/1048576,
		gdi, user,
	)
}

// ---------------------------------------------------------------------------
// UI thread plumbing
// ---------------------------------------------------------------------------

// post queues f for the UI thread.
func (a *App) post(f func()) {
	select {
	case a.uiQ <- f:
		w32.PostMessage(a.Main.HWND(), ui.WMDispatch, 0, 0)
	default:
		// queue full: drop rather than block a worker
	}
}

// drain runs queued closures (main window message handler).
func (a *App) drain() {
	for {
		select {
		case f := <-a.uiQ:
			f()
		default:
			return
		}
	}
}

// ---------------------------------------------------------------------------
// ui.Controller
// ---------------------------------------------------------------------------

func (a *App) IsPlaying() bool     { return a.pl.IsPlaying() }
func (a *App) Position() float64   { return a.pl.Position() }
func (a *App) Volume() int         { return a.pl.Volume() }
func (a *App) Mode() int           { return a.mode }
func (a *App) Duration() float64 {
	if d := a.pl.Duration(); d > 0 {
		return d
	}
	if a.cur.Duration > 0 {
		return float64(a.cur.Duration)
	}
	return 0
}

func (a *App) Track() ui.TrackInfo {
	ti := ui.TrackInfo{
		Title:  a.cur.Name,
		Artist: a.cur.Artists,
		Album:  a.cur.Album,
		Cover:  a.curCover,
	}
	if ti.Title == "" {
		ti.Title = "未在播放"
	}
	if a.loading && a.loadNote != "" {
		ti.Album = a.loadNote
	} else if a.playError != "" {
		ti.Album = a.playError
	}
	return ti
}

func (a *App) Toggle() {
	if a.loading {
		return
	}
	a.pl.Toggle()
	a.Main.Invalidate()
}

func (a *App) Next() { a.advance(1) }
func (a *App) Prev() { a.advance(-1) }

func (a *App) Seek(frac float64) {
	a.pl.SeekFrac(frac)
	a.lyricCatchUp()
}

func (a *App) SetVolume(v int) {
	a.pl.SetVolume(v)
	a.cfg.Volume = v
	a.cfg.SaveDebounced()
	a.Main.Invalidate()
}

func (a *App) CycleMode() {
	a.mode = (a.mode + 1) % 3
	a.cfg.Mode = a.mode
	a.cfg.SaveDebounced()
}

// ---------------------------------------------------------------------------
// playback
// ---------------------------------------------------------------------------

func (a *App) advance(dir int) {
	if len(a.queue) == 0 {
		return
	}
	a.queueAt += dir
	if a.queueAt < 0 {
		a.queueAt = len(a.queue) - 1
	}
	if a.queueAt >= len(a.queue) {
		a.queueAt = 0
	}
	a.playCurrent()
}

// advanceAuto is what happens when a track ends by itself.
func (a *App) advanceAuto() {
	if len(a.queue) == 0 {
		return
	}
	switch a.mode {
	case 1: // single repeat
		a.playCurrent()
	case 2: // shuffle
		if len(a.queue) > 1 {
			a.queueAt = rand.Intn(len(a.queue))
		}
		a.playCurrent()
	default:
		a.advance(1)
	}
}

func (a *App) playCurrent() {
	if a.queueAt < 0 || a.queueAt >= len(a.queue) {
		return
	}
	a.play(a.queue[a.queueAt])
	a.list.MarkPlaying(a.queueAt)
}

// play starts a track. Safe to call only from the UI thread.
func (a *App) play(t netease.Track) {
	a.cur = t
	a.loading = true
	a.loadNote = "解析中…"
	a.playError = ""
	a.lyricDoc = nil
	a.lyricFor = 0
	a.lyr.SetDoc(nil, t.Name)
	a.Main.Invalidate()

	br := a.cfg.Quality
	go func(track netease.Track) {
		u, realBr, err := a.cl.SongURL(track.ID, br)
		if err != nil {
			a.post(func() {
				if a.cur.ID != track.ID {
					return
				}
				a.loading = false
				a.playError = friendly(err)
				a.Main.Invalidate()
			})
			return
		}
		// FLAC cannot be decoded by the pure Go MP3 path; ask for MP3.
		if strings.HasSuffix(strings.ToLower(stripQuery(u)), ".flac") {
			if u2, br2, err2 := a.cl.SongURL(track.ID, 320000); err2 == nil {
				u, realBr = u2, br2
			}
		}
		a.post(func() {
			if a.cur.ID == track.ID {
				a.loadNote = "缓冲中…"
				a.Main.Invalidate()
			}
		})
		data, err := a.download(u)
		if err != nil {
			a.post(func() {
				if a.cur.ID != track.ID {
					return
				}
				a.loading = false
				a.playError = "下载失败: " + friendly(err)
				a.Main.Invalidate()
			})
			return
		}
		_ = realBr
		a.pl.Load(data, track.Duration)
		a.post(func() {
			if a.cur.ID != track.ID {
				return
			}
			a.loading = false
			a.playError = ""
			a.Main.Invalidate()
		})
		// side loads, independent of the audio path
		go a.loadCover(track)
		go a.loadLyric(track)
		go a.loadCommentsFor(track)
	}(t)
}

func (a *App) download(u string) ([]byte, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	hc := &http.Client{Transport: a.http.Transport, Timeout: 60 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	// Allocate exactly once from Content-Length: a bytes.Buffer would double
	// its capacity and waste several MB on a 320 kbps track.
	n := resp.ContentLength
	if n > 0 && n <= 40<<20 {
		buf := make([]byte, n)
		m, rerr := io.ReadFull(resp.Body, buf)
		if rerr != nil && !errors.Is(rerr, io.ErrUnexpectedEOF) && !errors.Is(rerr, io.EOF) {
			return nil, rerr
		}
		if m == 0 {
			return nil, errors.New("empty response")
		}
		return buf[:m], nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 40<<20))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New("empty response")
	}
	return data, nil
}

func (a *App) loadCover(t netease.Track) {
	if t.PicURL == "" {
		return
	}
	u := netease.PicURL(t.PicURL, 200)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := a.http.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var img image.Image
	if strings.Contains(u, ".png") {
		img, err = png.Decode(resp.Body)
	} else {
		img, err = jpeg.Decode(resp.Body)
	}
	if err != nil || img == nil {
		return
	}
	size := ui.CoverSize()
	dib := w32.ImageToDIB(img, size, size)
	if dib == nil {
		return
	}
	a.post(func() {
		if a.cur.ID != t.ID {
			dib.Free()
			return
		}
		if a.curCover != nil {
			a.curCover.Free()
		}
		a.curCover = dib
		a.Main.Invalidate()
	})
}

func (a *App) loadLyric(t netease.Track) {
	raw, tr, err := a.cl.Lyric(t.ID)
	if err != nil {
		return
	}
	doc := lrc.Parse(raw, tr)
	a.post(func() {
		if a.cur.ID != t.ID {
			return
		}
		a.lyricDoc = doc
		a.lyricFor = t.ID
		a.lyr.SetDoc(doc, t.Name)
	})
}

// lyricCatchUp re-centers the lyric panel after a seek.
func (a *App) lyricCatchUp() {
	if a.lyricDoc == nil {
		return
	}
	if a.lyr.Tick(int64(a.pl.Position() * 1000)) {
		a.lyr.Invalidate()
	}
}

func (a *App) onTick() {
	if a.lyricDoc != nil && a.lyr.Visible {
		if a.lyr.Tick(int64(a.pl.Position() * 1000)) {
			a.lyr.Invalidate()
		}
	}
}

// ---------------------------------------------------------------------------
// windows / actions
// ---------------------------------------------------------------------------

func (a *App) onAction(act int) {
	switch act {
	case ui.ActList:
		a.togglePlaylist()
	case ui.ActLyric:
		a.toggleLyrics()
	case ui.ActComment:
		a.toggleComments()
	case ui.ActAccount:
		a.openLogin()
	case ui.ActCover:
		// clicking the cover opens the playlist, like TTPlayer's album jump
		a.togglePlaylist()
	}
}

// Act aliases keep the app code readable while the constants live in ui.

func (a *App) ref() w32.RECT { return w32.GetWindowRect(a.Main.HWND()) }

func (a *App) togglePlaylist() {
	if a.list.Visible {
		a.list.Hide()
		return
	}
	a.list.EnsureEdit()
	a.list.Place(a.ref())
	a.list.Show()
	if a.profile != nil && len(a.playlists) == 0 {
		go a.loadPlaylists()
	}
}

func (a *App) toggleLyrics() {
	if a.lyr.Visible {
		a.lyr.Hide()
		return
	}
	a.lyr.Place(a.ref())
	a.lyr.Show()
	a.lyricCatchUp()
}

func (a *App) toggleComments() {
	if a.comm.Visible {
		a.comm.Hide()
		return
	}
	if a.cur.ID == 0 {
		return
	}
	a.comm.Place(a.ref())
	a.comm.Show()
	a.comm.SetSong(a.cur.Name)
	a.comm.Request()
}

// ---------------------------------------------------------------------------
// data loading
// ---------------------------------------------------------------------------

func (a *App) bootstrap() {
	prof, err := a.cl.Account()
	a.post(func() {
		if err != nil || prof == nil {
			a.profile = nil
			a.updateAccountLabel()
			a.openLogin()
			return
		}
		a.profile = prof
		a.cfg.UID = prof.UserID
		a.cfg.Nickname = prof.Nickname
		a.updateAccountLabel()
		go a.loadPlaylists()
	})
}

func (a *App) updateAccountLabel() {
	if a.profile != nil {
		a.Main.AccountLabel = a.profile.Nickname
	} else {
		a.Main.AccountLabel = "登录"
	}
	a.Main.Invalidate()
}

func (a *App) loadPlaylists() {
	if a.profile == nil {
		return
	}
	pls, err := a.cl.UserPlaylists(a.profile.UserID)
	if err != nil {
		return
	}
	a.post(func() {
		a.playlists = pls
		vms := make([]ui.PlaylistVM, 0, len(pls)+1)
		vms = append(vms, ui.PlaylistVM{Name: "播放队列", Count: len(a.queue), Queue: true})
		for _, p := range pls {
			vms = append(vms, ui.PlaylistVM{Name: p.Name, Count: p.Count})
		}
		a.list.SetPlaylists(vms)
		// open 我喜欢的音乐 by default
		if len(pls) > 0 {
			a.loadPlaylistTracks(pls[0].ID, pls[0].Name)
			a.list.SelectPlaylist(1)
		}
	})
}

func (a *App) loadPlaylistTracks(id int64, name string) {
	go func() {
		plName, tracks, err := a.cl.PlaylistTracks(id)
		if err != nil {
			return
		}
		if plName == "" {
			plName = name
		}
		a.post(func() {
			a.viewTracks = tracks
			a.viewIsQueue = false
			a.viewHeader = fmt.Sprintf("%s  ·  %d 首", plName, len(tracks))
			a.list.SetTracks(a.trackVMs(tracks), a.viewHeader, false)
		})
	}()
}

func (a *App) showQueue() {
	a.viewTracks = a.queue
	a.viewIsQueue = true
	a.viewHeader = fmt.Sprintf("播放队列  ·  %d 首", len(a.queue))
	a.list.SetTracks(a.trackVMs(a.queue), a.viewHeader, true)
	a.list.MarkPlaying(a.queueAt)
}

func (a *App) trackVMs(tracks []netease.Track) []ui.TrackVM {
	vms := make([]ui.TrackVM, len(tracks))
	for i, t := range tracks {
		vms[i] = ui.TrackVM{
			Title:   t.Name,
			Artist:  t.Artists,
			Dur:     t.Duration,
			Playing: a.cur.ID == t.ID,
		}
	}
	return vms
}

func (a *App) wire() {
	a.list.OnPlaylistSelect = func(idx int) {
		if idx == 0 {
			a.showQueue()
			return
		}
		i := idx - 1
		if i < 0 || i >= len(a.playlists) {
			return
		}
		a.loadPlaylistTracks(a.playlists[i].ID, a.playlists[i].Name)
	}
	a.list.OnTrackActivate = func(idx int) {
		if idx < 0 || idx >= len(a.viewTracks) {
			return
		}
		if !a.viewIsQueue {
			// adopt the browsed playlist as the queue, starting at this row
			a.queue = append([]netease.Track(nil), a.viewTracks...)
			a.queueAt = idx
			a.playCurrent()
			a.refreshQueueVMs()
		} else {
			a.queueAt = idx
			a.playCurrent()
		}
	}
	a.list.OnSearch = func(kw string) {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			return
		}
		go func() {
			tracks, err := a.cl.Search(kw, 40)
			if err != nil {
				return
			}
			a.post(func() {
				a.viewTracks = tracks
				a.viewIsQueue = false
				a.viewHeader = fmt.Sprintf("搜索「%s」 · %d 首", kw, len(tracks))
				a.list.SetTracks(a.trackVMs(tracks), a.viewHeader, false)
			})
		}()
	}
	a.list.OnQueueAction = func(kind int) {
		sel := a.list.SelectedTrack()
		switch kind {
		case 0: // move up
			if a.viewIsQueue && sel > 0 {
				a.queue[sel-1], a.queue[sel] = a.queue[sel], a.queue[sel-1]
				if a.queueAt == sel {
					a.queueAt--
				} else if a.queueAt == sel-1 {
					a.queueAt++
				}
				a.showQueue()
				a.list.SelectTrack(sel - 1)
			}
		case 1: // move down
			if a.viewIsQueue && sel >= 0 && sel < len(a.queue)-1 {
				a.queue[sel+1], a.queue[sel] = a.queue[sel], a.queue[sel+1]
				if a.queueAt == sel {
					a.queueAt++
				} else if a.queueAt == sel+1 {
					a.queueAt--
				}
				a.showQueue()
				a.list.SelectTrack(sel + 1)
			}
		case 2: // remove
			if a.viewIsQueue && sel >= 0 && sel < len(a.queue) {
				a.queue = append(a.queue[:sel], a.queue[sel+1:]...)
				if a.queueAt >= len(a.queue) {
					a.queueAt = len(a.queue) - 1
				}
				if a.queueAt < 0 {
					a.queueAt = 0
				}
				a.showQueue()
			}
		case 10: // add the selected browsed row to the queue
			if sel >= 0 && sel < len(a.viewTracks) {
				a.queue = append(a.queue, a.viewTracks[sel])
				a.refreshQueueVMs()
			}
		}
	}

	a.lyr.OnSeek = func(frac float64) { a.Seek(frac) }
	a.lyr.Duration = func() float64 { return a.Duration() }
	a.lyr.Position = func() float64 { return a.pl.Position() }

	a.comm.OnRequestHot = func() { a.loadCommentsPage(true, 0, 20) }
	a.comm.OnRequestPage = func(offset, limit int) { a.loadCommentsPage(false, offset, limit) }

	a.login.OnRequestQR = func() { a.requestQR() }
	a.login.OnPhoneLogin = func(phone, pw string) { a.phoneLogin(phone, pw) }
}

func (a *App) refreshQueueVMs() {
	if a.viewIsQueue {
		a.showQueue()
	}
}

// ---------------------------------------------------------------------------
// comments
// ---------------------------------------------------------------------------

func (a *App) loadCommentsFor(t netease.Track) {
	a.post(func() {
		a.commSong = t.ID
		if a.comm.Visible {
			a.comm.SetSong(t.Name)
			a.comm.Request()
		}
	})
}

func (a *App) loadCommentsPage(hot bool, offset, limit int) {
	songID := a.commSong
	if songID == 0 {
		a.comm.SetError("没有正在播放的歌曲")
		return
	}
	go func() {
		page, err := a.cl.Comments(songID, offset, limit)
		a.post(func() {
			if songID != a.commSong {
				return
			}
			if err != nil {
				a.comm.SetError("加载失败：" + friendly(err))
				return
			}
			if hot {
				vms := make([]ui.CommentVM, 0, len(page.Hot))
				for _, c := range page.Hot {
					vms = append(vms, toCommentVM(c))
				}
				a.comm.SetTotal(page.Total)
				a.comm.SetHot(vms)
			} else {
				vms := make([]ui.CommentVM, 0, len(page.List))
				for _, c := range page.List {
					vms = append(vms, toCommentVM(c))
				}
				a.comm.SetPage(vms, page.Total)
			}
		})
	}()
}

func toCommentVM(c netease.Comment) ui.CommentVM {
	content := c.Content
	if c.Replied != "" {
		content = "回复 " + c.Replied + "：" + content
	}
	return ui.CommentVM{
		User:    c.User,
		Content: content,
		TimeStr: ui.FormatRelativeTime(c.TimeSec),
		Likes:   c.Liked,
		Hot:     c.Hot,
	}
}

// ---------------------------------------------------------------------------
// login
// ---------------------------------------------------------------------------

func (a *App) openLogin() {
	if a.profile != nil {
		// already logged in: show the window as an account panel
		a.login.SetStatus("已登录："+a.profile.Nickname, false)
	}
	a.login.Reset()
	a.login.Place(a.ref())
	a.login.ShowQR()
	a.requestQR()
}

func (a *App) requestQR() {
	gen := a.loginGen.Add(1)
	go func() {
		key, url, err := a.cl.QRKey()
		if err != nil {
			a.post(func() { a.login.SetQRState("获取二维码失败：" + friendly(err)) })
			return
		}
		dib, err := renderQR(url)
		if err != nil {
			a.post(func() { a.login.SetQRState("二维码渲染失败") })
			return
		}
		a.post(func() {
			if gen != a.loginGen.Load() {
				dib.Free()
				return
			}
			a.login.SetQR(dib)
			a.login.SetQRState("请使用网易云音乐 App 扫码")
		})
		a.pollQR(key, gen)
	}()
}

func (a *App) pollQR(key string, gen int32) {
	for i := 0; i < 90; i++ {
		time.Sleep(2 * time.Second)
		if gen != a.loginGen.Load() {
			return
		}
		st, err := a.cl.QRCheck(key)
		if err != nil {
			a.post(func() { a.login.SetQRState("轮询失败：" + friendly(err)) })
			return
		}
		switch st {
		case 800:
			a.post(func() { a.login.SetQRState("二维码已过期，请刷新") })
			return
		case 801:
			// still waiting
		case 802:
			a.post(func() { a.login.SetQRState("已扫码，请在手机上确认登录") })
		case 803:
			a.post(func() { a.login.SetQRState("登录成功") })
			a.afterLogin()
			return
		}
	}
}

func (a *App) phoneLogin(phone, pw string) {
	if phone == "" || pw == "" {
		a.login.SetStatus("请输入手机号和密码", false)
		return
	}
	a.login.SetStatus("登录中…", true)
	go func() {
		prof, err := a.cl.LoginCellphone(phone, pw, "86")
		if err != nil {
			a.post(func() { a.login.SetStatus(friendly(err), false) })
			return
		}
		a.post(func() {
			a.profile = prof
			a.login.SetStatus("登录成功", false)
		})
		a.afterLogin()
	}()
}

func (a *App) afterLogin() {
	prof, err := a.cl.Account()
	a.post(func() {
		if err != nil || prof == nil {
			a.login.SetStatus("登录状态校验失败", false)
			return
		}
		a.profile = prof
		a.cfg.UID = prof.UserID
		a.cfg.Nickname = prof.Nickname
		a.cfg.SetCookies(a.cl.Cookies())
		a.updateAccountLabel()
		a.login.Hide()
		go a.loadPlaylists()
	})
}

// ---------------------------------------------------------------------------
// shutdown
// ---------------------------------------------------------------------------

func (a *App) shutdown() {
	r := w32.GetWindowRect(a.Main.HWND())
	a.cfg.MainX, a.cfg.MainY = r.Left, r.Top
	a.cfg.Volume = a.pl.Volume()
	a.cfg.Mode = a.mode
	_ = a.cfg.Save()
	a.pl.Close()
	if a.curCover != nil {
		a.curCover.Free()
		a.curCover = nil
	}
	ui.T.Free()
	w32.PostQuitMessage()
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func renderQR(text string) (*w32.DIB, error) {
	pngBytes, err := qrcode.Encode(text, qrcode.Medium, 210)
	if err != nil {
		return nil, err
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, err
	}
	dib := w32.ImageToDIB(img, 210, 210)
	if dib == nil {
		return nil, errors.New("dib")
	}
	return dib, nil
}

func friendly(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	switch {
	case errors.Is(err, netease.ErrNoURL):
		return "该歌曲无版权或需要 VIP"
	case strings.Contains(s, "timeout"):
		return "网络超时"
	case strings.Contains(s, "dial tcp"):
		return "网络不可用"
	}
	if len(s) > 60 {
		s = s[:60] + "…"
	}
	return s
}

func stripQuery(s string) string {
	if i := strings.IndexByte(s, '?'); i >= 0 {
		return s[:i]
	}
	return s
}
