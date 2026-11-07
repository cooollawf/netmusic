//go:build 386

package audio

import "syscall"

// syscallV invokes a function pointer with an explicit argument list.
//
// On 32-bit Windows every Win32/COM signature is stdcall, and the number of
// arguments has to be communicated to the runtime so it can fix up the stack.
// Syscall15 carries that count explicitly, which makes it the safest primitive
// for COM vtable calls on 386.
func syscallV(trap uintptr, args []uintptr) uintptr {
	var a [15]uintptr
	copy(a[:], args)
	n := uintptr(len(args))
	r, _, _ := syscall.Syscall15(trap, n,
		a[0], a[1], a[2], a[3], a[4], a[5], a[6], a[7],
		a[8], a[9], a[10], a[11], a[12], a[13], a[14])
	return r
}
