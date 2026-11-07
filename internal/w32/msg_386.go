//go:build 386 || arm

package w32

// MSG is the message structure. On 32-bit Windows every field is 4 bytes wide,
// so there is no padding after the message id (the 64-bit definition with its
// explicit pad would shift WPARAM/LPARAM and break input handling).
type MSG struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
}
