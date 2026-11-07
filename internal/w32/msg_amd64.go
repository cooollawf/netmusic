//go:build amd64 || arm64

package w32

// MSG is the message structure. On 64-bit Windows the UINT message field is
// followed by 4 bytes of padding so that WPARAM lands on an 8-byte boundary.
type MSG struct {
	Hwnd    uintptr
	Message uint32
	_       uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
	_       uint32
}
