package ui

import (
	"netmusic/internal/w32"
)

// Theme holds the dark, TTPlayer-inspired palette plus the lazily created GDI
// objects. Every GDI object is created once and reused, so painting at 60 Hz
// allocates nothing.
type Theme struct {
	Scale float32

	BgTop      uint32
	BgBottom   uint32
	TitleTop   uint32
	TitleBtm   uint32
	Border     uint32
	Hairline   uint32
	Panel      uint32
	PanelAlt   uint32
	TrackBg    uint32
	Accent     uint32
	AccentDark uint32
	AccentSoft uint32
	TextHi     uint32
	TextMid    uint32
	TextDim    uint32
	CoverBg    uint32
	CoverEdge  uint32

	FontXS, FontS, FontM, FontL, FontXL w32.HFONT

	brushes map[uint32]w32.HBRUSH
	pens    map[uint32]w32.HPEN
}

var T Theme

// dp converts a 96-dpi design value to physical pixels.
func dp(v int32) int32 {
	if T.Scale == 1 {
		return v
	}
	return int32(float32(v)*T.Scale + 0.5)
}

// rc is a terse rectangle constructor; the layout code reads much better with
// it than with four-key struct literals in every line.
func rc(l, t, r, b int32) w32.RECT {
	return w32.RECT{Left: l, Top: t, Right: r, Bottom: b}
}

func InitTheme() {
	T = Theme{
		BgTop:      w32.RGB(0x2A, 0x31, 0x39),
		BgBottom:   w32.RGB(0x17, 0x1B, 0x20),
		TitleTop:   w32.RGB(0x33, 0x3B, 0x45),
		TitleBtm:   w32.RGB(0x25, 0x2B, 0x33),
		Border:     w32.RGB(0x46, 0x53, 0x61),
		Hairline:   w32.RGB(0x3A, 0x44, 0x50),
		Panel:      w32.RGB(0x20, 0x25, 0x2B),
		PanelAlt:   w32.RGB(0x1A, 0x1F, 0x24),
		TrackBg:    w32.RGB(0x33, 0x3C, 0x46),
		Accent:     w32.RGB(0x48, 0xB8, 0xE8),
		AccentDark: w32.RGB(0x2E, 0x86, 0xB0),
		AccentSoft: w32.RGB(0x1E, 0x3A, 0x4A),
		TextHi:     w32.RGB(0xE8, 0xEF, 0xF6),
		TextMid:    w32.RGB(0xA8, 0xB6, 0xC4),
		TextDim:    w32.RGB(0x6E, 0x7D, 0x8C),
		CoverBg:    w32.RGB(0x12, 0x16, 0x1A),
		CoverEdge:  w32.RGB(0x3D, 0x49, 0x56),
		brushes:    map[uint32]w32.HBRUSH{},
		pens:       map[uint32]w32.HPEN{},
	}
	dpi := w32.DPI()
	T.Scale = float32(dpi) / 96
	if T.Scale < 1 {
		T.Scale = 1
	}
	// UIface: "Microsoft YaHei UI" only exists on Windows 8+, and the Windows 7
// target is the reason this player exists. Plain "Microsoft YaHei" ships with
// Vista and every later release, so one face name works everywhere.
UIface := "Microsoft YaHei"
	T.FontXS = w32.CreateFont(sz(11), 400, UIface, false)
	T.FontS = w32.CreateFont(sz(12), 400, UIface, false)
	T.FontM = w32.CreateFont(sz(13), 400, UIface, false)
	T.FontL = w32.CreateFont(sz(15), 700, UIface, false)
	T.FontXL = w32.CreateFont(sz(19), 700, UIface, false)
}

func sz(v int32) int32 { return dp(v) }

func (t *Theme) brush(c uint32) w32.HBRUSH {
	if b, ok := t.brushes[c]; ok {
		return b
	}
	b := w32.CreateSolidBrush(c)
	t.brushes[c] = b
	return b
}

// BrushHandle exposes a cached brush for child controls (EDIT background).
func (t *Theme) BrushHandle(c uint32) w32.HBRUSH { return t.brush(c) }

func (t *Theme) pen(c uint32, width int32) w32.HPEN {
	key := c ^ uint32(width)<<24
	if p, ok := t.pens[key]; ok {
		return p
	}
	p := w32.CreatePen(w32.PS_SOLID, width, c)
	t.pens[key] = p
	return p
}

func (t *Theme) Free() {
	for _, b := range t.brushes {
		w32.DeleteObject(w32.HGDIOBJ(b))
	}
	for _, p := range t.pens {
		w32.DeleteObject(w32.HGDIOBJ(p))
	}
	for _, f := range []w32.HFONT{t.FontXS, t.FontS, t.FontM, t.FontL, t.FontXL} {
		w32.DeleteObject(w32.HGDIOBJ(f))
	}
	t.brushes = map[uint32]w32.HBRUSH{}
	t.pens = map[uint32]w32.HPEN{}
}

// ---------------------------------------------------------------------------
// painting primitives
// ---------------------------------------------------------------------------

func fill(dc w32.HDC, r w32.RECT, c uint32) {
	rr := r
	w32.FillRect(dc, &rr, T.brush(c))
}

func fillXY(dc w32.HDC, x, y, w, h int32, c uint32) {
	fill(dc, rc(x, y, x+w, y+h), c)
}

func hline(dc w32.HDC, x1, x2, y int32, c uint32) {
	fillXY(dc, x1, y, x2-x1, 1, c)
}

func vline(dc w32.HDC, x, y1, y2 int32, c uint32) {
	fillXY(dc, x, y1, 1, y2-y1, c)
}

func strokeRect(dc w32.HDC, r w32.RECT, c uint32) {
	hline(dc, r.Left, r.Right, r.Top, c)
	hline(dc, r.Left, r.Right, r.Bottom-1, c)
	vline(dc, r.Left, r.Top, r.Bottom, c)
	vline(dc, r.Right-1, r.Top, r.Bottom, c)
}

// roundFill paints a rounded rectangle filled with c (anti-aliasing is
// approximated by the GDI round rect, which is good enough for small radii).
func roundFill(dc w32.HDC, r w32.RECT, radius int32, c uint32) {
	oldPen := w32.SelectObject(dc, w32.HGDIOBJ(T.brush(c)))
	oldBr := w32.SelectObject(dc, w32.HGDIOBJ(T.brush(c)))
	_ = oldPen
	w32.RoundRect(dc, r.Left, r.Top, r.Right, r.Bottom, radius*2, radius*2)
	w32.SelectObject(dc, oldBr)
	w32.SelectObject(dc, w32.HGDIOBJ(w32.GetStockObject(w32.NULL_PEN)))
}

func roundStroke(dc w32.HDC, r w32.RECT, radius int32, c uint32) {
	oldBr := w32.SelectObject(dc, w32.HGDIOBJ(T.brush(w32.RGB(0, 0, 0))))
	oldPen := w32.SelectObject(dc, w32.HGDIOBJ(T.pen(c, 1)))
	w32.RoundRect(dc, r.Left, r.Top, r.Right, r.Bottom, radius*2, radius*2)
	w32.SelectObject(dc, oldPen)
	w32.SelectObject(dc, oldBr)
}

type textOpt struct {
	Font  w32.HFONT
	Color uint32
	Align uint32 // DT_LEFT / DT_CENTER / DT_RIGHT
}

func drawTextIn(dc w32.HDC, r w32.RECT, s string, o textOpt) {
	if s == "" {
		return
	}
	old, _, _ := w32.SelectObject2(dc, w32.HGDIOBJ(o.Font))
	w32.SetBkMode(dc, w32.TRANSPARENT)
	w32.SetTextColor(dc, o.Color)
	flags := o.Align | w32.DT_SINGLELINE | w32.DT_VCENTER | w32.DT_END_ELLIPSIS | w32.DT_NOPREFIX
	w32.DrawText(dc, s, r, flags)
	w32.SelectObject(dc, old)
}

func drawTextWrap(dc w32.HDC, r w32.RECT, s string, o textOpt) int32 {
	if s == "" {
		return 0
	}
	old, _, _ := w32.SelectObject2(dc, w32.HGDIOBJ(o.Font))
	w32.SetBkMode(dc, w32.TRANSPARENT)
	w32.SetTextColor(dc, o.Color)
	flags := o.Align | w32.DT_WORDBREAK | w32.DT_NOPREFIX
	n := w32.DrawText(dc, s, r, flags)
	w32.SelectObject(dc, old)
	return n
}

// drawWrapped2 renders at most two lines of text with a manual per-character
// wrap. CJK text has no spaces, so GDI's word break would overflow the row;
// measuring prefixes with GetTextExtentPoint32 is both correct and cheap for
// the two lines a comment row allows.
func drawWrapped2(dc w32.HDC, r w32.RECT, text string, font w32.HFONT, color uint32) {
	const maxLines = 2
	runes := []rune(text)
	if len(runes) == 0 {
		return
	}
	old, _, _ := w32.SelectObject2(dc, w32.HGDIOBJ(font))
	defer w32.SelectObject(dc, old)
	w32.SetBkMode(dc, w32.TRANSPARENT)
	w32.SetTextColor(dc, color)

	w := r.Dx()
	lineStart := 0
	y := r.Top
	for line := 0; line < maxLines && lineStart < len(runes); line++ {
		fits := len(runes) - lineStart
		if cx, _ := w32.TextExtent(dc, string(runes[lineStart:])); cx > w {
			lo, hi := 0, fits
			for lo < hi {
				mid := (lo + hi + 1) / 2
				if cx2, _ := w32.TextExtent(dc, string(runes[lineStart:lineStart+mid])); cx2 <= w {
					lo = mid
				} else {
					hi = mid - 1
				}
			}
			fits = lo
		}
		if fits <= 0 {
			fits = 1
		}
		seg := string(runes[lineStart : lineStart+fits])
		if line == maxLines-1 && lineStart+fits < len(runes) {
			seg += "…"
		}
		w32.TextOut(dc, r.Left, y, seg)
		y += dp(19)
		lineStart += fits
	}
}

// tri fills a triangle.
func tri(dc w32.HDC, pts [3]w32.POINT, c uint32) {
	oldBr := w32.SelectObject(dc, w32.HGDIOBJ(T.brush(c)))
	oldPen := w32.SelectObject(dc, w32.HGDIOBJ(w32.GetStockObject(w32.NULL_PEN)))
	w32.Polygon(dc, pts[:])
	w32.SelectObject(dc, oldPen)
	w32.SelectObject(dc, oldBr)
}

// icon geometry for the transport buttons, drawn from primitives so we do not
// depend on any font glyph coverage.
func iconPrev(dc w32.HDC, r w32.RECT, c uint32) {
	h := r.Dy()
	w := r.Dx()
	cx := r.Left + w/2
	cy := r.Top + h/2
	barW := dp(2)
	hh := h / 4
	tri(dc, [3]w32.POINT{
		{X: cx - w/6, Y: cy},
		{X: cx + w/5, Y: cy - hh},
		{X: cx + w/5, Y: cy + hh},
	}, c)
	tri(dc, [3]w32.POINT{
		{X: cx - w/3, Y: cy},
		{X: cx - w/6 + dp(1), Y: cy - hh},
		{X: cx - w/6 + dp(1), Y: cy + hh},
	}, c)
	fillXY(dc, cx-w/3-barW, cy-hh, barW, hh*2, c)
}

func iconNext(dc w32.HDC, r w32.RECT, c uint32) {
	h := r.Dy()
	w := r.Dx()
	cx := r.Left + w/2
	cy := r.Top + h/2
	barW := dp(2)
	hh := h / 4
	tri(dc, [3]w32.POINT{
		{X: cx + w/3, Y: cy},
		{X: cx - w/5, Y: cy - hh},
		{X: cx - w/5, Y: cy + hh},
	}, c)
	tri(dc, [3]w32.POINT{
		{X: cx + w/6 - dp(1), Y: cy},
		{X: cx - w/6 + dp(1), Y: cy - hh},
		{X: cx - w/6 + dp(1), Y: cy + hh},
	}, c)
	fillXY(dc, cx+w/3, cy-hh, barW, hh*2, c)
}

func iconPlay(dc w32.HDC, r w32.RECT, c uint32) {
	h := r.Dy()
	w := r.Dx()
	cx := r.Left + w/2
	cy := r.Top + h/2
	hh := h / 3
	tri(dc, [3]w32.POINT{
		{X: cx - w/8, Y: cy - hh},
		{X: cx + w/3, Y: cy},
		{X: cx - w/8, Y: cy + hh},
	}, c)
}

func iconPause(dc w32.HDC, r w32.RECT, c uint32) {
	h := r.Dy()
	w := r.Dx()
	cx := r.Left + w/2
	cy := r.Top + h/2
	bw := dp(4)
	hh := h / 3
	fillXY(dc, cx-bw-dp(2), cy-hh, bw, hh*2+dp(1), c)
	fillXY(dc, cx+dp(2), cy-hh, bw, hh*2+dp(1), c)
}

func iconSpeaker(dc w32.HDC, r w32.RECT, c uint32) {
	h := r.Dy()
	cy := r.Top + h/2
	x := r.Left
	bh := h / 3
	fillXY(dc, x, cy-bh/2, dp(3), bh, c)
	tri(dc, [3]w32.POINT{
		{X: x + dp(3), Y: cy - bh/2 - dp(1)},
		{X: x + dp(9), Y: cy - bh},
		{X: x + dp(9), Y: cy + bh},
	}, c)
	tri(dc, [3]w32.POINT{
		{X: x + dp(3), Y: cy + bh/2 + dp(1)},
		{X: x + dp(9), Y: cy + bh},
		{X: x + dp(3), Y: cy - bh/2 - dp(1)},
	}, c)
}

func iconClose(dc w32.HDC, r w32.RECT, c uint32) {
	cx := r.Left + r.Dx()/2
	cy := r.Top + r.Dy()/2
	s := dp(4)
	for i := int32(0); i < dp(2); i++ {
		w32.SelectObject(dc, w32.HGDIOBJ(T.pen(c, 1)))
		// two diagonals
		w32.MoveTo(dc, cx-s+i, cy-s)
		w32.LineTo(dc, cx+s+i, cy+s+1)
		w32.MoveTo(dc, cx+s-i, cy-s)
		w32.LineTo(dc, cx-s-i, cy+s+1)
	}
	w32.SelectObject(dc, w32.HGDIOBJ(w32.GetStockObject(w32.NULL_PEN)))
}

func iconMin(dc w32.HDC, r w32.RECT, c uint32) {
	cx := r.Left + r.Dx()/2
	cy := r.Top + r.Dy()/2
	s := dp(4)
	fillXY(dc, cx-s, cy, s*2+1, dp(1), c)
}

func iconPin(dc w32.HDC, r w32.RECT, c uint32) {
	cx := r.Left + r.Dx()/2
	cy := r.Top + r.Dy()/2
	s := dp(4)
	// head plate
	fillXY(dc, cx-s, cy-s, s*2, dp(2), c)
	// neck
	fillXY(dc, cx-dp(2), cy-s+dp(2), dp(4), dp(3), c)
	// shaft
	fillXY(dc, cx, cy+dp(1), dp(1), dp(3), c)
	// point
	tri(dc, [3]w32.POINT{
		{X: cx - dp(3), Y: cy + dp(1)},
		{X: cx + dp(3), Y: cy + dp(1)},
		{X: cx, Y: cy + dp(4)},
	}, c)
}

// iconNote draws a music note used as the cover placeholder.
func iconNote(dc w32.HDC, cx, cy int32, size int32, c uint32) {
	w := size
	h := size
	stemW := w / 10
	if stemW < 1 {
		stemW = 1
	}
	// stem
	fillXY(dc, cx, cy-h/2, stemW, h/2+h/8, c)
	// head
	w32.SelectObject(dc, w32.HGDIOBJ(T.brush(c)))
	oldPen := w32.SelectObject(dc, w32.HGDIOBJ(w32.GetStockObject(w32.NULL_PEN)))
	w32.Ellipse(dc, cx-w/2, cy+h/8, cx+stemW+w/4, cy+h/2)
	w32.SelectObject(dc, oldPen)
	// flag
	tri(dc, [3]w32.POINT{
		{X: cx + stemW, Y: cy - h/2},
		{X: cx + stemW + w/3, Y: cy - h/2 + h/6},
		{X: cx + stemW, Y: cy - h/4},
	}, c)
}
