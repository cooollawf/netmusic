package ui

import (
	"fmt"
	"time"

	"netmusic/internal/w32"
)

// CommentVM is the flattened, UI ready view of one comment. Rich text is
// already reduced to plain text upstream.
type CommentVM struct {
	User    string
	Content string
	TimeStr string
	Likes   int
	Hot     bool
}

// CommentsWindow pages through comments. It never holds more than one page of
// them, and the list itself is drawn virtually, so memory does not grow with
// the total number of comments on a song (which is easily >100k).
type CommentsWindow struct {
	*Panel
	list *ListView

	hot  []CommentVM
	page []CommentVM
	rows []CommentVM // currently displayed rows (hot tab or normal page)

	tab      int // 0 = 热门, 1 = 最新
	pageIdx  int
	perPage  int
	total    int
	loading  bool
	errMsg   string
	songName string

	// callbacks
	OnRequestPage func(offset, limit int)
	OnRequestHot  func()

	btnHot, btnNew   w32.RECT
	btnPrev, btnNext w32.RECT
	btnRefresh       w32.RECT
	hoverBtn         int
}

func NewCommentsWindow() *CommentsWindow {
	cw := &CommentsWindow{perPage: 20, pageIdx: 0}
	cw.Panel = NewPanel("评论", dp(470), dp(400))
	cw.list = NewListView()
	cw.list.Bg = T.PanelAlt
	cw.list.RowH = func() int32 { return dp(72) }
	cw.list.Count = func() int { return len(cw.rows) }
	cw.list.Draw = cw.drawRow
	cw.Panel.Body = cw.body
	cw.Panel.OnMsg = cw.onMsg
	return cw
}

func (cw *CommentsWindow) SetSong(name string) {
	cw.songName = name
	cw.Panel.Title = "评论 — " + name
	cw.hot = nil
	cw.page = nil
	cw.rows = nil
	cw.tab = 0
	cw.pageIdx = 0
	cw.total = 0
	cw.errMsg = ""
	cw.list.Sel = -1
	cw.list.Top = 0
	cw.refreshRows()
}

func (cw *CommentsWindow) refreshRows() {
	if cw.tab == 0 {
		cw.rows = cw.hot
	} else {
		cw.rows = cw.page
	}
	cw.list.clampTop()
	cw.Panel.Invalidate()
}

// Request issues the load for the current tab/page.
func (cw *CommentsWindow) Request() {
	if cw.OnRequestHot == nil || cw.OnRequestPage == nil {
		return
	}
	cw.loading = true
	cw.errMsg = ""
	if cw.tab == 0 {
		cw.OnRequestHot()
	} else {
		cw.OnRequestPage(cw.pageIdx*cw.perPage, cw.perPage)
	}
	cw.Panel.Invalidate()
}

func (cw *CommentsWindow) SetHot(list []CommentVM) {
	cw.hot = list
	cw.loading = false
	if cw.tab == 0 {
		cw.refreshRows()
	} else {
		cw.Panel.Invalidate()
	}
}

func (cw *CommentsWindow) SetPage(list []CommentVM, total int) {
	cw.page = list
	cw.total = total
	cw.loading = false
	if cw.tab == 1 {
		cw.refreshRows()
	} else {
		cw.Panel.Invalidate()
	}
}

func (cw *CommentsWindow) SetError(msg string) {
	cw.loading = false
	cw.errMsg = msg
	cw.Panel.Invalidate()
}

// SetTotal updates the total comment count shown in the header.
func (cw *CommentsWindow) SetTotal(n int) {
	cw.total = n
	cw.Panel.Invalidate()
}

// ShowLatest switches to the "newest" tab and loads the given page.
func (cw *CommentsWindow) ShowLatest(page int) {
	if page < 0 {
		page = 0
	}
	cw.tab = 1
	cw.pageIdx = page
	cw.refreshRows()
	cw.Request()
}

func (cw *CommentsWindow) layout() {
	top := dp(30)
	hdr := dp(30)
	cw.btnHot = rc(dp(8), top, dp(8) + dp(46), top + dp(22))
	cw.btnNew = rc(dp(58), top, dp(58) + dp(46), top + dp(22))
	cw.btnRefresh = rc(dp(112), top, dp(112) + dp(46), top + dp(22))
	cw.btnPrev = rc(cw.W - dp(150), top, cw.W - dp(108), top + dp(22))
	cw.btnNext = rc(cw.W - dp(104), top, cw.W - dp(62), top + dp(22))
	cw.list.Rect = rc(dp(8), top + hdr, cw.W - dp(8), cw.H - dp(30))
}

func (cw *CommentsWindow) body(dc w32.HDC, client w32.RECT) {
	cw.layout()
	drawChip(dc, cw.btnHot, "热门", cw.hoverBtn == 1, cw.tab == 0)
	drawChip(dc, cw.btnNew, "最新", cw.hoverBtn == 2, cw.tab == 1)
	drawChip(dc, cw.btnRefresh, "刷新", cw.hoverBtn == 3, false)
	if cw.tab == 1 {
		pages := (cw.total + cw.perPage - 1) / cw.perPage
		label := fmt.Sprintf("%d/%d", cw.pageIdx+1, maxInt(pages, 1))
		drawTextIn(dc, rc(cw.btnPrev.Right, dp(30), cw.btnNext.Left, dp(52)), label,
			textOpt{Font: T.FontXS, Color: T.TextDim, Align: w32.DT_CENTER})
		drawChip(dc, cw.btnPrev, "上一页", cw.hoverBtn == 4, false)
		drawChip(dc, cw.btnNext, "下一页", cw.hoverBtn == 5, false)
	} else {
		drawTextIn(dc, rc(cw.btnPrev.Left - dp(60), dp(30), cw.btnNext.Right, dp(52)),
			fmt.Sprintf("共 %d 条", cw.total), textOpt{Font: T.FontXS, Color: T.TextDim, Align: w32.DT_RIGHT})
	}

	cw.list.Paint(dc)

	switch {
	case cw.loading:
		drawTextIn(dc, rc(cw.list.Rect.Left, cw.list.Rect.Top + dp(60), cw.list.Rect.Right, cw.list.Rect.Top + dp(84)),
			"加载中…", textOpt{Font: T.FontS, Color: T.TextDim, Align: w32.DT_CENTER})
	case cw.errMsg != "":
		drawTextIn(dc, rc(cw.list.Rect.Left, cw.list.Rect.Top + dp(60), cw.list.Rect.Right, cw.list.Rect.Top + dp(84)),
			cw.errMsg, textOpt{Font: T.FontS, Color: T.AccentDark, Align: w32.DT_CENTER})
	case len(cw.rows) == 0:
		drawTextIn(dc, rc(cw.list.Rect.Left, cw.list.Rect.Top + dp(60), cw.list.Rect.Right, cw.list.Rect.Top + dp(84)),
			"还没有评论", textOpt{Font: T.FontS, Color: T.TextDim, Align: w32.DT_CENTER})
	}
}

func (cw *CommentsWindow) drawRow(dc w32.HDC, idx int, r w32.RECT, st ListState) {
	if idx < 0 || idx >= len(cw.rows) {
		return
	}
	c := cw.rows[idx]
	if st.Selected {
		fill(dc, r, T.AccentSoft)
	} else if st.Hover {
		fill(dc, r, T.Panel)
	}
	hline(dc, r.Left+dp(10), r.Right-dp(10), r.Bottom-1, T.Hairline)

	// avatar placeholder: a filled circle with the first character
	cxn := r.Left + dp(23)
	cyn := r.Top + dp(25)
	oldBr := w32.SelectObject(dc, w32.HGDIOBJ(T.brush(T.Panel)))
	oldPen := w32.SelectObject(dc, w32.HGDIOBJ(w32.GetStockObject(w32.NULL_PEN)))
	w32.Ellipse(dc, cxn-dp(11), cyn-dp(11), cxn+dp(11), cyn+dp(11))
	w32.SelectObject(dc, oldPen)
	w32.SelectObject(dc, oldBr)
	initial := "网"
	if rs := []rune(c.User); len(rs) > 0 {
		initial = string(rs[0])
	}
	drawTextIn(dc, rc(cxn - dp(11), cyn - dp(11), cxn + dp(11), cyn + dp(11)), initial,
		textOpt{Font: T.FontXS, Color: T.AccentDark, Align: w32.DT_CENTER})

	// user + time
	drawTextIn(dc, rc(r.Left + dp(40), r.Top + dp(6), r.Left + dp(240), r.Top + dp(24)),
		c.User, textOpt{Font: T.FontXS, Color: T.TextMid})
	drawTextIn(dc, rc(r.Left + dp(240), r.Top + dp(6), r.Right - dp(60), r.Top + dp(24)),
		c.TimeStr, textOpt{Font: T.FontXS, Color: T.TextDim})
	// likes
	drawTextIn(dc, rc(r.Right - dp(58), r.Top + dp(6), r.Right - dp(10), r.Top + dp(24)),
		fmt.Sprintf("♥ %d", c.Likes), textOpt{Font: T.FontXS, Color: T.TextDim, Align: w32.DT_RIGHT})
	if c.Hot {
		drawTextIn(dc, rc(r.Right - dp(58), r.Bottom - dp(22), r.Right - dp(10), r.Bottom - dp(6)),
			"热门", textOpt{Font: T.FontXS, Color: T.AccentDark, Align: w32.DT_RIGHT})
	}

	// content: two lines, wrapped, clipped
	body := rc(r.Left + dp(40), r.Top + dp(26), r.Right - dp(10), r.Bottom - dp(6))
	drawWrapped2(dc, body, c.Content, T.FontS, T.TextHi)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (cw *CommentsWindow) hitBtn(x, y int32) int {
	switch {
	case inRect(cw.btnHot, x, y):
		return 1
	case inRect(cw.btnNew, x, y):
		return 2
	case inRect(cw.btnRefresh, x, y):
		return 3
	case cw.tab == 1 && inRect(cw.btnPrev, x, y):
		return 4
	case cw.tab == 1 && inRect(cw.btnNext, x, y):
		return 5
	}
	return 0
}

func (cw *CommentsWindow) onMsg(msg uint32, wp, lp uintptr) (bool, uintptr) {
	x, y := lparamXY(lp)
	switch msg {
	case w32.WM_MOUSEWHEEL:
		delta := int16((wp >> 16) & 0xffff)
		if cw.list.Wheel(int(delta)) {
			w32.Invalidate(cw.HWnd)
		}
		return true, 0
	case w32.WM_MOUSEMOVE:
		hv := cw.hitBtn(x, y)
		changed := hv != cw.hoverBtn
		cw.hoverBtn = hv
		if cw.list.MouseMove(x, y) {
			changed = true
		}
		if changed {
			w32.Invalidate(cw.HWnd)
		}
		return true, 0
	case w32.WM_MOUSELEAVE:
		cw.hoverBtn = 0
		cw.list.Hover = -1
		w32.Invalidate(cw.HWnd)
		return true, 0
	case w32.WM_LBUTTONDOWN:
		if cw.hitBtn(x, y) != 0 {
			return true, 0
		}
		if cw.list.LDown(x, y) {
			w32.Invalidate(cw.HWnd)
		}
	case w32.WM_LBUTTONUP:
		b := cw.hitBtn(x, y)
		cw.list.LUp(x, y)
		switch b {
		case 1:
			if cw.tab != 0 {
				cw.tab = 0
				cw.pageIdx = 0
				cw.refreshRows()
				if len(cw.hot) == 0 {
					cw.Request()
				}
			}
		case 2:
			if cw.tab != 1 {
				cw.tab = 1
				cw.pageIdx = 0
				cw.refreshRows()
				cw.Request()
			}
		case 3:
			cw.Request()
		case 4:
			if cw.pageIdx > 0 {
				cw.pageIdx--
				cw.Request()
			}
		case 5:
			if (cw.pageIdx+1)*cw.perPage < cw.total {
				cw.pageIdx++
				cw.Request()
			}
		}
		w32.Invalidate(cw.HWnd)
		return true, 0
	case w32.WM_KEYDOWN:
		if cw.list.Key(wp) {
			w32.Invalidate(cw.HWnd)
			return true, 0
		}
	}
	return false, 0
}

// FormatRelativeTime renders a comment timestamp the way NetEase does.
func FormatRelativeTime(unix int64) string {
	if unix <= 0 {
		return ""
	}
	now := time.Now()
	t := time.Unix(unix, 0)
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d 小时前", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%d 天前", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}
