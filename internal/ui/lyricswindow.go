package ui

import (
	"netmusic/internal/lrc"
	"netmusic/internal/w32"
)

// LyricsWindow shows the synced lyric sheet, TTPlayer style: current line
// highlighted and kept centered, everything else dimmed.
type LyricsWindow struct {
	*Panel
	list *ListView
	doc  *lrc.Doc

	cur        int
	twoLine    bool // document carries translations
	title      string
	userScroll uint64 // tick until which auto-centering is suppressed
	autoFollow bool

	Position func() float64
	Duration func() float64
	OnSeek   func(frac float64)
}

func NewLyricsWindow() *LyricsWindow {
	lw := &LyricsWindow{cur: -1, autoFollow: true}
	lw.Panel = NewPanel("歌词", dp(400), dp(330))
	lw.list = NewListView()
	lw.list.RowH = func() int32 {
		// Bilingual documents get a taller row so the original and the
		// translation each own half of it instead of overlapping.
		if lw.twoLine {
			return dp(48)
		}
		return dp(34)
	}
	lw.list.Count = func() int { return lw.doc.Len() }
	lw.list.Draw = lw.drawLine
	lw.list.OnActivate = func(idx int) { lw.seekTo(idx) }
	lw.list.OnSelect = func(idx int) { lw.userScroll = w32.TickCount() + 4000 }
	lw.Panel.Body = lw.body
	lw.Panel.OnMsg = lw.onMsg
	return lw
}

func (lw *LyricsWindow) SetDoc(doc *lrc.Doc, title string) {
	lw.doc = doc
	lw.title = title
	lw.cur = -1
	lw.twoLine = doc != nil && doc.HasTrans
	lw.Panel.Title = "歌词 — " + title
	lw.list.Sel = -1
	lw.list.Top = 0
	lw.autoFollow = true
	if lw.Visible {
		lw.Panel.Invalidate()
	}
}

func (lw *LyricsWindow) layout() {
	lw.list.Rect = rc(dp(6), dp(30), lw.W - dp(6), lw.H - dp(6))
}

func (lw *LyricsWindow) body(dc w32.HDC, client w32.RECT) {
	lw.layout()
	lw.list.Bg = 0
	lw.list.Paint(dc)
	if lw.doc.Len() == 0 {
		drawTextIn(dc, rc(dp(10), client.Top + dp(100), lw.W - dp(10), client.Top + dp(124)),
			"暂无歌词", textOpt{Font: T.FontS, Color: T.TextDim, Align: w32.DT_CENTER})
	}
}

func (lw *LyricsWindow) drawLine(dc w32.HDC, idx int, r w32.RECT, st ListState) {
	if idx < 0 || idx >= lw.doc.Len() {
		return
	}
	ln := lw.doc.Lines[idx]
	if st.Selected && lw.list.visiblePage() > 0 {
		// subtle selection feedback only while browsing
	}
	cur := idx == lw.cur
	text := ln.Text
	// translation is stored after a newline
	main, trans := text, ""
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			main, trans = text[:i], text[i+1:]
			break
		}
	}
	if lw.twoLine {
		// Bilingual row: the original owns the top half, the translation the
		// bottom half. Previously both were drawn into the same 34 px row with a
		// 14 px offset, so a foreign-language line and its Chinese translation
		// overlapped each other.
		mainRect := rc(r.Left, r.Top+dp(1), r.Right, r.Top+dp(27))
		transRect := rc(r.Left, r.Top+dp(25), r.Right, r.Bottom-dp(1))
		if cur {
			drawTextIn(dc, mainRect, main, textOpt{Font: T.FontL, Color: T.Accent, Align: w32.DT_CENTER})
			if trans != "" {
				drawTextIn(dc, transRect, trans, textOpt{Font: T.FontXS, Color: T.AccentDark, Align: w32.DT_CENTER})
			}
			return
		}
		c := T.TextMid
		if st.Hover {
			c = T.TextHi
		}
		drawTextIn(dc, mainRect, main, textOpt{Font: T.FontS, Color: c, Align: w32.DT_CENTER})
		if trans != "" {
			drawTextIn(dc, transRect, trans, textOpt{Font: T.FontXS, Color: T.TextDim, Align: w32.DT_CENTER})
		}
		return
	}
	if cur {
		drawTextIn(dc, r, main, textOpt{Font: T.FontL, Color: T.Accent, Align: w32.DT_CENTER})
		return
	}
	c := T.TextMid
	if st.Hover {
		c = T.TextHi
	}
	drawTextIn(dc, r, main, textOpt{Font: T.FontS, Color: c, Align: w32.DT_CENTER})
}

// Tick advances the highlight. Called from the app's 250 ms UI timer.
func (lw *LyricsWindow) Tick(posMs int64) bool {
	if lw.doc.Len() == 0 {
		return false
	}
	// The viewport size is needed to centre the line; make sure it is current
	// even if the window has not been painted yet.
	lw.layout()
	idx := lw.doc.IndexAt(posMs)
	// Step forwards only. A small backwards step can only come from position
	// jitter (the audio clock estimate wobbles by a few ms), and honouring it
	// makes the highlight bounce between two lines - the reported "twitching".
	// A real seek moves far enough to pass this check.
	if idx >= 0 && lw.cur >= 0 && idx < lw.cur {
		if back := lw.doc.Lines[lw.cur].Ms - posMs; back < 500 {
			return false
		}
	}
	if idx == lw.cur {
		return false
	}
	lw.cur = idx
	if lw.autoFollow && w32.TickCount() > lw.userScroll {
		lw.centerOn(idx)
	}
	return true
}

func (lw *LyricsWindow) centerOn(idx int) {
	if idx < 0 {
		return
	}
	per := lw.list.visiblePage()
	lw.list.Top = idx - per/2
	lw.list.clampTop()
}

func (lw *LyricsWindow) seekTo(idx int) {
	if lw.OnSeek == nil || lw.Duration == nil {
		return
	}
	dur := lw.Duration()
	if dur <= 0 {
		return
	}
	frac := float64(lw.doc.Lines[idx].Ms) / 1000 / dur
	lw.OnSeek(frac)
}

func (lw *LyricsWindow) onMsg(msg uint32, wp, lp uintptr) (bool, uintptr) {
	x, y := lparamXY(lp)
	switch msg {
	case w32.WM_MOUSEWHEEL:
		delta := int16((wp >> 16) & 0xffff)
		if lw.list.Wheel(int(delta)) {
			lw.userScroll = w32.TickCount() + 4000
			w32.Invalidate(lw.HWnd)
		}
		return true, 0
	case w32.WM_MOUSEMOVE:
		if lw.list.MouseMove(x, y) {
			w32.Invalidate(lw.HWnd)
		}
	case w32.WM_LBUTTONDOWN:
		lw.list.LDown(x, y)
		w32.Invalidate(lw.HWnd)
	case w32.WM_LBUTTONUP:
		lw.list.LUp(x, y)
	case w32.WM_LBUTTONDBLCLK:
		lw.list.DblClick(x, y)
	case w32.WM_KEYDOWN:
		if lw.list.Key(wp) {
			w32.Invalidate(lw.HWnd)
			return true, 0
		}
	}
	return false, 0
}
