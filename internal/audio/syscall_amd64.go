//go:build amd64 || arm64 || arm

package audio

import "syscall"

// syscallV invokes a function pointer with an explicit argument list. The
// 64-bit Windows ABI passes the first four arguments in registers and the rest
// on the stack, and SyscallN handles any count, so no count bookkeeping is
// needed here.
func syscallV(trap uintptr, args []uintptr) uintptr {
	r, _, _ := syscall.SyscallN(trap, args...)
	return r
}
