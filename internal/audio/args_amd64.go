//go:build amd64 || arm64 || arm

package audio

// comArg64 passes a 64-bit argument as a single word: on 64-bit Windows every
// argument already occupies one 8-byte slot.
func comArg64(v uint64) []uintptr {
	return []uintptr{uintptr(v)}
}
