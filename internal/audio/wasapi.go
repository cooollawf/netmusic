// Package audio plays PCM through WASAPI (shared mode) and decodes MP3 with a
// pure Go decoder. No cgo, no Media Foundation, no FFmpeg: the entire audio
// path costs one 100 ms device buffer plus the compressed song itself.
package audio

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	ole32   = syscall.NewLazyDLL("ole32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
	procSleep            = kernel32.NewProc("Sleep")
)

const (
	coinitApartmentThreaded = 0x2
	clsctxAll               = 0x17

	audclntShareModeShared = 0x00000000
	audclntStreamFlagsEventCallback = 0x00040000
	audclntStreamFlagsAutoConvertPCM = 0x80000000
	audclntStreamFlagsSRCDefaultQuality = 0x08000000

	eRender  = 0
	eConsole = 0

	waveFormatPCM = 1
)

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

func makeGUID(d1 uint32, d2, d3 uint16, b []byte) guid {
	var g guid
	g.Data1 = d1
	g.Data2 = d2
	g.Data3 = d3
	copy(g.Data4[:], b)
	return g
}

var (
	clsidMMDeviceEnumerator = makeGUID(0xBCDE0395, 0xE52F, 0x467C, []byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E})
	iidIMMDeviceEnumerator = makeGUID(0xA95664D2, 0x9614, 0x4F35, []byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6})
	iidIAudioClient        = makeGUID(0x1CB9AD4C, 0xDBFA, 0x4C32, []byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2})
	iidIAudioRenderClient  = makeGUID(0xF294ACFC, 0x3146, 0x4483, []byte{0xA7, 0xBF, 0xAD, 0xDC, 0xA7, 0xC2, 0x60, 0xE2})
)

type waveFormatEx struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	CbSize         uint16
}

// comCall invokes vtable method idx on obj.
func comCall(obj unsafe.Pointer, idx int, args ...uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, uintptr(idx)*unsafe.Sizeof(uintptr(0))))
	full := make([]uintptr, 0, len(args)+1)
	full = append(full, uintptr(obj))
	full = append(full, args...)
	r, _, _ := syscall.SyscallN(fn, full...)
	return r
}

func hresult(r uintptr) error {
	if int32(r) < 0 {
		return fmt.Errorf("wasapi: hresult 0x%08x", uint32(r))
	}
	return nil
}

func comRelease(obj unsafe.Pointer) {
	if obj != nil {
		comCall(obj, 2) // IUnknown::Release
	}
}

// AudioOut is a shared-mode WASAPI render stream.
type AudioOut struct {
	client unsafe.Pointer // IAudioClient
	render unsafe.Pointer // IAudioRenderClient
	enum   unsafe.Pointer // IMMDeviceEnumerator
	device unsafe.Pointer // IMMDevice

	BufferFrames int
	SampleRate   int
	Channels     int
	BytesPerFrame int

	bufPtr unsafe.Pointer
	started bool
	coInit  bool
}

// OpenStream initialises a shared-mode stream expecting interleaved 16-bit PCM
// at the given rate. Windows converts (AUTOCONVERTPCM) to the device mix
// format, so no resampling code is needed on our side.
func OpenStream(sampleRate, channels int) (*AudioOut, error) {
	runtime.LockOSThread()
	// CoInitializeEx is idempotent per thread; S_FALSE also means success.
	hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded)
	if int32(hr) < 0 && int32(hr) != 1 {
		return nil, hresult(hr)
	}

	a := &AudioOut{SampleRate: sampleRate, Channels: channels, coInit: true}
	if err := a.init(sampleRate, channels); err != nil {
		a.Close()
		return nil, err
	}
	return a, nil
}

func (a *AudioOut) init(sampleRate, channels int) error {
	var enum unsafe.Pointer
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)),
		0, clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)),
		uintptr(unsafe.Pointer(&enum)),
	)
	if err := hresult(hr); err != nil {
		return fmt.Errorf("CoCreateInstance: %w", err)
	}
	a.enum = enum

	// IMMDeviceEnumerator::GetDefaultAudioEndpoint (vtable 4)
	var dev unsafe.Pointer
	r := comCall(enum, 4, uintptr(eRender), uintptr(eConsole), uintptr(unsafe.Pointer(&dev)))
	if err := hresult(r); err != nil {
		return fmt.Errorf("GetDefaultAudioEndpoint: %w", err)
	}
	a.device = dev

	// IMMDevice::Activate (vtable 3)
	var client unsafe.Pointer
	r = comCall(dev, 3, uintptr(unsafe.Pointer(&iidIAudioClient)), clsctxAll, 0, uintptr(unsafe.Pointer(&client)))
	if err := hresult(r); err != nil {
		return fmt.Errorf("Activate(IAudioClient): %w", err)
	}
	a.client = client

	wf := waveFormatEx{
		FormatTag:      waveFormatPCM,
		Channels:       uint16(channels),
		SamplesPerSec:  uint32(sampleRate),
		BitsPerSample:  16,
		BlockAlign:     uint16(channels * 2),
		AvgBytesPerSec: uint32(sampleRate * channels * 2),
	}
	a.BytesPerFrame = channels * 2

	// IAudioClient::Initialize (vtable 3): 100 ms shared buffer, let the audio
	// engine convert from our format to the endpoint format.
	const hnsBuffer = int64(1000000) // 100 ms in 100 ns units
	flags := uintptr(audclntStreamFlagsAutoConvertPCM | audclntStreamFlagsSRCDefaultQuality)
	r = comCall(client, 3,
		uintptr(audclntShareModeShared),
		flags,
		uintptr(hnsBuffer),
		0,
		uintptr(unsafe.Pointer(&wf)),
		0,
	)
	if err := hresult(r); err != nil {
		return fmt.Errorf("IAudioClient.Initialize: %w", err)
	}

	var frames uint32
	r = comCall(client, 4, uintptr(unsafe.Pointer(&frames))) // GetBufferSize
	if err := hresult(r); err != nil {
		return fmt.Errorf("GetBufferSize: %w", err)
	}
	a.BufferFrames = int(frames)

	var render unsafe.Pointer
	r = comCall(client, 14, uintptr(unsafe.Pointer(&iidIAudioRenderClient)), uintptr(unsafe.Pointer(&render))) // GetService
	if err := hresult(r); err != nil {
		return fmt.Errorf("GetService(IAudioRenderClient): %w", err)
	}
	a.render = render
	return nil
}

// Padding reports how many frames are still queued in the device buffer.
func (a *AudioOut) Padding() int {
	if a.client == nil {
		return 0
	}
	var p uint32
	comCall(a.client, 6, uintptr(unsafe.Pointer(&p))) // GetCurrentPadding
	return int(p)
}

// Acquire returns a writable pointer for up to frames frames.
func (a *AudioOut) Acquire(frames int) (unsafe.Pointer, error) {
	if a.render == nil || frames <= 0 {
		return nil, fmt.Errorf("audio: nothing to render")
	}
	var p unsafe.Pointer
	r := comCall(a.render, 3, uintptr(frames), uintptr(unsafe.Pointer(&p))) // GetBuffer
	if err := hresult(r); err != nil {
		return nil, err
	}
	a.bufPtr = p
	return p, nil
}

// Release publishes frames back to the device.
func (a *AudioOut) Release(frames int) {
	if a.render == nil || a.bufPtr == nil {
		return
	}
	comCall(a.render, 4, uintptr(frames), 0) // ReleaseBuffer
	a.bufPtr = nil
}

func (a *AudioOut) Start() {
	if a.client != nil && !a.started {
		comCall(a.client, 10) // Start
		a.started = true
	}
}

func (a *AudioOut) Stop() {
	if a.client != nil && a.started {
		comCall(a.client, 11) // Stop
		a.started = false
	}
}

// Reset drops queued frames.
func (a *AudioOut) Reset() {
	if a.client != nil {
		comCall(a.client, 12) // Reset
	}
}

// Close releases every COM object.
func (a *AudioOut) Close() {
	a.Stop()
	if a.render != nil {
		comRelease(a.render)
		a.render = nil
	}
	if a.client != nil {
		comRelease(a.client)
		a.client = nil
	}
	if a.device != nil {
		comRelease(a.device)
		a.device = nil
	}
	if a.enum != nil {
		comRelease(a.enum)
		a.enum = nil
	}
	if a.coInit {
		procCoUninitialize.Call()
		a.coInit = false
	}
}

// Slice returns a mutable byte view of a raw buffer of n bytes.
func Slice(p unsafe.Pointer, n int) []byte {
	return unsafe.Slice((*byte)(p), n)
}

func nanosleep(ms int) { procSleep.Call(uintptr(ms)) }
