package w32

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	shell32              = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteW    = shell32.NewProc("ShellExecuteW")
)

// ShellOpen opens a file or folder with its default handler.
//
// ShellExecuteW is used instead of spawning explorer.exe so opening the download
// folder costs zero extra processes on a 2 GB machine.
func ShellOpen(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	r, _, _ := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(target)),
		0, 0,
		1, // SW_SHOWNORMAL
	)
	if r <= 32 {
		return ErrFailed
	}
	return nil
}
