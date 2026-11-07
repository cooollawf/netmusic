package w32

import (
	"syscall"
	"unsafe"
)

var (
	psapi            = syscall.NewLazyDLL("psapi.dll")
	procGetProcessMem = psapi.NewProc("GetProcessMemoryInfo")
	procGetCurrentProc = kernel32.NewProc("GetCurrentProcess")
)

// pmcEx mirrors PROCESS_MEMORY_COUNTERS_EX.
type pmcEx struct {
	Cb                uint32
	PageFaultCount    uint32
	PeakWorkingSet    uintptr
	WorkingSet        uintptr
	QuotaPeakPaged    uintptr
	QuotaPaged        uintptr
	QuotaPeakNonPaged uintptr
	QuotaNonPaged     uintptr
	Pagefile          uintptr
	PeakPagefile      uintptr
	PrivateUsage      uintptr
}

// Memory returns working set and private bytes of this process, in bytes.
func Memory() (ws, private uint64) {
	h, _, _ := procGetCurrentProc.Call()
	var m pmcEx
	m.Cb = uint32(unsafe.Sizeof(m))
	r, _, _ := procGetProcessMem.Call(h, uintptr(unsafe.Pointer(&m)), uintptr(m.Cb))
	if r == 0 {
		return 0, 0
	}
	return uint64(m.WorkingSet), uint64(m.PrivateUsage)
}

// CurrentProcess returns the pseudo handle for this process.
func CurrentProcess() uintptr {
	r, _, _ := procGetCurrentProc.Call()
	return r
}

// GuiResources reads the GDI/USER counters for a process handle.
func GuiResources(h uintptr) (gdi, user uint32) {
	r, _, _ := procGetGuiRes.Call(h, 0)
	r2, _, _ := procGetGuiRes.Call(h, 1)
	return uint32(r), uint32(r2)
}

var procGetGuiRes = user32.NewProc("GetGuiResources")
