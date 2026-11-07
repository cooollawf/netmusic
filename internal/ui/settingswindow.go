package ui

import (
	"fmt"

	"netmusic/internal/w32"
)

// EQBandCount must match audio.EQBands / config.EQBands.
const EQBandCount = 10

var eqLabels = [EQBandCount]string{"31", "62", "125", "250", "500", "1k", "2k", "4k", "8k", "16k"}

type eqPreset struct {
	Name  string
	Gains [EQBandCount]int8
}

var eqPresets = []eqPreset{
	{"平直", [EQBandCount]int8{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}},
	{"流行", [EQBandCount]int8{-1, 0, 1, 3, 3, 2, 0, -1, -1, -1}},
	{"摇滚", [EQBandCount]int8{4, 3, 1, -1, -1, 1, 2, 3, 4, 4}},
	{"古典", [EQBandCount]int8{4, 3, 2, 1, 0, 0, -1, -2, -3, -4}},
	{"人声", [EQBandCount]int8{-3, -2, 0, 2, 4, 4, 3, 1, 0, -1}},
}

// SettingsWindow holds quality, equaliser, download folder and login info.
type SettingsWindow struct {
	*Panel

	quality  int
	eqOn     bool
	gains    [EQBandCount]int8
	dlDir    string
	loginTxt string

	btnQ [3]w32.RECT
	btnEQOn   w32.RECT
	btnPreset [5]w32.RECT
	btnOpen   w32.RECT
	btnLogout w32.RECT
	btnReset  w32.RECT

	faders   [EQBandCount]w32.RECT
	hoverBtn int
	dragBand int

	OnQuality      func(bps int)
	OnEQToggle     func(on bool)
	OnEQGain       func(band int, db int8)
	OnOpenDownload func()
	OnLogout       func()
}

func NewSettingsWindow() *SettingsWindow {
	sw := &SettingsWindow{dragBand: -1}
	sw.Panel = NewPanel("设置", dp(470), dp(430))
	sw.Panel.Body = sw.body
	sw.Panel.OnMsg = sw.onMsg
	return sw
}

func (sw *SettingsWindow) SetState(quality int, eqOn bool, gains [EQBandCount]int8, dlDir, login string) {
	sw.quality = quality
	sw.eqOn = eqOn
	sw.gains = gains
	sw.dlDir = dlDir
	sw.loginTxt = login
	sw.Panel.Invalidate()
}

func (sw *SettingsWindow) layout() {
	y := dp(58)
	for i := 0; i < 3; i++ {
		x := dp(16) + int32(i)*(dp(72)+dp(6))
		sw.btnQ[i] = rc(x, y, x+dp(72), y+dp(24))
	}
	sw.btnEQOn = rc(dp(16), dp(112), dp(16)+dp(84), dp(112)+dp(24))
	sw.btnReset = rc(dp(106), dp(112), dp(106)+dp(60), dp(112)+dp(24))
	for i := 0; i < 5; i++ {
		x := dp(176) + int32(i)*(dp(50)+dp(4))
		sw.btnPreset[i] = rc(x, dp(112), x+dp(50), dp(112)+dp(24))
	}
	// ten vertical faders
	for i := 0; i < EQBandCount; i++ {
		x := dp(16) + int32(i)*(dp(34)+dp(10))
		sw.faders[i] = rc(x, dp(150), x+dp(34), dp(292))
	}
	sw.btnOpen = rc(dp(16), dp(330), dp(16)+dp(90), dp(330)+dp(24))
	sw.btnLogout = rc(dp(16), dp(374), dp(16)+dp(90), dp(374)+dp(24))
}

func (sw *SettingsWindow) body(dc w32.HDC, client w32.RECT) {
	sw.layout()

	// ---- quality ----
	drawTextIn(dc, rc(dp(16), dp(32), dp(300), dp(54)), "音质（下一次播放生效）",
		textOpt{Font: T.FontXS, Color: T.TextMid})
	names := [3]string{"标准 128k", "较高 192k", "高 320k"}
	vals := [3]int{128000, 192000, 320000}
	for i := 0; i < 3; i++ {
		drawChip(dc, sw.btnQ[i], names[i], sw.hoverBtn == 10+i, sw.quality == vals[i])
	}

	// ---- equaliser ----
	eqName := "均衡器 关"
	if sw.eqOn {
		eqName = "均衡器 开"
	}
	drawChip(dc, sw.btnEQOn, eqName, sw.hoverBtn == 20, sw.eqOn)
	drawChip(dc, sw.btnReset, "重置", sw.hoverBtn == 21, false)
	for i, p := range eqPresets {
		active := sw.gains == p.Gains && sw.eqOn
		drawChip(dc, sw.btnPreset[i], p.Name, sw.hoverBtn == 30+i, active)
	}

	for i := 0; i < EQBandCount; i++ {
		r := sw.faders[i]
		// track
		midX := r.Left + r.Dx()/2
		top := r.Top + dp(10)
		bottom := r.Bottom - dp(12)
		vfill := T.PanelAlt
		fill(dc, rc(midX-dp(2), top, midX+dp(2), bottom), vfill)
		// 0 dB line
		zeroY := top + (bottom-top)/2
		fillXY(dc, midX-dp(7), zeroY, dp(14), dp(1), T.Hairline)
		// handle
		g := sw.gains[i]
		hy := zeroY - int32(g)*(bottom-top)/2/int32(12)
		if hy < top {
			hy = top
		}
		if hy > bottom {
			hy = bottom
		}
		knob := T.TextMid
		if sw.eqOn && g != 0 {
			knob = T.Accent
		}
		roundFill(dc, rc(midX-dp(9), hy-dp(4), midX+dp(9), hy+dp(4)), dp(3), knob)
		// value + label
		drawTextIn(dc, rc(r.Left, top-dp(16), r.Right, top-dp(2)),
			fmt.Sprintf("%+d", g), textOpt{Font: T.FontXS, Color: T.TextDim, Align: w32.DT_CENTER})
		drawTextIn(dc, rc(r.Left, bottom+dp(2), r.Right, r.Bottom),
			eqLabels[i], textOpt{Font: T.FontXS, Color: T.TextDim, Align: w32.DT_CENTER})
	}
	drawTextIn(dc, rc(dp(16), dp(300), dp(454), dp(322)),
		"提示：单频段 ±12dB 可能削波，正常使用建议 ±6dB 以内",
		textOpt{Font: T.FontXS, Color: T.TextDim})

	// ---- download ----
	drawTextIn(dc, rc(dp(16), dp(312), dp(300), dp(330)), "", textOpt{Font: T.FontXS})
	drawChip(dc, sw.btnOpen, "打开下载目录", sw.hoverBtn == 40, false)
	drawTextIn(dc, rc(dp(116), dp(330), dp(454), dp(354)), sw.dlDir,
		textOpt{Font: T.FontXS, Color: T.TextDim})

	// ---- login ----
	drawChip(dc, sw.btnLogout, "退出登录", sw.hoverBtn == 41, false)
	drawTextIn(dc, rc(dp(116), dp(374), dp(454), dp(398)), sw.loginTxt,
		textOpt{Font: T.FontXS, Color: T.TextDim})
}

func (sw *SettingsWindow) hitBtn(x, y int32) int {
	for i := 0; i < 3; i++ {
		if inRect(sw.btnQ[i], x, y) {
			return 10 + i
		}
	}
	if inRect(sw.btnEQOn, x, y) {
		return 20
	}
	if inRect(sw.btnReset, x, y) {
		return 21
	}
	for i := range sw.btnPreset {
		if inRect(sw.btnPreset[i], x, y) {
			return 30 + i
		}
	}
	if inRect(sw.btnOpen, x, y) {
		return 40
	}
	if inRect(sw.btnLogout, x, y) {
		return 41
	}
	return 0
}

// bandAt returns the fader index under the cursor, or -1.
func (sw *SettingsWindow) bandAt(x, y int32) int {
	for i := 0; i < EQBandCount; i++ {
		if inRect(sw.faders[i], x, y) {
			return i
		}
	}
	return -1
}

// gainForY maps a cursor position inside a fader to a dB value.
func (sw *SettingsWindow) gainForY(band int, y int32) int8 {
	r := sw.faders[band]
	top := r.Top + dp(10)
	bottom := r.Bottom - dp(12)
	if bottom <= top {
		return 0
	}
	// +12 at the top, -12 at the bottom
	v := float64(y-top) / float64(bottom-top)
	db := 12 - v*24
	if db > 12 {
		db = 12
	}
	if db < -12 {
		db = -12
	}
	return int8(db)
}

func (sw *SettingsWindow) setGains(g [EQBandCount]int8) {
	if sw.OnEQToggle != nil && !sw.eqOn {
		sw.eqOn = true
		sw.OnEQToggle(true)
	}
	sw.gains = g
	for i := 0; i < EQBandCount; i++ {
		if sw.OnEQGain != nil {
			sw.OnEQGain(i, g[i])
		}
	}
	sw.Panel.Invalidate()
}

func (sw *SettingsWindow) onMsg(msg uint32, wp, lp uintptr) (bool, uintptr) {
	x, y := lparamXY(lp)
	switch msg {
	case w32.WM_MOUSEMOVE:
		if sw.dragBand >= 0 {
			if g := sw.gainForY(sw.dragBand, y); g != sw.gains[sw.dragBand] {
				sw.gains[sw.dragBand] = g
				if sw.OnEQGain != nil {
					sw.OnEQGain(sw.dragBand, g)
				}
				sw.Panel.Invalidate()
			}
			return true, 0
		}
		hv := sw.hitBtn(x, y)
		if hv != sw.hoverBtn {
			sw.hoverBtn = hv
			sw.Panel.Invalidate()
		}
		return true, 0
	case w32.WM_MOUSELEAVE:
		sw.hoverBtn = 0
		sw.Panel.Invalidate()
		return true, 0
	case w32.WM_LBUTTONDOWN:
		if b := sw.bandAt(x, y); b >= 0 {
			sw.dragBand = b
			w32.SetCapture(sw.HWnd)
			if g := sw.gainForY(b, y); g != sw.gains[b] {
				sw.gains[b] = g
				if sw.OnEQGain != nil {
					sw.OnEQGain(b, g)
				}
				sw.Panel.Invalidate()
			}
			return true, 0
		}
	case w32.WM_LBUTTONUP:
		if sw.dragBand >= 0 {
			sw.dragBand = -1
			w32.ReleaseCapture()
			return true, 0
		}
		switch sw.hitBtn(x, y) {
		case 10:
			sw.quality = 128000
		case 11:
			sw.quality = 192000
		case 12:
			sw.quality = 320000
		case 20:
			sw.eqOn = !sw.eqOn
			if sw.OnEQToggle != nil {
				sw.OnEQToggle(sw.eqOn)
			}
		case 21:
			var zero [EQBandCount]int8
			sw.setGains(zero)
		case 40:
			if sw.OnOpenDownload != nil {
				sw.OnOpenDownload()
			}
		case 41:
			if sw.OnLogout != nil {
				sw.OnLogout()
			}
		default:
			if n := sw.hitBtn(x, y) - 30; n >= 0 && n < len(eqPresets) {
				sw.setGains(eqPresets[n].Gains)
			}
		}
		if q := sw.hitBtn(x, y); q >= 10 && q <= 12 {
			vals := [3]int{128000, 192000, 320000}
			sw.quality = vals[q-10]
			if sw.OnQuality != nil {
				sw.OnQuality(sw.quality)
			}
		}
		sw.Panel.Invalidate()
		return true, 0
	}
	return false, 0
}
