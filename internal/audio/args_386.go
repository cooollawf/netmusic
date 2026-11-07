//go:build 386

package audio

// comArg64 splits a 64-bit COM/Win32 argument into the two 32-bit stack slots
// it occupies in the stdcall layout.
//
// REFERENCE_TIME is an int64. On 32-bit Windows it takes two dwords on the
// stack, so passing a single uintptr shifts every later argument by 4 bytes —
// which is exactly how IAudioClient::Initialize ended up reading pFormat from
// the wrong slot (E_POINTER / E_INVALIDARG depending on the path).
func comArg64(v uint64) []uintptr {
	return []uintptr{uintptr(uint32(v)), uintptr(uint32(v >> 32))}
}
