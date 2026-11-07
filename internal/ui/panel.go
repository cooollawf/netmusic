package ui

import (
	"netmusic/internal/w32"
)

// Panel is the shared chrome for the secondary windows: frameless, dark
// gradient, a 25 px title strip that drags the window, and a close button.
// Sub-windows only implement Body painting and their own hit testing.
type Panel struct {
	HWnd  uintptr
	class *w32.Class
	buf   w32.BackBuffer

	Title   string
	W, H    int32
	Visible bool
	// Cascade staggers windows that share the same anchor so they do not
	// stack on top of each other.
	Cascade int

	OnClose func()
	Body    func(dc w32.HDC, client w32.RECT)
	OnMsg   func(msg uint32, wp, lp uintptr) (bool, uintptr)
	OnTick  func()

	hover      int // 1 == close button
	pressed    int
}

func NewPanel(title string, w, h int32) *Panel {
	p := &Panel{Title: title, W: w, H: h}
	p.class = w32.RegisterClass(w32.CS_DBLCLKS, w32.LoadCursor(w32.IDC_ARROW))
	p.HWnd = p.class.Create(w32.WindowSpec{
		Title: title,
		W:     w,
		H:     h,
		Style: w32.WS_POPUP | w32.WS_CLIPCHILDREN,
		// Tool windows stay out of the taskbar; owning them to the main window
		// makes them minimise and close together with it instead of lingering.
		ExStyle: w32.WS_EX_TOOLWINDOW,
		Owner:   mainOwner,
		Handler: p,
	})
	return p
}

// Place positions the panel just below/right of a reference rect, clamped to
// the screen and staggered by Cascade so windows never fully overlap.
func (p *Panel) Place(ref w32.RECT) {
	sw := w32.GetSystemMetrics(0)
	sh := w32.GetSystemMetrics(1)
	off := int32(p.Cascade) * dp(16)
	x := ref.Left + off
	y := ref.Bottom + dp(4) + off
	if x+p.W > sw {
		x = sw - p.W - dp(8)
	}
	if y+p.H > sh-dp(40) {
		y = ref.Top - p.H - dp(4)
	}
	if y < 0 {
		y = dp(8)
	}
	if x < 0 {
		x = dp(8)
	}
	w32.MoveWindow(p.HWnd, x, y, p.W, p.H, true)
}

func (p *Panel) Show() {
	p.Visible = true
	w32.Show(p.HWnd, w32.SW_SHOW)
	w32.SetForeground(p.HWnd)
	w32.Invalidate(p.HWnd)
}

func (p *Panel) Hide() {
	p.Visible = false
	w32.Show(p.HWnd, w32.SW_HIDE)
}

func (p *Panel) Toggle() {
	if p.Visible {
		p.Hide()
	} else {
		p.Show()
	}
}

func (p *Panel) Invalidate() { w32.Invalidate(p.HWnd) }

func (p *Panel) closeRect() w32.RECT {
	w := dp(16)
	return rc(p.W-dp(8)-w, dp(6), p.W-dp(8), dp(19))
}

func (p *Panel) titleBar() w32.RECT { return rc(0, 0, p.W, dp(25)) }

func (p *Panel) OnCreate(hwnd uintptr) bool {
	p.HWnd = hwnd
	return true
}

func (p *Panel) OnDestroy(hwnd uintptr) {
	p.buf.Free()
}

func (p *Panel) OnPaint(hwnd uintptr, hdc w32.HDC, client w32.RECT) {
	w, h := client.Dx(), client.Dy()
	bdc, _, _ := p.buf.DC(w, h)
	if bdc == 0 {
		return
	}
	// background
	w32.GradientV(bdc, rc(0, 0, w, h), T.BgTop, T.BgBottom)
	// title strip
	tb := p.titleBar()
	w32.GradientV(bdc, tb, T.TitleTop, T.TitleBtm)
	hline(bdc, 0, w, tb.Bottom-1, T.Hairline)
	strokeRect(bdc, rc(0, 0, w, h), T.Border)
	// title text with the accent dot, matching the main window
	fillXY(bdc, dp(9), dp(9), dp(5), dp(5), T.Accent)
	drawTextIn(bdc, rc(dp(19), dp(1), w - dp(30), dp(24)), p.Title, textOpt{Font: T.FontS, Color: T.TextMid})
	// close button
	cr := p.closeRect()
	if p.hover == 1 {
		roundFill(bdc, cr, dp(3), T.Hairline)
	}
	iconClose(bdc, cr, T.TextMid)

	if p.Body != nil {
		p.Body(bdc, rc(0, tb.Bottom, w, h))
	}
	p.buf.Flush(hdc, w, h)
}

func (p *Panel) OnMessage(hwnd uintptr, msg uint32, wp, lp uintptr) (bool, uintptr) {
	switch msg {
	case w32.WM_NCHITTEST:
		x := int32(int16(lp & 0xffff))
		y := int32(int16((lp >> 16) & 0xffff))
		pt := w32.POINT{X: x, Y: y}
		w32.ScreenToClient(hwnd, &pt)
		if inRect(p.titleBar(), pt.X, pt.Y) {
			if inRect(p.closeRect(), pt.X, pt.Y) {
				return true, w32.HTCLIENT
			}
			return true, w32.HTCAPTION
		}
		return true, w32.HTCLIENT
	case w32.WM_MOUSEMOVE:
		x := int32(int16(lp & 0xffff))
		y := int32(int16((lp >> 16) & 0xffff))
		hv := 0
		if inRect(p.closeRect(), x, y) {
			hv = 1
		}
		if hv != p.hover {
			p.hover = hv
			w32.Invalidate(hwnd)
		}
		w32.TrackMouseLeave(hwnd)
	case w32.WM_MOUSELEAVE:
		if p.hover != 0 {
			p.hover = 0
			w32.Invalidate(hwnd)
		}
	case w32.WM_LBUTTONDOWN:
		x := int32(int16(lp & 0xffff))
		y := int32(int16((lp >> 16) & 0xffff))
		if inRect(p.closeRect(), x, y) {
			p.pressed = 1
		}
		w32.SetFocus(hwnd)
	case w32.WM_LBUTTONUP:
		x := int32(int16(lp & 0xffff))
		y := int32(int16((lp >> 16) & 0xffff))
		if p.pressed == 1 && inRect(p.closeRect(), x, y) {
			p.pressed = 0
			p.Hide()
			if p.OnClose != nil {
				p.OnClose()
			}
			return true, 0
		}
		p.pressed = 0
	case w32.WM_LBUTTONDBLCLK:
		if inRect(p.titleBar(), int32(int16(lp&0xffff)), int32(int16((lp>>16)&0xffff))) {
			p.Hide()
			if p.OnClose != nil {
				p.OnClose()
			}
			return true, 0
		}
	case w32.WM_CLOSE:
		p.Hide()
		return true, 0
	}
	if p.OnMsg != nil {
		if handled, res := p.OnMsg(msg, wp, lp); handled {
			return true, res
		}
	}
	return false, 0
}

// helper to convert MAKEPOINTS lParam into client coordinates
func lparamXY(lp uintptr) (int32, int32) {
	return int32(int16(lp & 0xffff)), int32(int16((lp >> 16) & 0xffff))
}
