//go:build windows

// Package config — per-user secret protection via DPAPI.
//
// Why not environment variables: a user environment variable is written to
// HKCU\Environment in plain text, is readable by every process of that user
// (including anything that scrapes GetEnvironmentStrings), survives in shell
// history and is not tied to a logon session. DPAPI encrypts the blob with a
// key derived from the user's logon credentials, so the ciphertext is useless
// if the file is copied to another account or another machine.
package config

import (
	"errors"
	"syscall"
	"unsafe"
)

var (
	crypt32  = syscall.NewLazyDLL("crypt32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procCryptProtectData   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	procLocalFree          = kernel32.NewProc("LocalFree")
)

type dataBlob struct {
	CbData uint32
	PbData *byte
}

const cryptProtectUIForbidden = 0x1

func newBlob(b []byte) (dataBlob, []byte) {
	if len(b) == 0 {
		return dataBlob{}, nil
	}
	// Copy so the caller's slice cannot move under the syscall.
	hold := make([]byte, len(b))
	copy(hold, b)
	return dataBlob{CbData: uint32(len(hold)), PbData: &hold[0]}, hold
}

// protect encrypts plain with the current user's DPAPI key.
func protect(plain []byte) ([]byte, error) {
	in, _ := newBlob(plain)
	var out dataBlob
	r, _, err := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0, // description
		0, 0, 0,
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, err
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.PbData)))
	buf := make([]byte, out.CbData)
	copy(buf, unsafe.Slice(out.PbData, out.CbData))
	return buf, nil
}

// unprotect reverses protect. It fails when the blob was produced by another
// user account or on another machine, which is the point.
func unprotect(cipher []byte) ([]byte, error) {
	if len(cipher) == 0 {
		return nil, errors.New("config: empty secret")
	}
	in, _ := newBlob(cipher)
	var out dataBlob
	r, _, err := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0, 0, 0, 0,
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, err
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.PbData)))
	buf := make([]byte, out.CbData)
	copy(buf, unsafe.Slice(out.PbData, out.CbData))
	return buf, nil
}
