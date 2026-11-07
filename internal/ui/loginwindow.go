package ui

import (
	"netmusic/internal/w32"
)

// LoginWindow offers both login paths NetEase supports for desktop clients:
// scan a QR code with the phone app, or type the phone number + password.
type LoginWindow struct {
	*Panel

	tab     int // 0 = 扫码, 1 = 手机号
	qr      *w32.DIB
	qrState string
	status  string
	busy    bool

	phoneEdit uintptr
	pwEdit    uintptr

	btnTabQR    w32.RECT
	btnTabPhone w32.RECT
	btnRefresh  w32.RECT
	btnLogin    w32.RECT
	hoverBtn    int

	OnRequestQR  func()               // ask for a fresh key + QR image
	OnPhoneLogin func(phone, pw string) // submit credentials
}

func NewLoginWindow() *LoginWindow {
	lw := &LoginWindow{}
	lw.Panel = NewPanel("登录", dp(340), dp(430))
	lw.Panel.Body = lw.body
	lw.Panel.OnMsg = lw.onMsg
	lw.Panel.OnClose = func() {}
	return lw
}

func (lw *LoginWindow) layout() {
	lw.btnTabQR = rc(dp(10), dp(31), dp(10) + dp(70), dp(31) + dp(22))
	lw.btnTabPhone = rc(dp(84), dp(31), dp(84) + dp(70), dp(31) + dp(22))
	lw.btnRefresh = rc(lw.W/2 - dp(40), dp(360), lw.W/2 + dp(40), dp(360) + dp(22))
	lw.btnLogin = rc(lw.W/2 - dp(50), dp(330), lw.W/2 + dp(50), dp(330) + dp(24))

	if lw.editReady() {
		ex := lw.W/2 - dp(100)
		w32.MoveWindow(lw.phoneEdit, ex, dp(120), dp(200), dp(22), true)
		w32.MoveWindow(lw.pwEdit, ex, dp(156), dp(200), dp(22), true)
	}
}

func (lw *LoginWindow) editReady() bool { return lw.phoneEdit != 0 && lw.pwEdit != 0 }

func (lw *LoginWindow) ensureEdits() {
	if lw.editReady() {
		return
	}
	ex := lw.W/2 - dp(100)
	lw.phoneEdit = NewEdit(lw.HWnd, 201, ex, dp(120), dp(200), dp(22), "手机号")
	lw.pwEdit = NewPasswordEdit(lw.HWnd, 202, ex, dp(156), dp(200), dp(22), "密码")
	if lw.phoneEdit != 0 {
		SetEditEnter(lw.phoneEdit, lw.submitPhone)
	}
	if lw.pwEdit != 0 {
		SetEditEnter(lw.pwEdit, lw.submitPhone)
	}
}

func (lw *LoginWindow) submitPhone() {
	if lw.OnPhoneLogin == nil || !lw.editReady() {
		return
	}
	lw.OnPhoneLogin(EditText(lw.phoneEdit), EditText(lw.pwEdit))
}

// SetQR installs a freshly rendered QR bitmap (ownership transfers).
func (lw *LoginWindow) SetQR(dib *w32.DIB) {
	if lw.qr != nil {
		lw.qr.Free()
	}
	lw.qr = dib
	lw.Panel.Invalidate()
}

func (lw *LoginWindow) SetQRState(s string) {
	lw.qrState = s
	lw.Panel.Invalidate()
}

func (lw *LoginWindow) SetStatus(s string, busy bool) {
	lw.status = s
	lw.busy = busy
	lw.Panel.Invalidate()
}

// Reset prepares the window for a new login attempt.
func (lw *LoginWindow) Reset() {
	lw.qrState = "正在获取二维码…"
	lw.status = ""
	lw.tab = 0
	lw.Panel.Invalidate()
}

func (lw *LoginWindow) ShowQR() {
	lw.tab = 0
	lw.ensureEdits()
	lw.hideEdits(true)
	lw.Panel.Show()
}

func (lw *LoginWindow) hideEdits(hide bool) {
	if !lw.editReady() {
		return
	}
	cmd := uintptr(w32.SW_SHOW)
	if hide {
		cmd = w32.SW_HIDE
	}
	// SW_HIDE for QR tab
	if lw.tab == 0 {
		cmd = w32.SW_HIDE
	}
	w32.ShowWindow(lw.phoneEdit, cmd)
	w32.ShowWindow(lw.pwEdit, cmd)
}

func (lw *LoginWindow) body(dc w32.HDC, client w32.RECT) {
	lw.layout()
	drawChip(dc, lw.btnTabQR, "扫码登录", lw.hoverBtn == 1, lw.tab == 0)
	drawChip(dc, lw.btnTabPhone, "手机号登录", lw.hoverBtn == 2, lw.tab == 1)

	if lw.tab == 0 {
		// QR frame
		side := dp(210)
		x := (lw.W - side) / 2
		y := dp(64)
		r := rc(x, y, x + side, y + side)
		fill(dc, r, w32.RGB(0xFF, 0xFF, 0xFF))
		if lw.qr != nil {
			lw.qr.Blit(dc, r.Left, r.Top)
		}
		strokeRect(dc, r, T.Border)
		st := lw.qrState
		if st == "" {
			st = "请使用网易云音乐 App 扫码"
		}
		drawTextIn(dc, rc(dp(10), y + side + dp(8), lw.W - dp(10), y + side + dp(28)), st,
			textOpt{Font: T.FontS, Color: T.TextMid, Align: w32.DT_CENTER})
		drawChip(dc, lw.btnRefresh, "刷新二维码", lw.hoverBtn == 3, false)
	} else {
		drawTextIn(dc, rc(dp(10), dp(96), lw.W - dp(10), dp(118)), "手机号", textOpt{Font: T.FontXS, Color: T.TextDim})
		drawTextIn(dc, rc(dp(10), dp(136), lw.W - dp(10), dp(156)), "密码", textOpt{Font: T.FontXS, Color: T.TextDim})
		drawChip(dc, lw.btnLogin, "登录", lw.hoverBtn == 4, false)
	}

	if lw.status != "" {
		drawTextIn(dc, rc(dp(10), lw.H - dp(34), lw.W - dp(10), lw.H - dp(12)), lw.status,
			textOpt{Font: T.FontXS, Color: T.AccentDark, Align: w32.DT_CENTER})
	}
}

func (lw *LoginWindow) hitBtn(x, y int32) int {
	switch {
	case inRect(lw.btnTabQR, x, y):
		return 1
	case inRect(lw.btnTabPhone, x, y):
		return 2
	case lw.tab == 0 && inRect(lw.btnRefresh, x, y):
		return 3
	case lw.tab == 1 && inRect(lw.btnLogin, x, y):
		return 4
	}
	return 0
}

func (lw *LoginWindow) onMsg(msg uint32, wp, lp uintptr) (bool, uintptr) {
	x, y := lparamXY(lp)
	switch msg {
	case w32.WM_CTLCOLOREDIT:
		hdc := w32.HDC(wp)
		w32.SetBkColor(hdc, T.PanelAlt)
		w32.SetTextColor(hdc, T.TextHi)
		return true, uintptr(T.BrushHandle(T.PanelAlt))
	case w32.WM_MOUSEMOVE:
		hv := lw.hitBtn(x, y)
		if hv != lw.hoverBtn {
			lw.hoverBtn = hv
			w32.Invalidate(lw.HWnd)
		}
		return true, 0
	case w32.WM_MOUSELEAVE:
		lw.hoverBtn = 0
		w32.Invalidate(lw.HWnd)
		return true, 0
	case w32.WM_LBUTTONUP:
		switch lw.hitBtn(x, y) {
		case 1:
			lw.tab = 0
			lw.hideEdits(true)
			if lw.qr == nil && lw.OnRequestQR != nil {
				lw.qrState = "正在获取二维码…"
				lw.OnRequestQR()
			}
		case 2:
			lw.tab = 1
			lw.ensureEdits()
			lw.hideEdits(false)
			w32.SetFocus(lw.phoneEdit)
		case 3:
			if lw.OnRequestQR != nil {
				lw.qrState = "正在获取二维码…"
				lw.OnRequestQR()
			}
		case 4:
			lw.submitPhone()
		}
		w32.Invalidate(lw.HWnd)
		return true, 0
	}
	return false, 0
}
