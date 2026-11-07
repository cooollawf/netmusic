//go:build windows

package netx

import (
	"syscall"
	"unsafe"
)

var (
	advapi32            = syscall.NewLazyDLL("advapi32.dll")
	procRegGetValueW    = advapi32.NewProc("RegGetValueW")
)

const (
	hkeyCurrentUser  = 0x80000001
	rrfRTREGSZ       = 0x00000002
	rrfRTREGDWORD    = 0x00000010
	errMoreData      = 234
	inetSettingsPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
)

// winINETProxy returns the ProxyServer string when ProxyEnable is 1.
func winINETProxy() string {
	sub, err := syscall.UTF16PtrFromString(inetSettingsPath)
	if err != nil {
		return ""
	}
	name, err := syscall.UTF16PtrFromString("ProxyEnable")
	if err != nil {
		return ""
	}
	var enable uint32
	size := uint32(4)
	r, _, _ := procRegGetValueW.Call(
		hkeyCurrentUser,
		uintptr(unsafe.Pointer(sub)),
		uintptr(unsafe.Pointer(name)),
		rrfRTREGDWORD,
		0,
		uintptr(unsafe.Pointer(&enable)),
		uintptr(unsafe.Pointer(&size)),
	)
	if r != 0 || enable == 0 {
		return ""
	}
	name2, err := syscall.UTF16PtrFromString("ProxyServer")
	if err != nil {
		return ""
	}
	buf := make([]uint16, 512)
	size = uint32(len(buf) * 2)
	r, _, _ = procRegGetValueW.Call(
		hkeyCurrentUser,
		uintptr(unsafe.Pointer(sub)),
		uintptr(unsafe.Pointer(name2)),
		rrfRTREGSZ,
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if r != 0 {
		return ""
	}
	out := make([]rune, 0, 64)
	for _, c := range buf {
		if c == 0 {
			break
		}
		out = append(out, rune(c))
	}
	return string(out)
}
