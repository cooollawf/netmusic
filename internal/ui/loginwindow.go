package ui

import (
	"netmusic/internal/w32"
)

// LoginWindow offers the three ways NetEase allows a desktop client to sign in:
// scan a QR code, type the phone number + password, or phone + SMS code (the
// supported way past risk control when the password path returns 8821).
type LoginWindow struct {
	*Panel

	tab     int // 0 = 扫码, 1 = 手机号
	mode    int // 0 = 密码, 1 = 短信验证码
	qr      *w32.DIB
	qrState string
	status  string
	busy    bool

	phoneEdit uintptr
	pwEdit    uintptr
	codeEdit  uintptr

	btnTabQR    w32.RECT
	btnTabPhone w32.RECT
	btnMode     w32.RECT
	btnSendCode w32.RECT
	btnRefresh  w32.RECT
	btnLogin    w32.RECT
	hoverBtn    int

	OnRequestQR    func()                   // ask for a fresh key + QR image
	OnPhoneLogin   func(phone, pw string)   // password path
	OnSendCaptcha  func(phone string)       // request an SMS code
	OnCaptchaLogin func(phone, code string) // SMS code path
}

func NewLoginWindow() *LoginWindow {
	lw := &LoginWindow{}
	lw.Panel = NewPanel("登录", dp(340), dp(430))
	lw.Panel.Body = lw.body
	lw.Panel.OnMsg = lw.onMsg
	return lw
}

func (lw *LoginWindow) formX() (int32, int32) {
	ex := lw.W/2 - dp(100)
	return ex, dp(200)
}

func (lw *LoginWindow) layout() {
	lw.btnTabQR = rc(dp(10), dp(31), dp(10)+dp(70), dp(31)+dp(22))
	lw.btnTabPhone = rc(dp(84), dp(31), dp(84)+dp(70), dp(31)+dp(22))
	lw.btnMode = rc(dp(14), dp(196), dp(14)+dp(116), dp(196)+dp(22))
	lw.btnSendCode = rc(lw.W-dp(14)-dp(96), dp(196), lw.W-dp(14), dp(196)+dp(22))
	lw.btnRefresh = rc(lw.W/2-dp(46), dp(360), lw.W/2+dp(46), dp(360)+dp(22))
	lw.btnLogin = rc(lw.W/2-dp(60), dp(248), lw.W/2+dp(60), dp(248)+dp(26))

	if lw.editReady() {
		ex, ew := lw.formX()
		w32.MoveWindow(lw.phoneEdit, ex, dp(120), ew, dp(22), true)
		w32.MoveWindow(lw.pwEdit, ex, dp(158), ew, dp(22), true)
		w32.MoveWindow(lw.codeEdit, ex, dp(158), ew, dp(22), true)
		lw.syncEditVisibility()
	}
}

// syncEditVisibility keeps the native EDIT children in sync with the current
// tab/mode. Native child windows always paint over the parent, so an edit left
// visible on the QR tab shows up as a dark bar across the code - a regression
// worth a dedicated function.
func (lw *LoginWindow) syncEditVisibility() {
	show := func(h uintptr, visible bool) {
		if h == 0 {
			return
		}
		cmd := uintptr(w32.SW_HIDE)
		if visible {
			cmd = w32.SW_SHOW
		}
		w32.ShowWindow(h, cmd)
	}
	phoneTab := lw.tab == 1
	show(lw.phoneEdit, phoneTab)
	show(lw.pwEdit, phoneTab && lw.mode == 0)
	show(lw.codeEdit, phoneTab && lw.mode == 1)
}

func (lw *LoginWindow) editReady() bool {
	return lw.phoneEdit != 0 && lw.pwEdit != 0 && lw.codeEdit != 0
}

func (lw *LoginWindow) ensureEdits() {
	if lw.editReady() {
		return
	}
	ex, ew := lw.formX()
	lw.phoneEdit = NewEdit(lw.HWnd, 201, ex, dp(120), ew, dp(22), "手机号")
	lw.pwEdit = NewPasswordEdit(lw.HWnd, 202, ex, dp(158), ew, dp(22), "密码")
	lw.codeEdit = NewEdit(lw.HWnd, 203, ex, dp(158), ew, dp(22), "短信验证码")
	if lw.pwEdit != 0 {
		SetEditEnter(lw.pwEdit, lw.submit)
	}
	if lw.codeEdit != 0 {
		SetEditEnter(lw.codeEdit, lw.submit)
	}
	if lw.phoneEdit != 0 {
		SetEditEnter(lw.phoneEdit, lw.submit)
	}
}

func (lw *LoginWindow) submit() {
	if !lw.editReady() {
		return
	}
	phone := EditText(lw.phoneEdit)
	if lw.mode == 0 {
		if lw.OnPhoneLogin != nil {
			lw.OnPhoneLogin(phone, EditText(lw.pwEdit))
		}
		return
	}
	if lw.OnCaptchaLogin != nil {
		lw.OnCaptchaLogin(phone, EditText(lw.codeEdit))
	}
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
	lw.mode = 0
	lw.Panel.Invalidate()
}

func (lw *LoginWindow) ShowQR() {
	lw.tab = 0
	lw.ensureEdits()
	lw.layout()
	lw.Panel.Show()
}

// UseCaptchaMode switches the phone form to the SMS code flow, used when the
// password path hits risk control (code 8821).
func (lw *LoginWindow) UseCaptchaMode(hint string) {
	lw.ensureEdits()
	lw.tab = 1
	lw.mode = 1
	if hint != "" {
		lw.status = hint
	}
	lw.layout()
	w32.Invalidate(lw.HWnd)
}

// ShowPhoneMode opens the password form (used when the QR flow is blocked).
func (lw *LoginWindow) ShowPhoneMode() {
	lw.ensureEdits()
	lw.tab = 1
	lw.layout()
	w32.SetFocus(lw.phoneEdit)
	w32.Invalidate(lw.HWnd)
}

func (lw *LoginWindow) body(dc w32.HDC, client w32.RECT) {
	lw.layout()
	drawChip(dc, lw.btnTabQR, "扫码登录", lw.hoverBtn == 1, lw.tab == 0)
	drawChip(dc, lw.btnTabPhone, "手机号登录", lw.hoverBtn == 2, lw.tab == 1)

	if lw.tab == 0 {
		side := dp(210)
		x := (lw.W - side) / 2
		y := dp(64)
		r := rc(x, y, x+side, y+side)
		fill(dc, r, w32.RGB(0xFF, 0xFF, 0xFF))
		if lw.qr != nil {
			lw.qr.Blit(dc, r.Left, r.Top)
		}
		strokeRect(dc, r, T.Border)
		st := lw.qrState
		if st == "" {
			st = "请使用网易云音乐 App 扫码"
		}
		drawTextIn(dc, rc(dp(10), y+side+dp(8), lw.W-dp(10), y+side+dp(28)), st,
			textOpt{Font: T.FontS, Color: T.TextMid, Align: w32.DT_CENTER})
		drawChip(dc, lw.btnRefresh, "刷新二维码", lw.hoverBtn == 3, false)
	} else {
		drawTextIn(dc, rc(dp(14), dp(96), lw.W-dp(14), dp(118)), "手机号",
			textOpt{Font: T.FontXS, Color: T.TextDim})
		if lw.mode == 0 {
			drawTextIn(dc, rc(dp(14), dp(134), lw.W-dp(14), dp(156)), "密码",
				textOpt{Font: T.FontXS, Color: T.TextDim})
			drawChip(dc, lw.btnMode, "改用验证码登录", lw.hoverBtn == 4, false)
		} else {
			drawTextIn(dc, rc(dp(14), dp(134), lw.W-dp(14), dp(156)), "短信验证码",
				textOpt{Font: T.FontXS, Color: T.TextDim})
			drawChip(dc, lw.btnMode, "改用密码登录", lw.hoverBtn == 4, false)
			drawChip(dc, lw.btnSendCode, "获取验证码", lw.hoverBtn == 5, false)
		}
		drawChip(dc, lw.btnLogin, "登录", lw.hoverBtn == 6, true)
	}

	if lw.status != "" {
		drawTextIn(dc, rc(dp(10), lw.H-dp(40), lw.W-dp(10), lw.H-dp(14)), lw.status,
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
	case lw.tab == 1 && inRect(lw.btnMode, x, y):
		return 4
	case lw.tab == 1 && lw.mode == 1 && inRect(lw.btnSendCode, x, y):
		return 5
	case lw.tab == 1 && inRect(lw.btnLogin, x, y):
		return 6
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
			lw.layout()
			if lw.qr == nil && lw.OnRequestQR != nil {
				lw.qrState = "正在获取二维码…"
				lw.OnRequestQR()
			}
		case 2:
			lw.tab = 1
			lw.ensureEdits()
			lw.layout()
			w32.SetFocus(lw.phoneEdit)
		case 3:
			if lw.OnRequestQR != nil {
				lw.qrState = "正在获取二维码…"
				lw.OnRequestQR()
			}
		case 4:
			lw.mode = 1 - lw.mode
			lw.status = ""
			lw.layout()
			if lw.mode == 1 {
				w32.SetFocus(lw.codeEdit)
			} else {
				w32.SetFocus(lw.pwEdit)
			}
		case 5:
			if lw.OnSendCaptcha != nil && lw.editReady() {
				lw.OnSendCaptcha(EditText(lw.phoneEdit))
			}
		case 6:
			lw.submit()
		}
		w32.Invalidate(lw.HWnd)
		return true, 0
	}
	return false, 0
}
