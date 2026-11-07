package ui

import (
	"netmusic/internal/w32"
)

// ListState is what a row renderer gets told about its row.
type ListState struct {
	Selected bool
	Hover    bool
	Index    int
}

// ListView is a fully virtualised list: it stores nothing per row except the
// selected/hover index, asks the caller for the row count and paints only the
// rows that are actually visible. This is what keeps the comment browser at a
// constant memory cost no matter how large the list claims to be.
type ListView struct {
	Rect w32.RECT
	Bg   uint32 // 0 = leave the background to the parent

	Count func() int
	RowH  func() int32
	Draw  func(dc w32.HDC, idx int, r w32.RECT, st ListState)
	OnSelect func(idx int)
	OnActivate func(idx int)

	Sel     int
	Hover   int
	Top     int
	scrollH int32

	draggingThumb bool
	PressY        int32
	PressTop      int
}

func NewListView() *ListView {
	return &ListView{Sel: -1, Hover: -1}
}

func (l *ListView) rowH() int32 {
	if l.RowH != nil {
		return l.RowH()
	}
	return dp(24)
}

func (l *ListView) count() int {
	if l.Count == nil {
		return 0
	}
	return l.Count()
}

// visibleRows reports the row range that fits in the viewport.
func (l *ListView) visibleRows() (int, int) {
	h := l.Rect.Dy()
	rh := l.rowH()
	if rh <= 0 || h <= 0 {
		return 0, 0
	}
	n := l.count()
	first := l.Top
	if first < 0 {
		first = 0
	}
	if first > n {
		first = n
	}
	rows := int(h/rh) + 1
	last := first + rows
	if last > n {
		last = n
	}
	return first, last
}

func (l *ListView) maxTop() int {
	rh := l.rowH()
	if rh <= 0 {
		return 0
	}
	total := l.count() * int(rh)
	vis := int(l.Rect.Dy())
	if total <= vis {
		return 0
	}
	// last full page position
	m := l.count() - vis/int(rh)
	if m < 0 {
		m = 0
	}
	return m
}

func (l *ListView) clampTop() {
	if l.Top < 0 {
		l.Top = 0
	}
	if m := l.maxTop(); l.Top > m {
		l.Top = m
	}
}

// Paint renders the visible slice.
func (l *ListView) Paint(dc w32.HDC) {
	if l.Rect.Dx() <= 0 || l.Rect.Dy() <= 0 {
		return
	}
	save := w32.SaveDC(dc)
	w32.IntersectClipRect(dc, l.Rect.Left, l.Rect.Top, l.Rect.Right, l.Rect.Bottom)
	if l.Bg != 0 {
		fill(dc, l.Rect, l.Bg)
	}

	l.clampTop()
	first, last := l.visibleRows()
	rh := l.rowH()
	y := l.Rect.Top
	for i := first; i < last; i++ {
		r := rc(l.Rect.Left, y, l.Rect.Right, y+rh)
		if l.Draw != nil {
			l.Draw(dc, i, r, ListState{Selected: i == l.Sel, Hover: i == l.Hover, Index: i})
		}
		y += rh
	}
	w32.RestoreDC(dc, save)
	l.paintScrollbar(dc)
}

func (l *ListView) paintScrollbar(dc w32.HDC) {
	n := l.count()
	rh := l.rowH()
	vis := l.Rect.Dy()
	if n <= 0 || int(rh)*n <= int(vis) || rh <= 0 {
		return
	}
	barW := dp(5)
	x := l.Rect.Right - barW - dp(2)
	top := l.Rect.Top + dp(2)
	h := vis - dp(4)
	fillXY(dc, x, top, barW, h, T.Panel)
	total := int32(n) * rh
	thumbH := h * vis / total
	if thumbH < dp(18) {
		thumbH = dp(18)
	}
	maxScroll := total - vis
	if maxScroll < 1 {
		maxScroll = 1
	}
	thumbY := top + (h-thumbH)*int32(l.Top)*rh/maxScroll
	l.scrollH = thumbH
	fillXY(dc, x, thumbY, barW, thumbH, T.Border)
}

func (l *ListView) scrollbarHit(x, y int32) bool {
	barW := dp(5)
	return x >= l.Rect.Right-barW-dp(2) && x < l.Rect.Right && y >= l.Rect.Top && y < l.Rect.Bottom
}

func (l *ListView) Wheel(delta int) bool {
	if delta == 0 {
		return false
	}
	step := 3
	if delta < 0 {
		l.Top += step
	} else {
		l.Top -= step
	}
	l.clampTop()
	return true
}

func (l *ListView) MouseMove(x, y int32) bool {
	if l.draggingThumb {
		l.dragThumbTo(y)
		return true
	}
	idx := -1
	if inRect(l.Rect, x, y) {
		rh := l.rowH()
		if rh > 0 {
			idx = l.Top + int((y-l.Rect.Top)/rh)
			if idx >= l.count() {
				idx = -1
			}
		}
	}
	if idx != l.Hover {
		l.Hover = idx
		return true
	}
	return false
}

func (l *ListView) dragThumbTo(y int32) {
	h := l.Rect.Dy() - dp(4)
	thumbH := l.scrollH
	if h <= thumbH {
		return
	}
	rel := (y - l.Rect.Top - dp(2) - thumbH/2)
	if rel < 0 {
		rel = 0
	}
	if rel > h-thumbH {
		rel = h - thumbH
	}
	maxTop := l.maxTop()
	l.Top = int(rel) * maxTop / int(h-thumbH)
	l.clampTop()
}

func (l *ListView) LDown(x, y int32) bool {
	if !inRect(l.Rect, x, y) {
		return false
	}
	if l.scrollbarHit(x, y) {
		l.draggingThumb = true
		l.dragThumbTo(y)
		return true
	}
	rh := l.rowH()
	if rh <= 0 {
		return true
	}
	idx := l.Top + int((y-l.Rect.Top)/rh)
	if idx >= 0 && idx < l.count() {
		l.Sel = idx
		if l.OnSelect != nil {
			l.OnSelect(idx)
		}
	}
	return true
}

func (l *ListView) LUp(x, y int32) bool {
	if l.draggingThumb {
		l.draggingThumb = false
		return true
	}
	return false
}

func (l *ListView) DblClick(x, y int32) bool {
	if !inRect(l.Rect, x, y) {
		return false
	}
	rh := l.rowH()
	if rh <= 0 {
		return false
	}
	idx := l.Top + int((y-l.Rect.Top)/rh)
	if idx >= 0 && idx < l.count() && l.OnActivate != nil {
		l.OnActivate(idx)
		return true
	}
	return false
}

func (l *ListView) Key(vk uintptr) bool {
	n := l.count()
	if n == 0 {
		return false
	}
	switch vk {
	case 0x28: // down
		l.Sel = clampInt(l.Sel+1, 0, n-1)
	case 0x26: // up
		if l.Sel < 0 {
			l.Sel = 0
		} else {
			l.Sel = clampInt(l.Sel-1, 0, n-1)
		}
	case 0x21: // page up
		l.Sel = clampInt(l.Sel-l.visiblePage(), 0, n-1)
	case 0x22: // page down
		l.Sel = clampInt(l.Sel+l.visiblePage(), 0, n-1)
	case 0x24: // home
		l.Sel = 0
	case 0x23: // end
		l.Sel = n - 1
	case 0x0D: // enter
		if l.Sel >= 0 && l.OnActivate != nil {
			l.OnActivate(l.Sel)
		}
		return true
	default:
		return false
	}
	l.ensureVisible(l.Sel)
	if l.OnSelect != nil && l.Sel >= 0 {
		l.OnSelect(l.Sel)
	}
	return true
}

func (l *ListView) visiblePage() int {
	rh := l.rowH()
	if rh <= 0 {
		return 1
	}
	p := int(l.Rect.Dy() / rh)
	if p < 1 {
		p = 1
	}
	return p
}

func (l *ListView) ensureVisible(idx int) {
	if idx < 0 {
		return
	}
	per := l.visiblePage()
	if idx < l.Top {
		l.Top = idx
	}
	if idx >= l.Top+per {
		l.Top = idx - per + 1
	}
	l.clampTop()
}

// ScrollToBottom moves the viewport to the end (used by the lyric panel).
func (l *ListView) ScrollTo(idx int) {
	l.Top = idx
	l.clampTop()
}

func clampInt(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ---------------------------------------------------------------------------
// small text button used across the panels
// ---------------------------------------------------------------------------

type Chip struct {
	Rect  w32.RECT
	Label string
	Accent bool
}

func drawChip(dc w32.HDC, r w32.RECT, label string, hover, accent bool) {
	c := T.TextDim
	if accent {
		c = T.Accent
	}
	if hover {
		roundFill(dc, r, dp(4), T.AccentSoft)
		c = T.Accent
	} else {
		roundStroke(dc, r, dp(4), T.Hairline)
	}
	drawTextIn(dc, r, label, textOpt{Font: T.FontXS, Color: c, Align: w32.DT_CENTER})
}
