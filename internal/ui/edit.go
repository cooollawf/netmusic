package ui

import (
	"sync"
	"syscall"
	"unsafe"

	"netmusic/internal/w32"
)

// A single line EDIT control is the one place where we happily use a native
// control: it gives us IME support (Chinese input) and clipboard behaviour for
// free, which would be a lot of code to reproduce. Everything else is painted
// by hand.
//
// Two native features make the control fit the dark skin without hacks:
//   - EM_SETBKGNDCOLOR gives the control its own background brush
//   - EM_SETCUEBANNER renders the placeholder text for us
//
// The only missing piece is "press Enter to commit", so we subclass it.

var (
	editMu      sync.Mutex
	editEnter   = map[uintptr]func(){}  // hwnd -> callback
	editOldProc = map[uintptr]uintptr{} // hwnd -> original wndproc
	editProcCB  = syscall.NewCallback(editWndProc)
)

const (
	wmSetFont       = 0x0030
	emSetPassword   = 0x00CC
	emSetBkGndColor = 0x0443
	emSetCueBanner  = 0x1501
	vkReturn        = 0x0D
	vkEscape        = 0x1B

	esPassword = 0x0020
)

func editWndProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	editMu.Lock()
	cb := editEnter[hwnd]
	old := editOldProc[hwnd]
	editMu.Unlock()
	if old != 0 {
		if msg == w32.WM_KEYDOWN && (wp == vkReturn || wp == vkEscape) && cb != nil {
			cb()
			return 0
		}
		r, _, _ := syscall.SyscallN(old, hwnd, uintptr(msg), wp, lp)
		return r
	}
	return w32.DefWindowProc(hwnd, msg, wp, lp)
}

// NewEdit creates a dark, single line text field child of parent.
func NewEdit(parent uintptr, id int, x, y, w, h int32, placeholder string) uintptr {
	return NewEditStyled(parent, id, x, y, w, h, 0, placeholder)
}

// NewPasswordEdit is NewEdit with ES_PASSWORD.
func NewPasswordEdit(parent uintptr, id int, x, y, w, h int32, placeholder string) uintptr {
	return NewEditStyled(parent, id, x, y, w, h, esPassword, placeholder)
}

// NewEditStyled creates an edit control with extra style bits.
func NewEditStyled(parent uintptr, id int, x, y, w, h int32, style uint32, placeholder string) uintptr {
	hwnd := w32.CreateChild(parent, "EDIT", "", w32.ES_AUTOHSCROLL|w32.ES_LEFT|style, 0, id, x, y, w, h)
	if hwnd == 0 {
		return 0
	}
	w32.SetWindowFont(hwnd, uintptr(T.FontS))
	w32.SendMessage(hwnd, emSetBkGndColor, 0, uintptr(T.PanelAlt))
	if style&esPassword != 0 {
		w32.SendMessage(hwnd, emSetPassword, uintptr('*'), 0)
	}
	if placeholder != "" {
		if p, err := syscall.UTF16PtrFromString(placeholder); err == nil {
			w32.SendMessage(hwnd, emSetCueBanner, 0, uintptr(unsafe.Pointer(p)))
		}
	}
	old := w32.SetWindowLongPtr(hwnd, w32.GWLP_WNDPROC, editProcCB)
	editMu.Lock()
	editOldProc[hwnd] = old
	editMu.Unlock()
	return hwnd
}

// SetEditEnter installs the Enter (and Escape) handler for an edit control.
func SetEditEnter(hwnd uintptr, fn func()) {
	editMu.Lock()
	editEnter[hwnd] = fn
	editMu.Unlock()
}

// EditText returns the control's text.
func EditText(hwnd uintptr) string { return w32.GetWindowText(hwnd) }

// ClearEdit empties the control.
func ClearEdit(hwnd uintptr) { w32.SetTitle(hwnd, "") }
