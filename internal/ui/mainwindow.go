package ui

import (
	"fmt"

	"netmusic/internal/w32"
)

// TrackInfo is the view model for the currently loaded song. It is rebuilt on
// song change only; painting never allocates strings.
type TrackInfo struct {
	Title  string
	Artist string
	Album  string
	Cover  *w32.DIB
}

// Controller is implemented by the player core. The UI only ever talks to this
// interface, which keeps the Win32 layer free of business logic.
type Controller interface {
	IsPlaying() bool
	Position() float64 // seconds
	Duration() float64 // seconds
	Volume() int       // 0..100
	Mode() int         // 0=顺序 1=单曲 2=随机
	Track() TrackInfo
	Toggle()
	Next()
	Prev()
	Seek(frac float64)
	SetVolume(v int)
	CycleMode()
}

// action ids for the main window's custom buttons.
const (
	actNone = iota
	actPin
	actMin
	actClose
	actPrev
	actPlay
	actNext
	actMode
	actVol
	actProgress
	actList
	actLyric
	actComment
	actCover
	actAccount
)

// Exported action ids so the application layer can react to the custom
// buttons without duplicating the layout knowledge.
const (
	ActNone     = actNone
	ActPin      = actPin
	ActMin      = actMin
	ActClose    = actClose
	ActPrev     = actPrev
	ActPlay     = actPlay
	ActNext     = actNext
	ActMode     = actMode
	ActVol      = actVol
	ActProgress = actProgress
	ActList     = actList
	ActLyric    = actLyric
	ActComment  = actComment
	ActCover    = actCover
	ActAccount  = actAccount
)

// CoverSize is the album art edge length in physical pixels.
func CoverSize() int32 { return dp(98) }

// mainOwner is the main window handle; helper panels are created owned by it.
var mainOwner uintptr

// WMDispatch is posted by worker goroutines to run a closure on the UI thread.
const WMDispatch = w32.WM_APP + 100

type mainLayout struct {
	TitleBar  w32.RECT
	Pin       w32.RECT
	Min       w32.RECT
	Close     w32.RECT
	Account   w32.RECT
	Cover     w32.RECT
	Title     w32.RECT
	Artist    w32.RECT
	Album     w32.RECT
	Progress  w32.RECT
	Time      w32.RECT
	Prev      w32.RECT
	Play      w32.RECT
	Next      w32.RECT
	Mode      w32.RECT
	VolIcon   w32.RECT
	VolSlider w32.RECT
	ChipList  w32.RECT
	ChipLyric w32.RECT
	ChipComm  w32.RECT
}

// MainWindow is the TTPlayer style 主控窗口: frameless, custom painted, with
// the transport controls laid out the way 千千静听 did it.
type MainWindow struct {
	hwnd    uintptr
	class   *w32.Class
	buf     w32.BackBuffer
	ctl     Controller
	OnOpen  func(act int)
	OnQuit  func()
	OnTick  func()
	// OnDispatch drains work queued by background goroutines.
	OnDispatch func()
	// AccountLabel is shown in the title strip ("登录" when logged out).
	AccountLabel string
	AccountHot   bool

	hover   int
	pressed int
	seeking bool
	pinned  bool
}

func NewMainWindow(ctl Controller) *MainWindow {
	mw := &MainWindow{ctl: ctl}
	mw.class = w32.RegisterClass(w32.CS_DBLCLKS, w32.LoadCursor(w32.IDC_ARROW))
	w, h := dp(420), dp(152)
	mw.hwnd = mw.class.Create(w32.WindowSpec{
		Title:   "netmusic",
		W:       w,
		H:       h,
		Style:   w32.WS_POPUP | w32.WS_SYSMENU | w32.WS_MINIMIZEBOX | w32.WS_CLIPCHILDREN,
		// WS_EX_APPWINDOW is what puts the window in the taskbar and Alt+Tab.
		// WS_POPUP windows get neither by default, and WS_EX_TOOLWINDOW (the
		// value used for the helper panels) explicitly removes them.
		ExStyle: w32.WS_EX_APPWINDOW,
		Handler: mw,
		Center:  true,
	})
	// Helper panels are owned by the main window: minimising it hides them and
	// closing it takes them with it, instead of leaving orphan tool windows.
	mainOwner = mw.hwnd
	return mw
}

func (mw *MainWindow) HWND() uintptr { return mw.hwnd }

// Invalidate schedules a repaint of the main window.
func (mw *MainWindow) Invalidate() { w32.Invalidate(mw.hwnd) }

func (mw *MainWindow) Show() {
	w32.Show(mw.hwnd, w32.SW_SHOW)
	w32.Update(mw.hwnd)
}

func (mw *MainWindow) layout() mainLayout {
	var l mainLayout
	w := dp(420)
	// titlebar
	l.TitleBar = rc(0, 0, w, dp(25))
	btnTop := dp(6)
	btnH := dp(13)
	bw := dp(16)
	right := w - dp(6)
	l.Close = rc(right - bw, btnTop, right, btnTop + btnH)
	right -= bw + dp(3)
	l.Min = rc(right - bw, btnTop, right, btnTop + btnH)
	right -= bw + dp(3)
	l.Pin = rc(right - bw, btnTop, right, btnTop + btnH)
	l.Account = rc(dp(80), btnTop, right - bw - dp(6), btnTop + btnH)

	// cover
	cx, cy, cs := dp(10), dp(32), dp(98)
	l.Cover = rc(cx, cy, cx + cs, cy + cs)

	// info block
	ix := cx + cs + dp(12)
	iw := w - dp(10) - ix
	l.Title = rc(ix, cy + dp(1), ix + iw, cy + dp(23))
	l.Artist = rc(ix, cy + dp(24), ix + iw, cy + dp(42))
	l.Album = rc(ix, cy + dp(42), ix + iw, cy + dp(58))
	py := cy + dp(64)
	l.Progress = rc(ix, py, ix + iw, py + dp(10))
	l.Time = rc(ix, py + dp(10), ix + iw, py + dp(24))

	// transport row
	ry := dp(120)
	l.Prev = rc(ix, ry, ix + dp(26), ry + dp(26))
	l.Play = rc(ix + dp(28), ry - dp(2), ix + dp(28) + dp(30), ry + dp(28))
	l.Next = rc(ix + dp(60), ry, ix + dp(60) + dp(26), ry + dp(26))
	l.Mode = rc(ix + dp(90), ry + dp(4), ix + dp(90) + dp(22), ry + dp(22))
	l.VolIcon = rc(ix + dp(116), ry + dp(4), ix + dp(116) + dp(14), ry + dp(22))
	l.VolSlider = rc(ix + dp(132), ry + dp(4), ix + dp(132) + dp(52), ry + dp(22))

	// right chips
	chw, chh := dp(34), dp(20)
	chy := dp(123)
	x := w - dp(8) - chw
	l.ChipComm = rc(x, chy, x + chw, chy + chh)
	x -= chw + dp(3)
	l.ChipLyric = rc(x, chy, x + chw, chy + chh)
	x -= chw + dp(3)
	l.ChipList = rc(x, chy, x + chw, chy + chh)
	return l
}

func inRect(r w32.RECT, x, y int32) bool {
	return x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom
}

func (mw *MainWindow) hit(x, y int32) int {
	l := mw.layout()
	switch {
	case inRect(l.Close, x, y):
		return actClose
	case inRect(l.Min, x, y):
		return actMin
	case inRect(l.Pin, x, y):
		return actPin
	case inRect(l.Account, x, y):
		return actAccount
	case inRect(l.Prev, x, y):
		return actPrev
	case inRect(l.Play, x, y):
		return actPlay
	case inRect(l.Next, x, y):
		return actNext
	case inRect(l.Mode, x, y):
		return actMode
	case inRect(l.VolSlider, x, y):
		return actVol
	case inRect(l.Cover, x, y):
		return actCover
	case inRect(l.ChipList, x, y):
		return actList
	case inRect(l.ChipLyric, x, y):
		return actLyric
	case inRect(l.ChipComm, x, y):
		return actComment
	}
	if inRect(l.Progress, x, y) || inRect(l.Time, x, y) {
		return actProgress
	}
	return actNone
}

func (mw *MainWindow) fire(act int) {
	switch act {
	case actClose:
		if mw.OnQuit != nil {
			mw.OnQuit()
			return
		}
		w32.Destroy(mw.hwnd)
	case actMin:
		w32.Show(mw.hwnd, w32.SW_MINIMIZE)
	case actPin:
		mw.pinned = !mw.pinned
		if mw.pinned {
			w32.SetWindowPos(mw.hwnd, w32.HWND_TOPMOST, 0, 0, 0, 0, w32.SWP_NOMOVE|w32.SWP_NOSIZE)
		} else {
			w32.SetWindowPos(mw.hwnd, w32.HWND_NOTOPMOST, 0, 0, 0, 0, w32.SWP_NOMOVE|w32.SWP_NOSIZE)
		}
		w32.Invalidate(mw.hwnd)
	case actPrev:
		mw.ctl.Prev()
	case actPlay:
		mw.ctl.Toggle()
		w32.Invalidate(mw.hwnd)
	case actNext:
		mw.ctl.Next()
	case actMode:
		mw.ctl.CycleMode()
		w32.Invalidate(mw.hwnd)
	default:
		if mw.OnOpen != nil {
			mw.OnOpen(act)
		}
	}
}

// OnCreate implements w32.Handler.
func (mw *MainWindow) OnCreate(hwnd uintptr) bool {
	mw.hwnd = hwnd
	w32.SetTimer(hwnd, 1, 250)
	return true
}

func (mw *MainWindow) OnDestroy(hwnd uintptr) {
	w32.KillTimer(hwnd, 1)
	mw.buf.Free()
	// Safety net: if the window is destroyed by any path other than the ✕
	// button (Alt+F4, taskbar close, WM_CLOSE), the message loop must still
	// end — otherwise the process would linger forever with no window.
	w32.PostQuitMessage()
}

func (mw *MainWindow) OnMessage(hwnd uintptr, msg uint32, wp, lp uintptr) (bool, uintptr) {
	switch msg {
	case w32.WM_CLOSE:
		// Route Alt+F4 / taskbar close through the same path as ✕ so the
		// session (cookies, volume, geometry) gets persisted.
		if mw.OnQuit != nil {
			mw.OnQuit()
			return true, 0
		}
		return false, 0
	case w32.WM_NCHITTEST:
		// Let Windows do the dragging/snapping for the title strip.
		x := int32(int16(lp & 0xffff))
		y := int32(int16((lp >> 16) & 0xffff))
		var p = w32.POINT{X: x, Y: y}
		w32.ScreenToClient(hwnd, &p)
		l := mw.layout()
		if p.Y < l.TitleBar.Bottom {
			if mw.hit(p.X, p.Y) == actNone && p.X < l.Pin.Left-dp(8) {
				return true, w32.HTCAPTION
			}
			return true, w32.HTCLIENT
		}
		return true, w32.HTCLIENT
	case w32.WM_MOUSEMOVE:
		x := int32(int16(lp & 0xffff))
		y := int32(int16((lp >> 16) & 0xffff))
		if mw.seeking {
			mw.seekTo(x)
			return true, 0
		}
		h := mw.hit(x, y)
		if h != mw.hover {
			mw.hover = h
			w32.Invalidate(hwnd)
		}
		// make sure we get told when the pointer leaves
		w32.TrackMouseLeave(hwnd)
		return true, 0
	case w32.WM_MOUSELEAVE:
		if mw.hover != actNone {
			mw.hover = actNone
			w32.Invalidate(hwnd)
		}
		return true, 0
	case w32.WM_LBUTTONDOWN:
		x := int32(int16(lp & 0xffff))
		y := int32(int16((lp >> 16) & 0xffff))
		a := mw.hit(x, y)
		mw.pressed = a
		if a == actProgress || a == actVol {
			mw.seeking = true
			w32.SetCapture(hwnd)
			mw.dragTo(a, x)
		}
		w32.SetFocus(hwnd)
		w32.Invalidate(hwnd)
		return true, 0
	case w32.WM_LBUTTONUP:
		x := int32(int16(lp & 0xffff))
		y := int32(int16((lp >> 16) & 0xffff))
		if mw.seeking {
			mw.seeking = false
			w32.ReleaseCapture()
			w32.Invalidate(hwnd)
			return true, 0
		}
		a := mw.hit(x, y)
		if a != actNone && a == mw.pressed {
			mw.pressed = actNone
			mw.fire(a)
			w32.Invalidate(hwnd)
			return true, 0
		}
		mw.pressed = actNone
		w32.Invalidate(hwnd)
		return true, 0
	case w32.WM_TIMER:
		if mw.OnTick != nil {
			mw.OnTick()
		}
		w32.Invalidate(hwnd)
		return true, 0
	case WMDispatch:
		if mw.OnDispatch != nil {
			mw.OnDispatch()
		}
		return true, 0
	case w32.WM_KEYDOWN:
		switch wp {
		case 0x20: // space
			mw.ctl.Toggle()
			w32.Invalidate(hwnd)
		case 0x25: // left
			mw.ctl.Prev()
		case 0x27: // right
			mw.ctl.Next()
		}
		return true, 0
	}
	return false, 0
}

func (mw *MainWindow) dragTo(act int, x int32) {
	l := mw.layout()
	switch act {
	case actProgress:
		r := l.Progress
		f := float64(x-r.Left) / float64(r.Dx())
		if f < 0 {
			f = 0
		}
		if f > 1 {
			f = 1
		}
		mw.ctl.Seek(f)
	case actVol:
		r := l.VolSlider
		v := int(float64(x-r.Left) / float64(r.Dx()) * 100)
		if v < 0 {
			v = 0
		}
		if v > 100 {
			v = 100
		}
		mw.ctl.SetVolume(v)
	}
}

func (mw *MainWindow) seekTo(x int32) {
	mw.dragTo(actProgress, x)
}

// OnPaint implements w32.Handler.
func (mw *MainWindow) OnPaint(hwnd uintptr, hdc w32.HDC, client w32.RECT) {
	w, h := client.Dx(), client.Dy()
	bdc, bw, bh := mw.buf.DC(w, h)
	if bdc == 0 {
		return
	}
	mw.draw(bdc, w, h)
	mw.buf.Flush(hdc, w, h)
	_ = bw
	_ = bh
}

func (mw *MainWindow) draw(dc w32.HDC, w, h int32) {
	l := mw.layout()
	// background
	w32.GradientV(dc, rc(0, 0, w, h), T.BgTop, T.BgBottom)
	// titlebar
	w32.GradientV(dc, l.TitleBar, T.TitleTop, T.TitleBtm)
	hline(dc, 0, w, l.TitleBar.Bottom-1, T.Hairline)
	// outer border
	strokeRect(dc, rc(0, 0, w, h), T.Border)

	// title text + accent dot
	fillXY(dc, dp(9), dp(9), dp(6), dp(6), T.Accent)
	drawTextIn(dc, rc(dp(20), dp(1), dp(78), dp(24)), "netmusic", textOpt{Font: T.FontS, Color: T.TextMid})

	// account chip
	label := mw.AccountLabel
	if label == "" {
		label = "登录"
	}
	ac := T.TextDim
	if mw.hover == actAccount {
		ac = T.Accent
	}
	drawTextIn(dc, l.Account, label, textOpt{Font: T.FontXS, Color: ac, Align: w32.DT_RIGHT})

	// window buttons
	mw.drawBtn(dc, l.Pin, actPin, iconPin)
	mw.drawBtn(dc, l.Min, actMin, iconMin)
	mw.drawBtn(dc, l.Close, actClose, iconClose)

	// cover
	mw.drawCover(dc, l.Cover)

	ti := mw.ctl.Track()
	drawTextIn(dc, l.Title, ti.Title, textOpt{Font: T.FontL, Color: T.TextHi})
	drawTextIn(dc, l.Artist, ti.Artist, textOpt{Font: T.FontS, Color: T.TextMid})
	album := ti.Album
	if album == "" {
		album = "—"
	}
	drawTextIn(dc, l.Album, album, textOpt{Font: T.FontXS, Color: T.TextDim})

	// progress
	dur := mw.ctl.Duration()
	pos := mw.ctl.Position()
	frac := 0.0
	if dur > 0 {
		frac = pos / dur
	}
	if frac > 1 {
		frac = 1
	}
	pr := l.Progress
	fillXY(dc, pr.Left, pr.Top+dp(3), pr.Dx(), dp(3), T.TrackBg)
	fw := int32(float64(pr.Dx()) * frac)
	if fw > 0 {
		fillXY(dc, pr.Left, pr.Top+dp(3), fw, dp(3), T.Accent)
	}
	// knob
	kx := pr.Left + fw
	if mw.hover == actProgress || mw.seeking {
		w32.SelectObject(dc, w32.HGDIOBJ(T.brush(T.Accent)))
		oldPen := w32.SelectObject(dc, w32.HGDIOBJ(w32.GetStockObject(w32.NULL_PEN)))
		w32.Ellipse(dc, kx-dp(4), pr.Top+dp(5)-dp(4), kx+dp(4), pr.Top+dp(5)+dp(4))
		w32.SelectObject(dc, oldPen)
	}
	drawTextIn(dc, l.Time, fmtTime(pos)+" / "+fmtTime(dur), textOpt{Font: T.FontXS, Color: T.TextDim})

	// transport
	mw.drawIconBtn(dc, l.Prev, actPrev, iconPrev)
	mw.drawIconBtn(dc, l.Play, actPlay, func(d w32.HDC, r w32.RECT, c uint32) {
		if mw.ctl.IsPlaying() {
			iconPause(d, r, c)
		} else {
			iconPlay(d, r, c)
		}
	})
	mw.drawIconBtn(dc, l.Next, actNext, iconNext)

	// mode chip
	modeText := [3]string{"顺序", "单曲", "随机"}[mw.ctl.Mode()%3]
	drawTextIn(dc, l.Mode, modeText, textOpt{Font: T.FontXS, Color: T.TextDim, Align: w32.DT_CENTER})

	// volume
	iconSpeaker(dc, l.VolIcon, T.TextDim)
	vr := l.VolSlider
	vy := vr.Top + vr.Dy()/2
	fillXY(dc, vr.Left, vy-dp(1), vr.Dx(), dp(2), T.TrackBg)
	vw := int32(float64(vr.Dx()) * float64(mw.ctl.Volume()) / 100)
	if vw > 0 {
		fillXY(dc, vr.Left, vy-dp(1), vw, dp(2), T.TextMid)
	}

	// chips
	mw.drawChip(dc, l.ChipList, actList, "列表")
	mw.drawChip(dc, l.ChipLyric, actLyric, "歌词")
	mw.drawChip(dc, l.ChipComm, actComment, "评论")
}

func (mw *MainWindow) drawBtn(dc w32.HDC, r w32.RECT, act int, icon func(w32.HDC, w32.RECT, uint32)) {
	if mw.hover == act {
		roundFill(dc, r, dp(3), T.Hairline)
	}
	icon(dc, r, T.TextMid)
}

func (mw *MainWindow) drawIconBtn(dc w32.HDC, r w32.RECT, act int, icon func(w32.HDC, w32.RECT, uint32)) {
	if mw.hover == act {
		roundFill(dc, r, dp(5), T.Hairline)
	}
	c := T.TextHi
	if act == actPlay && mw.ctl.IsPlaying() {
		c = T.Accent
	}
	icon(dc, r, c)
}

func (mw *MainWindow) drawChip(dc w32.HDC, r w32.RECT, act int, label string) {
	c := T.TextDim
	if mw.hover == act {
		roundFill(dc, r, dp(4), T.AccentSoft)
		c = T.Accent
	}
	drawTextIn(dc, r, label, textOpt{Font: T.FontXS, Color: c, Align: w32.DT_CENTER})
}

func (mw *MainWindow) drawCover(dc w32.HDC, r w32.RECT) {
	ti := mw.ctl.Track()
	if ti.Cover != nil {
		ti.Cover.Blit(dc, r.Left, r.Top)
		strokeRect(dc, r, T.CoverEdge)
		return
	}
	fill(dc, r, T.CoverBg)
	strokeRect(dc, r, T.CoverEdge)
	iconNote(dc, r.Left+r.Dx()/2-dp(2), r.Top+r.Dy()/2, dp(30), T.Hairline)
}

func fmtTime(sec float64) string {
	if sec < 0 || sec != sec {
		sec = 0
	}
	s := int(sec)
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}
