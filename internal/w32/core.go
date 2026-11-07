// Package w32 is a minimal, hand-written Win32 binding.
//
// Design rules (memory is the top priority):
//   - pure syscall, no cgo (CGO_ENABLED=0)
//   - no heavy system framework is pulled in: no GDI+, no WIC, no DirectShow,
//     no WebView2. Only user32 / gdi32 / comctl32 / msimg32 / kernel32.
//   - images are decoded with Go's stdlib (image/jpeg, image/png) and blitted
//     to a DIB section, so the process never loads extra image codecs.
package w32

import (
	"errors"
	"image"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")
	msimg32  = syscall.NewLazyDLL("msimg32.dll")
	winmm    = syscall.NewLazyDLL("winmm.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")
)

var (
	procDefWindowProcW  = user32.NewProc("DefWindowProcW")
	procRegisterClassEx = user32.NewProc("RegisterClassExW")
	procCreateWindowEx  = user32.NewProc("CreateWindowExW")
	procDestroyWindow   = user32.NewProc("DestroyWindow")
	procShowWindow      = user32.NewProc("ShowWindow")
	procUpdateWindow    = user32.NewProc("UpdateWindow")
	procGetMessageW     = user32.NewProc("GetMessageW")
	procTranslateMsg    = user32.NewProc("TranslateMessage")
	procDispatchMsg     = user32.NewProc("DispatchMessageW")
	procPostMessageW    = user32.NewProc("PostMessageW")
	procSendMessageW    = user32.NewProc("SendMessageW")
	procPostQuitMessage = user32.NewProc("PostQuitMessage")
	procSetWindowTextW  = user32.NewProc("SetWindowTextW")
	procSetWindowPos    = user32.NewProc("SetWindowPos")
	procMoveWindow      = user32.NewProc("MoveWindow")
	procGetClientRect   = user32.NewProc("GetClientRect")
	procGetWindowRect   = user32.NewProc("GetWindowRect")
	procInvalidateRect  = user32.NewProc("InvalidateRect")
	procBeginPaint      = user32.NewProc("BeginPaint")
	procEndPaint        = user32.NewProc("EndPaint")
	procGetDC           = user32.NewProc("GetDC")
	procReleaseDC       = user32.NewProc("ReleaseDC")
	procLoadCursorW     = user32.NewProc("LoadCursorW")
	procSetCursor       = user32.NewProc("SetCursor")
	procSetCapture      = user32.NewProc("SetCapture")
	procReleaseCapture  = user32.NewProc("ReleaseCapture")
	procGetCursorPos    = user32.NewProc("GetCursorPos")
	procScreenToClient  = user32.NewProc("ScreenToClient")
	procClientToScreen  = user32.NewProc("ClientToScreen")
	procSetForeground   = user32.NewProc("SetForegroundWindow")
	procMessageBoxW     = user32.NewProc("MessageBoxW")
	procSetTimer        = user32.NewProc("SetTimer")
	procKillTimer       = user32.NewProc("KillTimer")
	procDrawTextW       = user32.NewProc("DrawTextW")
	procFillRect        = user32.NewProc("FillRect")
	procEnableWindow    = user32.NewProc("EnableWindow")
	procSetFocus        = user32.NewProc("SetFocus")
	procGetForeground   = user32.NewProc("GetForegroundWindow")
	procSetProcessDPIAw = user32.NewProc("SetProcessDPIAware")
	procSetWindowLongPtr = user32.NewProc("SetWindowLongPtrW")
	procGetWindowLongPtr = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLong    = user32.NewProc("SetWindowLongW")
	procGetWindowLong    = user32.NewProc("GetWindowLongW")
	procGetAsyncKey     = user32.NewProc("GetAsyncKeyState")
	procScrollWindowEx  = user32.NewProc("ScrollWindowEx")
	procSetScrollInfo   = user32.NewProc("SetScrollInfo")
	procShowScrollBar   = user32.NewProc("ShowScrollBar")
	procIsWindowVisible = user32.NewProc("IsWindowVisible")
	procSetClassLongPtr = user32.NewProc("SetClassLongPtrW")
	procSetProp         = user32.NewProc("SetPropW")
	procGetProp         = user32.NewProc("GetPropW")
	procTrackMouseEvent = user32.NewProc("TrackMouseEvent")

	procGetModuleHandle = kernel32.NewProc("GetModuleHandleW")
	procGetTickCount64  = kernel32.NewProc("GetTickCount64")
	procSleep           = kernel32.NewProc("Sleep")
	procGetLastError    = kernel32.NewProc("GetLastError")

	procCreateFontW     = gdi32.NewProc("CreateFontW")
	procCreateSolidBr   = gdi32.NewProc("CreateSolidBrush")
	procCreatePen       = gdi32.NewProc("CreatePen")
	procSelectObject    = gdi32.NewProc("SelectObject")
	procDeleteObject    = gdi32.NewProc("DeleteObject")
	procDeleteDC        = gdi32.NewProc("DeleteDC")
	procSetBkMode       = gdi32.NewProc("SetBkMode")
	procSetTextColor    = gdi32.NewProc("SetTextColor")
	procSetBkColor      = gdi32.NewProc("SetBkColor")
	procTextOutW        = gdi32.NewProc("TextOutW")
	procGetTextExtent   = gdi32.NewProc("GetTextExtentPoint32W")
	procCreateCompatDC  = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatBmp = gdi32.NewProc("CreateCompatibleBitmap")
	procCreateDIBSec    = gdi32.NewProc("CreateDIBSection")
	procBitBlt          = gdi32.NewProc("BitBlt")
	procStretchBlt      = gdi32.NewProc("StretchBlt")
	procSetStretchMode  = gdi32.NewProc("SetStretchBltMode")
	procRect            = gdi32.NewProc("Rectangle")
	procRoundRect       = gdi32.NewProc("RoundRect")
	procEllipse         = gdi32.NewProc("Ellipse")
	procMoveToEx        = gdi32.NewProc("MoveToEx")
	procLineTo          = gdi32.NewProc("LineTo")
	procGetStockObject  = gdi32.NewProc("GetStockObject")
	procCreatePenEx     = gdi32.NewProc("ExtCreatePen")
	procSetPixel        = gdi32.NewProc("SetPixel")
	procGdiFlush        = gdi32.NewProc("GdiFlush")
	procPolygon         = gdi32.NewProc("Polygon")
	procSaveDC          = gdi32.NewProc("SaveDC")
	procRestoreDC       = gdi32.NewProc("RestoreDC")
	procIntersectClip   = gdi32.NewProc("IntersectClipRect")

	procGradientFill = msimg32.NewProc("GradientFill")
	procAlphaBlend   = msimg32.NewProc("AlphaBlend")
	procTransparentB = msimg32.NewProc("TransparentBlt")

	procInitCommonCtrls = comctl32.NewProc("InitCommonControlsEx")

	procCreateStreamOnHGlobal = ole32.NewProc("CreateStreamOnHGlobal")
)

// ---------------------------------------------------------------------------
// basic types
// ---------------------------------------------------------------------------

type POINT struct{ X, Y int32 }
type SIZE struct{ CX, CY int32 }
type RECT struct{ Left, Top, Right, Bottom int32 }

func (r RECT) Dx() int32 { return r.Right - r.Left }
func (r RECT) Dy() int32 { return r.Bottom - r.Top }


type PAINTSTRUCT struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     RECT
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

type WNDCLASSEX struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type NMHDR struct {
	HwndFrom uintptr
	IDFrom   uintptr
	Code     uint32
	_        uint32
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func u16ptr(s string) (*uint16, error) { return syscall.UTF16PtrFromString(s) }

// UTF16 converts a Go string into a NUL-terminated UTF-16 buffer.
func UTF16(s string) []uint16 {
	b, _ := syscall.UTF16FromString(s)
	return b
}

func Ptr[T any](v *T) uintptr { return uintptr(unsafe.Pointer(v)) }

func ModHandle() uintptr {
	r, _, _ := procGetModuleHandle.Call(0)
	return r
}

func TickCount() uint64 {
	r, _, _ := procGetTickCount64.Call()
	return uint64(r)
}

func Sleep(ms uint32) { procSleep.Call(uintptr(ms)) }

func LastError() error {
	r, _, _ := procGetLastError.Call()
	if r == 0 {
		return nil
	}
	return syscall.Errno(r)
}

func DPI() int {
	hdc, _, _ := procGetDC.Call(0)
	if hdc == 0 {
		return 96
	}
	defer procReleaseDC.Call(0, hdc)
	// LOGPIXELSX = 88
	r, _, _ := procGetDeviceCaps.Call(hdc, 88)
	if r == 0 {
		return 96
	}
	return int(r)
}

var procGetDeviceCaps = gdi32.NewProc("GetDeviceCaps")

func SetProcessDPIAware() { procSetProcessDPIAw.Call() }

// ---------------------------------------------------------------------------
// GDI objects
// ---------------------------------------------------------------------------

type HDC uintptr
type HBITMAP uintptr
type HBRUSH uintptr
type HPEN uintptr
type HFONT uintptr
type HGDIOBJ uintptr

const (
	TRANSPARENT = 1

	PS_SOLID     = 0
	PS_NULL      = 5
	NULL_PEN     = 8
	NULL_BRUSH   = 5
	DC_BRUSH     = 18
	DC_PEN       = 19
	STOCK_SYSTEM_FONT = 13
)

// COLORREF is 0x00BBGGRR.
func RGB(r, g, b uint8) uint32 { return uint32(r) | uint32(g)<<8 | uint32(b)<<16 }

func CreateSolidBrush(c uint32) HBRUSH {
	r, _, _ := procCreateSolidBr.Call(uintptr(c))
	return HBRUSH(r)
}

func CreatePen(style, width int32, c uint32) HPEN {
	r, _, _ := procCreatePen.Call(uintptr(style), uintptr(width), uintptr(c))
	return HPEN(r)
}

func SelectObject(dc HDC, obj HGDIOBJ) HGDIOBJ {
	r, _, _ := procSelectObject.Call(uintptr(dc), uintptr(obj))
	return HGDIOBJ(r)
}

// SelectObject2 is an alias kept for readability at painting call sites.
func SelectObject2(dc HDC, obj HGDIOBJ) (HGDIOBJ, uintptr, error) {
	return SelectObject(dc, obj), 0, nil
}

func DeleteObject(obj HGDIOBJ) {
	if obj != 0 {
		procDeleteObject.Call(uintptr(obj))
	}
}

func GetStockObject(id int32) HGDIOBJ {
	r, _, _ := procGetStockObject.Call(uintptr(id))
	return HGDIOBJ(r)
}

func SetBkMode(dc HDC, mode int32) { procSetBkMode.Call(uintptr(dc), uintptr(mode)) }

func SetTextColor(dc HDC, c uint32) { procSetTextColor.Call(uintptr(dc), uintptr(c)) }

func SetBkColor(dc HDC, c uint32) { procSetBkColor.Call(uintptr(dc), uintptr(c)) }

func FillRect(dc HDC, rc *RECT, br HBRUSH) {
	procFillRect.Call(uintptr(dc), uintptr(unsafe.Pointer(rc)), uintptr(br))
}

func MoveTo(dc HDC, x, y int32) { procMoveToEx.Call(uintptr(dc), uintptr(x), uintptr(y), 0) }

func LineTo(dc HDC, x, y int32) { procLineTo.Call(uintptr(dc), uintptr(x), uintptr(y)) }

func Rectangle(dc HDC, l, t, r, b int32) {
	procRect.Call(uintptr(dc), uintptr(l), uintptr(t), uintptr(r), uintptr(b))
}

func RoundRect(dc HDC, l, t, r, b, ew, eh int32) {
	procRoundRect.Call(uintptr(dc), uintptr(l), uintptr(t), uintptr(r), uintptr(b), uintptr(ew), uintptr(eh))
}

func Ellipse(dc HDC, l, t, r, b int32) {
	procEllipse.Call(uintptr(dc), uintptr(l), uintptr(t), uintptr(r), uintptr(b))
}

// Polygon fills a polygon using the DC's current brush and outlines with pen.
func Polygon(dc HDC, pts []POINT) {
	if len(pts) < 3 {
		return
	}
	procPolygon.Call(uintptr(dc), uintptr(unsafe.Pointer(&pts[0])), uintptr(len(pts)))
}

// Clip rect helpers keep list rows from spilling outside their viewport.
func SaveDC(dc HDC) int32 {
	r, _, _ := procSaveDC.Call(uintptr(dc))
	return int32(r)
}

func RestoreDC(dc HDC, level int32) {
	procRestoreDC.Call(uintptr(dc), uintptr(level))
}

func IntersectClipRect(dc HDC, l, t, r, b int32) {
	procIntersectClip.Call(uintptr(dc), uintptr(l), uintptr(t), uintptr(r), uintptr(b))
}

func TextOut(dc HDC, x, y int32, s string) {
	if s == "" {
		return
	}
	u := UTF16(s)
	procTextOutW.Call(uintptr(dc), uintptr(x), uintptr(y), uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1))
}

func TextExtent(dc HDC, s string) (int32, int32) {
	if s == "" {
		return 0, 0
	}
	u := UTF16(s)
	var sz SIZE
	procGetTextExtent.Call(uintptr(dc), uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&sz)))
	return sz.CX, sz.CY
}

const (
	DT_LEFT       = 0x00000000
	DT_CENTER     = 0x00000001
	DT_RIGHT      = 0x00000002
	DT_VCENTER    = 0x00000004
	DT_SINGLELINE = 0x00000020
	DT_NOPREFIX   = 0x00000800
	DT_END_ELLIPSIS = 0x00008000
	DT_WORDBREAK  = 0x00000010
	DT_CALCRECT   = 0x00000400
	DT_TOP        = 0x00000000
)

func DrawText(dc HDC, s string, rc RECT, flags uint32) int32 {
	if s == "" {
		return 0
	}
	u := UTF16(s)
	r, _, _ := procDrawTextW.Call(uintptr(dc), uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1),
		uintptr(unsafe.Pointer(&rc)), uintptr(flags))
	return int32(r)
}

// MeasureText returns the height needed to wrap s inside width w.
func MeasureText(dc HDC, s string, w int32, flags uint32) (int32, int32) {
	u := UTF16(s)
	rc := RECT{0, 0, w, 0}
	r, _, _ := procDrawTextW.Call(uintptr(dc), uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1),
		uintptr(unsafe.Pointer(&rc)), uintptr(flags|DT_CALCRECT))
	return int32(r), rc.Bottom - rc.Top
}

// CreateFont builds a GDI font. weight 400=normal, 700=bold.
func CreateFont(size int32, weight int32, face string, italic bool) HFONT {
	fp, err := syscall.UTF16PtrFromString(face)
	if err != nil {
		fp = nil
	}
	it := uintptr(0)
	if italic {
		it = 1
	}
	r, _, _ := procCreateFontW.Call(
		uintptr(-size), 0, 0, 0,
		uintptr(weight),
		it, 0, 0,
		134, // GB2312_CHARSET
		0, 0, 5 /*CLEARTYPE_QUALITY*/, 0,
		uintptr(unsafe.Pointer(fp)),
	)
	return HFONT(r)
}

type TRIVERTEX struct {
	X, Y   int32
	Red    uint16
	Green  uint16
	Blue   uint16
	Alpha  uint16
}

type GRADIENT_RECT struct{ UpperLeft, LowerRight uint32 }

// GradientV paints a vertical linear gradient (top color c1 -> bottom color c2).
func GradientV(dc HDC, rc RECT, c1, c2 uint32) {
	tv := [2]TRIVERTEX{
		{X: rc.Left, Y: rc.Top, Red: uint16(c1&0xff) << 8, Green: uint16((c1>>8)&0xff) << 8, Blue: uint16((c1>>16)&0xff) << 8, Alpha: 0xff00},
		{X: rc.Right, Y: rc.Bottom, Red: uint16(c2&0xff) << 8, Green: uint16((c2>>8)&0xff) << 8, Blue: uint16((c2>>16)&0xff) << 8, Alpha: 0xff00},
	}
	gr := [1]GRADIENT_RECT{{0, 1}}
	procGradientFill.Call(uintptr(dc), uintptr(unsafe.Pointer(&tv[0])), 2,
		uintptr(unsafe.Pointer(&gr[0])), 1, 0 /*GRADIENT_FILL_RECT_V*/)
}

func BLT(dst HDC, dx, dy, w, h int32, src HDC, sx, sy int32) {
	procBitBlt.Call(uintptr(dst), uintptr(dx), uintptr(dy), uintptr(w), uintptr(h),
		uintptr(src), uintptr(sx), uintptr(sy), 0x00CC0020 /*SRCCOPY*/)
}

// ---------------------------------------------------------------------------
// DIB section: an owned bitmap we can both draw with and write pixels into.
// ---------------------------------------------------------------------------

type DIB struct {
	DC     HDC
	Bmp    HBITMAP
	Old    HGDIOBJ
	W, H   int32
	Bits   unsafe.Pointer
	Stride int32
}

type BITMAPINFOHEADER struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

type BITMAPINFO struct {
	Header BITMAPINFOHEADER
	Colors [1]uint32
}

// NewDIB creates a 32bpp top-down DIB section and selects it into a fresh
// memory DC. Remember to Free() it.
func NewDIB(w, h int32) *DIB {
	if w <= 0 || h <= 0 {
		return nil
	}
	bi := BITMAPINFO{Header: BITMAPINFOHEADER{
		BiSize:        uint32(unsafe.Sizeof(BITMAPINFOHEADER{})),
		BiWidth:       w,
		BiHeight:      -h, // top-down
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: 0, // BI_RGB
	}}
	var bits unsafe.Pointer
	dc, _, _ := procCreateCompatDC.Call(0)
	hbm, _, _ := procCreateDIBSec.Call(uintptr(dc), uintptr(unsafe.Pointer(&bi)), 0,
		uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hbm == 0 || dc == 0 {
		return nil
	}
	old, _, _ := procSelectObject.Call(dc, hbm)
	return &DIB{DC: HDC(dc), Bmp: HBITMAP(hbm), Old: HGDIOBJ(old), W: w, H: h, Bits: bits, Stride: w * 4}
}

func (d *DIB) Free() {
	if d == nil {
		return
	}
	procSelectObject.Call(uintptr(d.DC), uintptr(d.Old))
	DeleteObject(HGDIOBJ(d.Bmp))
	procDeleteDC.Call(uintptr(d.DC))
	d.Bits = nil
}

// PixelPtr returns a 0-copy view of the DIB memory as BGRA bytes.
func (d *DIB) PixelPtr() []byte {
	if d == nil || d.Bits == nil {
		return nil
	}
	return unsafe.Slice((*byte)(d.Bits), int(d.Stride)*int(d.H))
}

// ---------------------------------------------------------------------------
// image -> DIB conversion (stdlib decoders only)
// ---------------------------------------------------------------------------

// ImageToDIB converts any image.Image into a 32bpp DIB of exactly w x h using
// area-average downscaling. Alpha is written, but GDI blits ignore it; callers
// that need alpha compositing should use their own path.
func ImageToDIB(src image.Image, w, h int32) *DIB {
	dib := NewDIB(w, h)
	if dib == nil {
		return nil
	}
	px := dib.PixelPtr()
	if px == nil {
		return dib
	}
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= 0 || sh <= 0 {
		return dib
	}
	switch si := src.(type) {
	case *image.RGBA:
		for y := int32(0); y < h; y++ {
			sy := b.Min.Y + int(y)*sh/int(h)
			for x := int32(0); x < w; x++ {
				sx := b.Min.X + int(x)*sw/int(w)
				o := int(y)*int(dib.Stride) + int(x)*4
				i := si.PixOffset(sx, sy)
				px[o+0] = si.Pix[i+2]
				px[o+1] = si.Pix[i+1]
				px[o+2] = si.Pix[i+0]
				px[o+3] = 0xff
			}
		}
	case *image.NRGBA:
		for y := int32(0); y < h; y++ {
			sy := b.Min.Y + int(y)*sh/int(h)
			for x := int32(0); x < w; x++ {
				sx := b.Min.X + int(x)*sw/int(w)
				o := int(y)*int(dib.Stride) + int(x)*4
				i := si.PixOffset(sx, sy)
				px[o+0] = si.Pix[i+2]
				px[o+1] = si.Pix[i+1]
				px[o+2] = si.Pix[i+0]
				px[o+3] = 0xff
			}
		}
	case *image.Gray:
		for y := int32(0); y < h; y++ {
			sy := b.Min.Y + int(y)*sh/int(h)
			for x := int32(0); x < w; x++ {
				sx := b.Min.X + int(x)*sw/int(w)
				o := int(y)*int(dib.Stride) + int(x)*4
				v := si.Pix[si.PixOffset(sx, sy)]
				px[o+0], px[o+1], px[o+2], px[o+3] = v, v, v, 0xff
			}
		}
	default:
		// Generic path: coarse sampling through At(). Only used for paletted
		// sources such as the QR matrix, which are 1:1 anyway.
		for y := int32(0); y < h; y++ {
			sy := b.Min.Y + int(y)*sh/int(h)
			for x := int32(0); x < w; x++ {
				sx := b.Min.X + int(x)*sw/int(w)
				r, g, bl, _ := src.At(sx, sy).RGBA()
				o := int(y)*int(dib.Stride) + int(x)*4
				px[o+0] = byte(bl >> 8)
				px[o+1] = byte(g >> 8)
				px[o+2] = byte(r >> 8)
				px[o+3] = 0xff
			}
		}
	}
	return dib
}

// BitmapFromDIB detaches a standalone HBITMAP from a DIB.
//
// NOTE: the compatible bitmap must be created from a *screen* DC. Creating it
// from a CreateCompatibleDC(0) memory DC yields a 1bpp monochrome bitmap, which
// silently destroys colour images (the original bug that made album art flat).
func BitmapFromDIB(d *DIB) HBITMAP {
	if d == nil {
		return 0
	}
	sdc, _, _ := procGetDC.Call(0)
	if sdc == 0 {
		return 0
	}
	defer procReleaseDC.Call(0, sdc)
	dc, _, _ := procCreateCompatDC.Call(sdc)
	if dc == 0 {
		return 0
	}
	defer procDeleteDC.Call(dc)
	hbm, _, _ := procCreateCompatBmp.Call(sdc, uintptr(d.W), uintptr(d.H))
	if hbm == 0 {
		return 0
	}
	old, _, _ := procSelectObject.Call(dc, hbm)
	BLT(HDC(dc), 0, 0, d.W, d.H, d.DC, 0, 0)
	procSelectObject.Call(dc, old)
	return HBITMAP(hbm)
}

// Blit copies the whole DIB into dst at the given rectangle.
func (d *DIB) Blit(dst HDC, x, y int32) {
	if d == nil || d.DC == 0 {
		return
	}
	BLT(dst, x, y, d.W, d.H, d.DC, 0, 0)
}

func DeleteBitmap(b HBITMAP) { DeleteObject(HGDIOBJ(b)) }

// CreateCompatDCFor makes a memory DC holding bmp, for BitBlt from a stored
// bitmap without keeping a DC alive per image.
func CreateCompatDCFor(w, h int32, bmp HBITMAP) (HDC, int32, int32) {
	dc, _, _ := procCreateCompatDC.Call(0)
	if dc == 0 {
		return 0, 0, 0
	}
	procSelectObject.Call(uintptr(dc), uintptr(bmp))
	return HDC(dc), w, h
}

func DeleteCompatDC(dc HDC) { procDeleteDC.Call(uintptr(dc)) }

// ---------------------------------------------------------------------------
// Common controls
// ---------------------------------------------------------------------------

const (
	ICC_LISTVIEW_CLASSES = 0x00000001
	ICC_STANDARD_CLASSES = 0x00004000
)

type INITCOMMONCONTROLSEX struct {
	DwSize uint32
	DwICC  uint32
}

func InitCommonControls() {
	icc := INITCOMMONCONTROLSEX{DwSize: uint32(unsafe.Sizeof(INITCOMMONCONTROLSEX{})), DwICC: ICC_LISTVIEW_CLASSES | ICC_STANDARD_CLASSES}
	procInitCommonCtrls.Call(uintptr(unsafe.Pointer(&icc)))
}

var ErrFailed = errors.New("w32: call failed")

// TrackMouseLeave subscribes the window to a single WM_MOUSELEAVE event.
type TRACKMOUSEEVENT struct {
	CbSize      uint32
	DwFlags     uint32
	HwndTrack   uintptr
	DwHoverTime uint32
}

const TME_LEAVE = 0x00000002

func TrackMouseLeave(hwnd uintptr) {
	tme := TRACKMOUSEEVENT{
		CbSize:    uint32(unsafe.Sizeof(TRACKMOUSEEVENT{})),
		DwFlags:   TME_LEAVE,
		HwndTrack: hwnd,
	}
	procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
}
