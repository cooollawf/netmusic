package ui

import (
	"fmt"
	"os"

	"netmusic/internal/w32"
)

// PlaylistVM is one row in the left sidebar.
type PlaylistVM struct {
	Name  string
	Count int
	Queue bool // the "播放队列" pseudo playlist
}

// TrackVM is one row in the song list.
type TrackVM struct {
	Title   string
	Artist  string
	Dur     int // seconds
	Playing bool
}

// PlaylistWindow is the 播放列表 window: playlists on the left, songs on the
// right, search box on top. Both lists are virtualised.
type PlaylistWindow struct {
	*Panel
	side  *ListView
	songs *ListView

	playlists []PlaylistVM
	tracks    []TrackVM
	header    string
	inQueue   bool
	sideSel   int

	edit        uintptr
	btnSearch   w32.RECT
	btnAddQueue w32.RECT
	btnUp       w32.RECT
	btnDown     w32.RECT
	btnDel      w32.RECT
	hoverBtn    int

	OnPlaylistSelect func(idx int)
	OnTrackActivate  func(idx int)
	OnSearch         func(keyword string)
	OnQueueAction    func(kind int) // 0=up 1=down 2=remove
}

func NewPlaylistWindow() *PlaylistWindow {
	pw := &PlaylistWindow{sideSel: -1}
	pw.Panel = NewPanel("播放列表", dp(470), dp(430))
	pw.side = NewListView()
	pw.side.Bg = T.Panel
	pw.side.RowH = func() int32 { return dp(26) }
	pw.side.Count = func() int { return len(pw.playlists) }
	pw.side.Draw = pw.drawPlaylist
	pw.side.OnSelect = func(idx int) {
		pw.sideSel = idx
		if pw.OnPlaylistSelect != nil {
			pw.OnPlaylistSelect(idx)
		}
	}

	pw.songs = NewListView()
	pw.songs.Bg = T.PanelAlt
	pw.songs.RowH = func() int32 { return dp(24) }
	pw.songs.Count = func() int { return len(pw.tracks) }
	pw.songs.Draw = pw.drawTrack
	pw.songs.OnActivate = func(idx int) {
		if pw.OnTrackActivate != nil {
			pw.OnTrackActivate(idx)
		}
	}
	pw.Panel.Body = pw.body
	pw.Panel.OnMsg = pw.onMsg
	return pw
}

func (pw *PlaylistWindow) layout() {
	top := dp(29)
	searchH := dp(24)
	sideW := dp(128)

	pw.side.Rect = rc(dp(8), top + dp(4), dp(8) + sideW, pw.H - dp(8))

	rx := dp(8) + sideW + dp(10)
	rw := pw.W - dp(8) - rx

	pw.editW(rx, top)
	pw.btnSearch = rc(rx + rw - dp(46), top, rx + rw, top + searchH)

	hy := top + searchH + dp(4)
	hdr := rc(rx, hy, rx + rw, hy + dp(20))

	listTop := hdr.Bottom + dp(2)
	bottom := pw.H - dp(8)
	if pw.inQueue {
		bottom -= dp(26)
	}
	pw.songs.Rect = rc(rx, listTop, rx + rw, bottom)

	by := pw.H - dp(28)
	pw.btnAddQueue = rc(rx, by, rx + dp(64), by + dp(20))
	pw.btnUp = rc(rx + dp(70), by, rx + dp(70) + dp(46), by + dp(20))
	pw.btnDown = rc(rx + dp(120), by, rx + dp(120) + dp(46), by + dp(20))
	pw.btnDel = rc(rx + dp(170), by, rx + dp(170) + dp(46), by + dp(20))
	_ = hdr
	_ = rw
}

// editW keeps the search field in sync with the layout.
func (pw *PlaylistWindow) editW(rx, top int32) {
	sideW := dp(128)
	rw := pw.W - dp(8) - (dp(8) + sideW + dp(10))
	w := rw - dp(52)
	if pw.edit != 0 {
		// one pixel inside the painted frame so the border stays visible
		w32.MoveWindow(pw.edit, rx+dp(1), top+dp(4), w-dp(2), dp(18), true)
	}
}

func (pw *PlaylistWindow) EnsureEdit() {
	if pw.edit != 0 {
		return
	}
	sideW := dp(128)
	rx := dp(8) + sideW + dp(10)
	rw := pw.W - dp(8) - rx
	pw.edit = NewEdit(pw.HWnd, 101, rx+dp(1), dp(29)+dp(4), rw-dp(54), dp(18), "搜索歌曲")
	if pw.edit != 0 {
		SetEditEnter(pw.edit, func() {
			if pw.OnSearch != nil {
				pw.OnSearch(EditText(pw.edit))
			}
		})
	}
}

func (pw *PlaylistWindow) SetPlaylists(list []PlaylistVM) {
	pw.playlists = list
	pw.side.clampTop()
	pw.Panel.Invalidate()
}

func (pw *PlaylistWindow) SetTracks(tracks []TrackVM, header string, inQueue bool) {
	pw.tracks = tracks
	pw.header = header
	pw.inQueue = inQueue
	pw.songs.Top = 0
	pw.songs.Sel = -1
	pw.layout()
	pw.Panel.Invalidate()
}

// SelectedTrack returns the selected song row, or -1.
func (pw *PlaylistWindow) SelectedTrack() int { return pw.songs.Sel }

// SelectTrack highlights a song row.
func (pw *PlaylistWindow) SelectTrack(i int) {
	pw.songs.Sel = i
	pw.songs.ensureVisible(i)
	pw.Panel.Invalidate()
}

// SelectPlaylist highlights a sidebar row.
func (pw *PlaylistWindow) SelectPlaylist(i int) {
	pw.side.Sel = i
	pw.sideSel = i
	pw.side.ensureVisible(i)
	pw.Panel.Invalidate()
}

// MarkPlaying highlights the row of the track that is playing.
func (pw *PlaylistWindow) MarkPlaying(idx int) {
	pw.songs.Sel = idx
	pw.songs.ensureVisible(idx)
	pw.Panel.Invalidate()
}

func (pw *PlaylistWindow) body(dc w32.HDC, client w32.RECT) {
	pw.layout()
	pw.side.Paint(dc)
	pw.songs.Paint(dc)

	// search field frame + placeholder (the EDIT child paints over the inside)
	sideW := dp(128)
	rx := dp(8) + sideW + dp(10)
	rw := pw.W - dp(8) - rx
	er := rc(rx, dp(32), rx + rw - dp(52), dp(52))
	roundStroke(dc, er, dp(4), T.Hairline)
	drawChip(dc, pw.btnSearch, "搜索", pw.hoverBtn == 1, false)

	if len(pw.playlists) == 0 {
		drawTextIn(dc, rc(pw.side.Rect.Left, pw.side.Rect.Top + dp(20), pw.side.Rect.Right, pw.side.Rect.Top + dp(44)),
			"未登录", textOpt{Font: T.FontXS, Color: T.TextDim, Align: w32.DT_CENTER})
	}

	// header line above the song list
	hy := dp(29) + dp(24) + dp(4)
	drawTextIn(dc, rc(rx, hy, pw.W - dp(8), hy + dp(20)), pw.header,
		textOpt{Font: T.FontXS, Color: T.TextMid})

	drawChip(dc, pw.btnAddQueue, "加入队列", pw.hoverBtn == 2, false)
	if pw.inQueue {
		drawChip(dc, pw.btnUp, "上移", pw.hoverBtn == 3, false)
		drawChip(dc, pw.btnDown, "下移", pw.hoverBtn == 4, false)
		drawChip(dc, pw.btnDel, "移除", pw.hoverBtn == 5, false)
	}
}

func (pw *PlaylistWindow) drawPlaylist(dc w32.HDC, idx int, r w32.RECT, st ListState) {
	if idx < 0 || idx >= len(pw.playlists) {
		return
	}
	p := pw.playlists[idx]
	if st.Selected {
		fill(dc, r, T.AccentSoft)
		vline(dc, r.Left, r.Top, r.Bottom, T.Accent)
	} else if st.Hover {
		fill(dc, r, T.PanelAlt)
	}
	rr := rc(r.Left + dp(10), r.Top, r.Right - dp(6), r.Bottom)
	c := T.TextMid
	if st.Selected {
		c = T.TextHi
	}
	drawTextIn(dc, rr, p.Name, textOpt{Font: T.FontXS, Color: c})
	if p.Count > 0 {
		drawTextIn(dc, rr, fmt.Sprintf("%d", p.Count), textOpt{Font: T.FontXS, Color: T.TextDim, Align: w32.DT_RIGHT})
	}
}

func (pw *PlaylistWindow) drawTrack(dc w32.HDC, idx int, r w32.RECT, st ListState) {
	if idx < 0 || idx >= len(pw.tracks) {
		return
	}
	t := pw.tracks[idx]
	if st.Selected {
		fill(dc, r, T.AccentSoft)
	} else if st.Hover {
		fill(dc, r, T.Panel)
	}
	if t.Playing {
		vline(dc, r.Left, r.Top + dp(3), r.Bottom - dp(3), T.Accent)
	}
	durW := dp(44)
	col := T.TextHi
	if t.Playing {
		col = T.Accent
	}
	title := rc(r.Left + dp(10), r.Top, r.Right - durW - dp(150), r.Bottom)
	drawTextIn(dc, title, t.Title, textOpt{Font: T.FontS, Color: col})
	artist := rc(r.Right - durW - dp(146), r.Top, r.Right - durW - dp(6), r.Bottom)
	drawTextIn(dc, artist, t.Artist, textOpt{Font: T.FontXS, Color: T.TextDim, Align: w32.DT_RIGHT})
	drawTextIn(dc, rc(r.Right - durW, r.Top, r.Right - dp(10), r.Bottom), mmss(t.Dur),
		textOpt{Font: T.FontXS, Color: T.TextDim, Align: w32.DT_RIGHT})
}

func mmss(sec int) string {
	if sec <= 0 {
		return "-:--"
	}
	return fmt.Sprintf("%d:%02d", sec/60, sec%60)
}

func (pw *PlaylistWindow) hitBtn(x, y int32) int {
	switch {
	case inRect(pw.btnSearch, x, y):
		return 1
	case inRect(pw.btnAddQueue, x, y):
		return 2
	}
	if pw.inQueue {
		switch {
		case inRect(pw.btnUp, x, y):
			return 3
		case inRect(pw.btnDown, x, y):
			return 4
		case inRect(pw.btnDel, x, y):
			return 5
		}
	}
	return 0
}

func (pw *PlaylistWindow) onMsg(msg uint32, wp, lp uintptr) (bool, uintptr) {
	x, y := lparamXY(lp)
	switch msg {
	case w32.WM_MOUSEWHEEL:
		delta := int16((wp >> 16) & 0xffff)
		pt := w32.POINT{X: x, Y: y}
		w32.ScreenToClient(pw.HWnd, &pt)
		if inRect(pw.side.Rect, pt.X, pt.Y) {
			if pw.side.Wheel(int(delta)) {
				w32.Invalidate(pw.HWnd)
			}
		} else if pw.songs.Wheel(int(delta)) {
			w32.Invalidate(pw.HWnd)
		}
		return true, 0
	case w32.WM_CTLCOLOREDIT:
		hdc := w32.HDC(wp)
		w32.SetBkColor(hdc, T.PanelAlt)
		w32.SetTextColor(hdc, T.TextHi)
		if os.Getenv("NETMUSIC_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "CTLCOLOREDIT hdc=%v brush=%v\n", hdc, T.BrushHandle(T.PanelAlt))
		}
		return true, uintptr(T.BrushHandle(T.PanelAlt))
	case w32.WM_MOUSEMOVE:
		hv := pw.hitBtn(x, y)
		changed := hv != pw.hoverBtn
		pw.hoverBtn = hv
		if pw.side.MouseMove(x, y) || pw.songs.MouseMove(x, y) {
			changed = true
		}
		if changed {
			w32.Invalidate(pw.HWnd)
		}
		return true, 0
	case w32.WM_MOUSELEAVE:
		pw.hoverBtn = 0
		pw.side.Hover, pw.songs.Hover = -1, -1
		w32.Invalidate(pw.HWnd)
		return true, 0
	case w32.WM_LBUTTONDOWN:
		if pw.hitBtn(x, y) != 0 {
			return true, 0
		}
		if pw.side.LDown(x, y) || pw.songs.LDown(x, y) {
			w32.Invalidate(pw.HWnd)
		}
	case w32.WM_LBUTTONUP:
		b := pw.hitBtn(x, y)
		pw.side.LUp(x, y)
		pw.songs.LUp(x, y)
		switch b {
		case 1:
			if pw.OnSearch != nil && pw.edit != 0 {
				pw.OnSearch(EditText(pw.edit))
			}
		case 2:
			if pw.songs.Sel >= 0 && pw.OnQueueAction != nil {
				pw.OnQueueAction(10) // add to queue
			}
		case 3:
			if pw.songs.Sel > 0 && pw.OnQueueAction != nil {
				pw.OnQueueAction(0)
			}
		case 4:
			if pw.OnQueueAction != nil {
				pw.OnQueueAction(1)
			}
		case 5:
			if pw.OnQueueAction != nil {
				pw.OnQueueAction(2)
			}
		}
		w32.Invalidate(pw.HWnd)
		return true, 0
	case w32.WM_LBUTTONDBLCLK:
		pw.songs.DblClick(x, y)
		return true, 0
	case w32.WM_KEYDOWN:
		if pw.songs.Key(wp) {
			w32.Invalidate(pw.HWnd)
			return true, 0
		}
	}
	return false, 0
}
