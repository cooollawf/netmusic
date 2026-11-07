package w32

import (
	"sync"
	"syscall"
	"unsafe"
)

// ---------------------------------------------------------------------------
// window styles / messages
// ---------------------------------------------------------------------------

const (
	WS_OVERLAPPED    = 0x00000000
	WS_POPUP         = 0x80000000
	WS_CHILD         = 0x40000000
	WS_MINIMIZE      = 0x20000000
	WS_VISIBLE       = 0x10000000
	WS_DISABLED      = 0x08000000
	WS_CLIPSIBLINGS  = 0x04000000
	WS_CLIPCHILDREN  = 0x02000000
	WS_BORDER        = 0x00800000
	WS_CAPTION       = 0x00C00000
	WS_VSCROLL       = 0x00200000
	WS_HSCROLL       = 0x00100000
	WS_SYSMENU       = 0x00080000
	WS_THICKFRAME    = 0x00040000
	WS_MINIMIZEBOX   = 0x00020000
	WS_MAXIMIZEBOX   = 0x00010000

	SW_HIDE     = 0
	SW_SHOWNORMAL = 1
	SW_SHOW     = 5
	SW_MINIMIZE = 6
	SW_RESTORE  = 9

	HWND_TOP       = 0
	HWND_TOPMOST   = ^uintptr(0)
	HWND_NOTOPMOST = ^uintptr(1)
	SWP_NOSIZE     = 0x0001
	SWP_NOMOVE     = 0x0002
	SWP_NOZORDER   = 0x0004
	SWP_NOACTIVATE = 0x0010
	SWP_FRAMECHANGED = 0x0020
	SWP_SHOWWINDOW   = 0x0040

	WM_CREATE         = 0x0001
	WM_DESTROY        = 0x0002
	WM_MOVE           = 0x0003
	WM_SIZE           = 0x0005
	WM_ACTIVATE       = 0x0006
	WM_SETFOCUS       = 0x0007
	WM_KILLFOCUS      = 0x0008
	WM_PAINT          = 0x000F
	WM_CLOSE          = 0x0010
	WM_QUIT           = 0x0012
	WM_ERASEBKGND     = 0x0014
	WM_SETCURSOR      = 0x0020
	WM_GETMINMAXINFO  = 0x0024
	WM_COMMAND        = 0x0111
	WM_SYSCOMMAND     = 0x0112
	WM_TIMER          = 0x0113
	WM_HSCROLL        = 0x0114
	WM_VSCROLL        = 0x0115
	WM_MOUSEMOVE      = 0x0200
	WM_LBUTTONDOWN    = 0x0201
	WM_LBUTTONUP      = 0x0202
	WM_LBUTTONDBLCLK  = 0x0203
	WM_RBUTTONDOWN    = 0x0204
	WM_RBUTTONUP      = 0x0205
	WM_MOUSEWHEEL     = 0x020A
	WM_MOUSELEAVE     = 0x02A3
	WM_KEYDOWN        = 0x0100
	WM_KEYUP          = 0x0101
	WM_CHAR           = 0x0102
	WM_SETTEXT        = 0x000C
	WM_GETTEXT        = 0x000D
	WM_GETTEXTLENGTH  = 0x000E
	WM_NOTIFY         = 0x004E
	WM_CTLCOLOREDIT   = 0x0133
	WM_CTLCOLORLISTBOX = 0x0134
	WM_CTLCOLORSTATIC = 0x0138
	WM_NCHITTEST      = 0x0084
	WM_APP            = 0x8000
	WM_USER           = 0x0400

	HTTRANSPARENT = ^uintptr(0)
	HTNOWHERE     = 0
	HTCLIENT      = 1
	HTCAPTION     = 2
	HTLEFT        = 10
	HTRIGHT       = 11
	HTTOP         = 12
	HTTOPLEFT     = 13
	HTTOPRIGHT    = 14
	HTBOTTOM      = 15
	HTBOTTOMLEFT  = 16
	HTBOTTOMRIGHT = 17

	CS_VREDRAW = 0x0001
	CS_HREDRAW = 0x0002
	CS_DBLCLKS = 0x0008
	CS_OWNDC   = 0x0020

	IDC_ARROW = 32512
	IDC_HAND  = 32649
	IDC_IBEAM = 32513

	// edit / button / listbox styles
	ES_AUTOHSCROLL = 0x0080
	ES_LEFT        = 0x0000
	ES_READONLY    = 0x0800
	BS_PUSHBUTTON  = 0x00000000
	BS_OWNERDRAW   = 0x0000000B
	LBS_NOTIFY     = 0x0001
	LBS_NOINTEGRALHEIGHT = 0x0100
	LBS_OWNERDRAWFIXED   = 0x0010
	EN_CHANGE      = 0x0300
	LBN_SELCHANGE  = 0x0001
	LBN_DBLCLK     = 0x0002
	SB_VERT        = 1
	SB_HORZ        = 0
	SIF_ALL        = 0x0017
	SIF_RANGE      = 0x0001
	SIF_PAGE       = 0x0002
	SIF_POS        = 0x0004
	SB_THUMBTRACK  = 5
	SB_THUMBPOSITION = 4
	SB_LINEDOWN    = 1
	SB_LINEUP      = 0
	SB_PAGEDOWN    = 3
	SB_PAGEUP      = 2
	SB_TOP         = 6
	SB_BOTTOM      = 7
)

// ---------------------------------------------------------------------------
// handler plumbing
// ---------------------------------------------------------------------------

// Handler receives window callbacks. Every method runs on the UI thread.
type Handler interface {
	OnCreate(hwnd uintptr) bool
	OnPaint(hwnd uintptr, hdc HDC, client RECT)
	OnMessage(hwnd uintptr, msg uint32, wp, lp uintptr) (handled bool, result uintptr)
	OnDestroy(hwnd uintptr)
}

var (
	regMu     sync.RWMutex
	regWnds   = map[uintptr]Handler{}
	// nextHandler is consumed by WM_CREATE, which fires synchronously inside
	// CreateWindowEx on the UI thread, so a plain global is race free and
	// avoids passing a Go pointer through lpCreateParams.
	nextHandler Handler
	classSeq    int
	wndProcCB   = syscall.NewCallback(wndProc)
)

func registerWindow(hwnd uintptr, h Handler) {
	regMu.Lock()
	regWnds[hwnd] = h
	regMu.Unlock()
}

func unregisterWindow(hwnd uintptr) {
	regMu.Lock()
	delete(regWnds, hwnd)
	regMu.Unlock()
}

func lookupWindow(hwnd uintptr) Handler {
	regMu.RLock()
	h := regWnds[hwnd]
	regMu.RUnlock()
	return h
}

func takePendingHandler() Handler {
	h := nextHandler
	nextHandler = nil
	return h
}

type createStruct struct {
	LpCreateParams uintptr
	HInstance      uintptr
	HMenu          uintptr
	HwndParent     uintptr
	Cy             int32
	Cx             int32
	Y              int32
	X              int32
	Style          int32
	LpszName       uintptr
	LpszClass      uintptr
	DwExStyle      uint32
}

func wndProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	switch msg {
	case WM_CREATE:
		h := takePendingHandler()
		if h == nil {
			return ^uintptr(0)
		}
		registerWindow(hwnd, h)
		if !h.OnCreate(hwnd) {
			unregisterWindow(hwnd)
			return ^uintptr(0)
		}
		return 0
	case WM_PAINT:
		var ps PAINTSTRUCT
		hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		h := lookupWindow(hwnd)
		if h == nil {
			hdc = 0
			h = nil
		}
		if h != nil {
			h.OnPaint(hwnd, HDC(hdc), ps.RcPaint)
		}
		procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case WM_ERASEBKGND:
		return 1
	case WM_DESTROY:
		if h := lookupWindow(hwnd); h != nil {
			h.OnDestroy(hwnd)
		}
		unregisterWindow(hwnd)
		return 0
	}
	if h := lookupWindow(hwnd); h != nil {
		if handled, res := h.OnMessage(hwnd, msg, wp, lp); handled {
			return res
		}
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

// ---------------------------------------------------------------------------
// window class + creation
// ---------------------------------------------------------------------------

// Class is a registered window class.
type Class struct{ Name string }

// RegisterClass registers a private window class routed through Handler.
func RegisterClass(style uint32, cursor uintptr) *Class {
	classSeq++
	name := "nmwnd" + itoa(classSeq)
	cn, _ := syscall.UTF16PtrFromString(name)
	wc := WNDCLASSEX{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEX{})),
		Style:         style,
		LpfnWndProc:   wndProcCB,
		HInstance:     ModHandle(),
		HCursor:       cursor,
		HbrBackground: 0,
		LpszClassName: cn,
	}
	procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	return &Class{Name: name}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// Create creates a top level window bound to spec.Handler.
func (c *Class) Create(spec WindowSpec) uintptr {
	cn, _ := syscall.UTF16PtrFromString(c.Name)
	title, err := syscall.UTF16PtrFromString(spec.Title)
	if err != nil {
		title, _ = syscall.UTF16PtrFromString("")
	}
	tok := uintptr(0)
	nextHandler = spec.Handler
	x, y := spec.X, spec.Y
	if spec.Center {
		sw := GetSystemMetrics(0)
		sh := GetSystemMetrics(1)
		x = (int32(sw) - spec.W) / 2
		y = (int32(sh) - spec.H) / 3
	}
	hwnd, _, _ := procCreateWindowEx.Call(
		uintptr(spec.ExStyle),
		uintptr(unsafe.Pointer(cn)),
		uintptr(unsafe.Pointer(title)),
		uintptr(spec.Style),
		uintptr(x), uintptr(y), uintptr(spec.W), uintptr(spec.H),
		spec.Owner, 0, ModHandle(), tok,
	)
	return hwnd
}

type WindowSpec struct {
	Title   string
	X, Y    int32
	W, H    int32
	Style   uint32
	ExStyle uint32
	Owner   uintptr // owner window for popups (hidden/destroyed with it)
	Handler Handler
	Center  bool
}

var procGetSystemMetrics = user32.NewProc("GetSystemMetrics")

func GetSystemMetrics(i int) int32 {
	r, _, _ := procGetSystemMetrics.Call(uintptr(i))
	return int32(r)
}

func ScreenWorkArea() RECT {
	// SM_CXSCREEN=0, SM_CYSCREEN=1
	return RECT{0, 0, GetSystemMetrics(0), GetSystemMetrics(1)}
}

// ---------------------------------------------------------------------------
// window helpers
// ---------------------------------------------------------------------------

func Show(hwnd uintptr, cmd int32) { procShowWindow.Call(hwnd, uintptr(cmd)) }
func Update(hwnd uintptr)          { procUpdateWindow.Call(hwnd) }
func Destroy(hwnd uintptr)         { procDestroyWindow.Call(hwnd) }

func SetTitle(hwnd uintptr, s string) {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		return
	}
	procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(p)))
}

func GetWindowText(hwnd uintptr) string {
	n, _, _ := procSendMessageW.Call(hwnd, WM_GETTEXTLENGTH, 0, 0)
	if int32(n) <= 0 {
		return ""
	}
	buf := make([]uint16, int(n)+2)
	procSendMessageW.Call(hwnd, WM_GETTEXT, uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])))
	for i, c := range buf {
		if c == 0 {
			return string(utf16Decode(buf[:i]))
		}
	}
	return string(utf16Decode(buf))
}

func utf16Decode(u []uint16) []rune {
	out := make([]rune, 0, len(u))
	for i := 0; i < len(u); i++ {
		c := rune(u[i])
		if c >= 0xD800 && c <= 0xDBFF && i+1 < len(u) {
			c2 := rune(u[i+1])
			if c2 >= 0xDC00 && c2 <= 0xDFFF {
				out = append(out, ((c-0xD800)<<10 | (c2 - 0xDC00)) + 0x10000)
				i++
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

func GetClientRect(hwnd uintptr) RECT {
	var rc RECT
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	return rc
}

func GetWindowRect(hwnd uintptr) RECT {
	var rc RECT
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	return rc
}

func MoveWindow(hwnd uintptr, x, y, w, h int32, repaint bool) {
	rp := uintptr(0)
	if repaint {
		rp = 1
	}
	procMoveWindow.Call(hwnd, uintptr(x), uintptr(y), uintptr(w), uintptr(h), rp)
}

func SetWindowPos(hwnd, after uintptr, x, y, w, h int32, flags uint32) {
	procSetWindowPos.Call(hwnd, after, uintptr(x), uintptr(y), uintptr(w), uintptr(h), uintptr(flags))
}

func Invalidate(hwnd uintptr) { procInvalidateRect.Call(hwnd, 0, 0) }

func InvalidateRect(hwnd uintptr, rc *RECT, erase bool) {
	e := uintptr(0)
	if erase {
		e = 1
	}
	procInvalidateRect.Call(hwnd, uintptr(unsafe.Pointer(rc)), e)
}

func PostMessage(hwnd uintptr, msg uint32, wp, lp uintptr) {
	procPostMessageW.Call(hwnd, uintptr(msg), wp, lp)
}

func SendMessage(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	r, _, _ := procSendMessageW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

func PostQuitMessage() { procPostQuitMessage.Call(0) }

// RunMessageLoop pumps messages until WM_QUIT.
func RunMessageLoop() int {
	var msg MSG
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMsg.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMsg.Call(uintptr(unsafe.Pointer(&msg)))
	}
	return int(msg.WParam)
}

func SetTimer(hwnd, id uintptr, ms uint32) uintptr {
	r, _, _ := procSetTimer.Call(hwnd, id, uintptr(ms), 0)
	return r
}

func KillTimer(hwnd, id uintptr) { procKillTimer.Call(hwnd, id) }

func SetCapture(hwnd uintptr) { procSetCapture.Call(hwnd) }
func ReleaseCapture()         { procReleaseCapture.Call() }
func SetFocus(hwnd uintptr)   { procSetFocus.Call(hwnd) }
func IsVisible(hwnd uintptr) bool {
	r, _, _ := procIsWindowVisible.Call(hwnd)
	return r != 0
}

func CursorPos() POINT {
	var p POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p
}

func ScreenToClient(hwnd uintptr, p *POINT) {
	procScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(p)))
}

func ClientToScreen(hwnd uintptr, p *POINT) {
	procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(p)))
}

func SetForeground(hwnd uintptr) { procSetForeground.Call(hwnd) }

func MessageBox(hwnd uintptr, text, caption string) int {
	t, _ := syscall.UTF16PtrFromString(text)
	c, _ := syscall.UTF16PtrFromString(caption)
	r, _, _ := procMessageBoxW.Call(hwnd, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), 0x40)
	return int(r)
}

func LoadCursor(id uintptr) uintptr {
	r, _, _ := procLoadCursorW.Call(0, id)
	return r
}

func SetCursor(h uintptr) { procSetCursor.Call(h) }

// IsHotkeyDown reports whether a virtual key is currently pressed.
func IsHotkeyDown(vk int) bool {
	r, _, _ := procGetAsyncKey.Call(uintptr(vk))
	return r&0x8000 != 0
}

const (
	WS_EX_TOOLWINDOW = 0x00000080
	WS_EX_APPWINDOW  = 0x00040000
)

// ---------------------------------------------------------------------------
// child controls
// ---------------------------------------------------------------------------

const (
	LVS_REPORT           = 0x0001
	LVS_SINGLESEL        = 0x0004
	LVS_SHOWSELALWAYS    = 0x0008
	LVS_NOSORTHEADER     = 0x8000
	LVS_OWNERDATA        = 0x1000
	LVS_NOCOLUMNHEADER   = 0x4000
	LVS_EX_FULLROWSELECT = 0x00000020
	LVS_EX_DOUBLEBUFFER  = 0x00010000

	LVM_FIRST                    = 0x1000
	LVM_SETEXTENDEDLISTVIEWSTYLE = LVM_FIRST + 54
	LVM_GETEXTENDEDLISTVIEWSTYLE = LVM_FIRST + 55
	LVM_INSERTCOLUMNW            = LVM_FIRST + 97
	LVM_DELETEALLITEMS           = LVM_FIRST + 9
	LVM_SETITEMCOUNT             = LVM_FIRST + 47
	LVM_GETNEXTITEM              = LVM_FIRST + 12
	LVM_ENSUREVISIBLE            = LVM_FIRST + 19
	LVM_SETITEMSTATE             = LVM_FIRST + 43
	LVM_GETTOPINDEX              = LVM_FIRST + 27
	LVM_GETCOUNTPERPAGE          = LVM_FIRST + 40
	LVM_GETITEMCOUNT             = LVM_FIRST + 4
	LVM_SETBKCOLOR               = LVM_FIRST + 1
	LVM_SETTEXTCOLOR             = LVM_FIRST + 36
	LVM_SETTEXTBKCOLOR           = LVM_FIRST + 38
	LVM_SCROLL                   = LVM_FIRST + 20
	LVM_REDRAWITEMS              = LVM_FIRST + 21
	LVM_UPDATE                   = LVM_FIRST + 42
	LVM_SETITEMSPACING           = LVM_FIRST + 17

	LVNI_SELECTED = 0x0002
	LVIF_TEXT     = 0x0001
	LVIF_PARAM    = 0x0004
	LVIF_STATE    = 0x0008
	LVIS_SELECTED = 0x0002
	LVIS_FOCUSED  = 0x0001

	LVN_FIRST = ^uint32(99)
	LVN_ITEMCHANGED    = LVN_FIRST - 1
	LVN_GETDISPINFOW   = LVN_FIRST - 77
	LVN_COLUMNCLICK    = LVN_FIRST - 8
	LVN_ODSTATECHANGED = LVN_FIRST - 47

	NM_CUSTOMDRAW     = ^uint32(11)
	CDDS_PREPAINT     = 0x00000001
	CDDS_ITEMPREPAINT = 0x00010001
	CDRF_DODEFAULT       = 0x00000000
	CDRF_NOTIFYITEMDRAW  = 0x00000020
	CDRF_SKIPDEFAULT     = 0x00000004
)

type LVCOLUMN struct {
	Mask       uint32
	Fmt        int32
	Cx         int32
	PszText    *uint16
	CchTextMax int32
	ISubItem   int32
	IImage     int32
	IOrder     int32
	CxMin      int32
	CxDefault  int32
	IColumn    int32
}

type LVITEM struct {
	Mask       uint32
	IItem      int32
	ISubItem   int32
	State      uint32
	StateMask  uint32
	PszText    *uint16
	CchTextMax int32
	IImage     int32
	LParam     uintptr
	IIndent    int32
	IGroupID   int32
	CColumns   uint32
	PuColumns  uintptr
	PiColFmt   uintptr
}

type NMLVDISPINFO struct {
	Hdr  NMHDR
	Item LVITEM
}

type NMLISTVIEW struct {
	Hdr      NMHDR
	IItem    int32
	ISubItem int32
	NewState uint32
	OldState uint32
	Changed  uint32
	PtAction POINT
	LParam   uintptr
}

// NMLVCUSTOMDRAW mirrors the Win32 layout exactly (x64).
type NMLVCUSTOMDRAW struct {
	Hdr        NMHDR
	DrawStage  uint32
	_          uint32
	HDc        HDC
	Rc         RECT
	ItemSpec   uintptr
	ItemState  uint32
	LItemParam int32
	ClrText    uint32
	ClrTextBk  uint32
	ISubItem   int32
	ItemType   uint32
	ClrFace    uint32
	IconEffect int32
	IconPhase  int32
	PartID     int32
	StateID    int32
	RcText     RECT
	Align      uint32
}

// CreateChild creates a child control of a standard window class.
func CreateChild(parent uintptr, class, text string, style, exStyle uint32, id int, x, y, w, h int32) uintptr {
	cn, err := syscall.UTF16PtrFromString(class)
	if err != nil {
		return 0
	}
	tp, err := syscall.UTF16PtrFromString(text)
	if err != nil {
		tp, _ = syscall.UTF16PtrFromString("")
	}
	hwnd, _, _ := procCreateWindowEx.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(cn)),
		uintptr(unsafe.Pointer(tp)),
		uintptr(WS_CHILD|WS_VISIBLE|style),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		parent, uintptr(id), ModHandle(), 0,
	)
	return hwnd
}

func ListViewInsertColumn(hwnd uintptr, iSub int32, title string, width int32, align int32, fmtMask uint32) {
	p, _ := syscall.UTF16PtrFromString(title)
	col := LVCOLUMN{Mask: 0x0002 | 0x0008, Cx: width, PszText: p, ISubItem: iSub, Fmt: align}
	procSendMessageW.Call(hwnd, LVM_INSERTCOLUMNW, uintptr(iSub), uintptr(unsafe.Pointer(&col)))
	_ = fmtMask
}

func ListViewSetItemCount(hwnd uintptr, n int32) {
	procSendMessageW.Call(hwnd, LVM_SETITEMCOUNT, uintptr(n), 0)
}

func ListViewEnsureVisible(hwnd uintptr, idx int32) {
	procSendMessageW.Call(hwnd, LVM_ENSUREVISIBLE, uintptr(idx), 1)
}

func ListViewSelected(hwnd uintptr) int32 {
	r, _, _ := procSendMessageW.Call(hwnd, LVM_GETNEXTITEM, ^uintptr(0), uintptr(LVNI_SELECTED))
	return int32(r)
}

func ListViewSelect(hwnd uintptr, idx int32, selected bool) {
	var item LVITEM
	item.StateMask = uint32(LVIS_SELECTED | LVIS_FOCUSED)
	if selected {
		item.State = uint32(LVIS_SELECTED | LVIS_FOCUSED)
	}
	procSendMessageW.Call(hwnd, LVM_SETITEMSTATE, uintptr(idx), uintptr(unsafe.Pointer(&item)))
}

func ListViewSetExtStyle(hwnd uintptr, flags uint32) {
	procSendMessageW.Call(hwnd, LVM_SETEXTENDEDLISTVIEWSTYLE, 0, uintptr(flags))
}

func ListViewDeleteAll(hwnd uintptr) {
	procSendMessageW.Call(hwnd, LVM_DELETEALLITEMS, 0, 0)
}

func SetWindowFont(hwnd, font uintptr) {
	const WM_SETFONT = 0x0030
	procSendMessageW.Call(hwnd, WM_SETFONT, font, 1)
}

// SetWindowLongPtr changes a window's data or window procedure.
//
// SetWindowLongPtrW / GetWindowLongPtrW only exist on 64-bit Windows; on 32-bit
// they are SDK macros over the non-Ptr variants and user32.dll exports no such
// name. Resolve lazily so one code path serves both — on a 32-bit Windows 7
// target the Ptr variant would otherwise fail at first use.
func SetWindowLongPtr(hwnd uintptr, index int, value uintptr) uintptr {
	if err := procSetWindowLongPtr.Find(); err == nil {
		r, _, _ := procSetWindowLongPtr.Call(hwnd, uintptr(index), value)
		return r
	}
	r, _, _ := procSetWindowLong.Call(hwnd, uintptr(index), value)
	return r
}

// GetWindowLongPtr mirrors SetWindowLongPtr.
func GetWindowLongPtr(hwnd uintptr, index int) uintptr {
	if err := procGetWindowLongPtr.Find(); err == nil {
		r, _, _ := procGetWindowLongPtr.Call(hwnd, uintptr(index))
		return r
	}
	r, _, _ := procGetWindowLong.Call(hwnd, uintptr(index))
	return r
}

// DefWindowProc is needed by subclasses that want default processing.
func DefWindowProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

const (
	GWLP_WNDPROC  = -4
	GWLP_USERDATA = -21
	GWLP_HINSTANCE = -6
)

func ShowWindow(hwnd uintptr, cmd uintptr) { procShowWindow.Call(hwnd, cmd) }

func ShowScrollBar(hwnd uintptr, bar int32, show bool) {
	s := uintptr(0)
	if show {
		s = 1
	}
	procShowScrollBar.Call(hwnd, uintptr(bar), s)
}

// ---------------------------------------------------------------------------
// back buffer
// ---------------------------------------------------------------------------

// BackBuffer is a per-window double buffer. It reallocates only when the
// window grows, so steady state painting allocates nothing.
type BackBuffer struct {
	dib *DIB
}

// DC returns a DC whose surface is at least w x h, plus the buffer size.
func (b *BackBuffer) DC(w, h int32) (HDC, int32, int32) {
	if w <= 0 || h <= 0 {
		return 0, 0, 0
	}
	if b.dib == nil || b.dib.W < w || b.dib.H < h {
		if b.dib != nil {
			b.dib.Free()
		}
		b.dib = NewDIB(w, h)
	}
	if b.dib == nil {
		return 0, 0, 0
	}
	return b.dib.DC, b.dib.W, b.dib.H
}

// Flush copies the buffer rectangles to the destination DC.
func (b *BackBuffer) Flush(dst HDC, w, h int32) {
	if b.dib == nil {
		return
	}
	BLT(dst, 0, 0, w, h, b.dib.DC, 0, 0)
}

func (b *BackBuffer) Free() {
	if b.dib != nil {
		b.dib.Free()
		b.dib = nil
	}
}
