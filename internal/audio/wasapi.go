// Package audio plays PCM through WASAPI (shared mode) and decodes MP3 with a
// pure Go decoder. No cgo, no Media Foundation, no FFmpeg: the entire audio
// path costs one 100 ms device buffer plus the compressed song itself.
package audio

import (
	"fmt"
	"os"
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
	procCoTaskMemFree    = ole32.NewProc("CoTaskMemFree")
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

	waveFormatPCM  = 1
	waveFormatIEEE = 3
	waveFormatExtensible = 0xFFFE
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

type extendedFormat struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	CbSize         uint16
	ValidBits      uint16
	ChannelMask    uint32
	SubFormat      [16]byte
}

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
//
// It must NOT allocate: callers take &localStruct and pass it as a uintptr, and
// any allocation between that conversion and the syscall can grow the goroutine
// stack and invalidate the pointer's address. The 32-bit build hit exactly this
// — IAudioClient::Initialize returned E_INVALIDARG because the slice allocation
// below moved the WAVEFORMATEX out from under the pointer. A fixed-size array
// keeps the call allocation free.
func comCall(obj unsafe.Pointer, idx int, args ...uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, uintptr(idx)*unsafe.Sizeof(uintptr(0))))
	var buf [16]uintptr
	n := len(args) + 1
	if n > len(buf) {
		// Nothing in this package passes that many arguments; guard anyway.
		full := make([]uintptr, 0, n)
		full = append(full, uintptr(obj))
		full = append(full, args...)
		r := syscallV(fn, full)
		return r
	}
	buf[0] = uintptr(obj)
	copy(buf[1:], args)
	r := syscallV(fn, buf[:n])
	runtime.KeepAlive(obj)
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

	// Autoconvert is true when the audio engine converts our 16-bit PCM to the
	// endpoint format (AUTOCONVERTPCM). When it is false we opened the stream
	// with the endpoint's own mix format and Dev tells the player how to
	// convert samples itself — the Windows 7 safety net for drivers that reject
	// the auto-convert path.
	Autoconvert bool
	Dev         DevFormat

	// wf lives in the heap-owned AudioOut so its address stays valid even if
	// the goroutine stack moves while the call is being prepared.
	wf waveFormatEx

	bufPtr unsafe.Pointer
	started bool
	coInit  bool
}

// DevFormat describes an endpoint mix format.
type DevFormat struct {
	Rate       int
	Channels   int
	Bits       int
	IsFloat    bool
	BlockAlign int
}

// ForceMixFormat disables the AUTOCONVERTPCM path so the manual sample
// conversion can be exercised on machines where auto-convert works
// (-force-audio-fallback, and nmprobe wasapi).
var ForceMixFormat bool

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

	tracef("OpenStream rate=%d forceMix=%v", sampleRate, ForceMixFormat)
	a := &AudioOut{SampleRate: sampleRate, Channels: channels, coInit: true}
	if err := a.init(sampleRate, channels); err != nil {
		a.Close()
		return nil, err
	}
	return a, nil
}

// Diagnose runs an IAudioClient::Initialize argument matrix and returns a log.
// It exists because the 32-bit build fails where the 64-bit build succeeds with
// byte-identical arguments, and guessing is expensive.
func Diagnose() []string {
	var out []string
	runtime.LockOSThread()
	hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded)
	out = append(out, fmt.Sprintf("CoInitializeEx hr=0x%08x sizeof(uintptr)=%d", uint32(hr), unsafe.Sizeof(uintptr(0))))

	var enum unsafe.Pointer
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&enum)))
	out = append(out, fmt.Sprintf("CoCreateInstance hr=0x%08x", uint32(hr)))
	if int32(hr) < 0 {
		return out
	}
	defer comRelease(enum)

	var dev unsafe.Pointer
	r := comCall(enum, 4, uintptr(eRender), uintptr(eConsole), uintptr(unsafe.Pointer(&dev)))
	out = append(out, fmt.Sprintf("GetDefaultAudioEndpoint hr=0x%08x", uint32(r)))
	if int32(r) < 0 {
		return out
	}
	defer comRelease(dev)

	var client unsafe.Pointer
	r = comCall(dev, 3, uintptr(unsafe.Pointer(&iidIAudioClient)), clsctxAll, 0, uintptr(unsafe.Pointer(&client)))
	out = append(out, fmt.Sprintf("Activate hr=0x%08x client=%p", uint32(r), client))
	if int32(r) < 0 {
		return out
	}
	defer comRelease(client)

	wf := &waveFormatEx{
		FormatTag: waveFormatPCM, Channels: 2, SamplesPerSec: 44100,
		BitsPerSample: 16, BlockAlign: 4, AvgBytesPerSec: 176400,
	}
	out = append(out, fmt.Sprintf("fmt=%p %+v", unsafe.Pointer(wf), *wf))

	// IsFormatSupported(shared, fmt, &closest): tells us whether the format
	// pointer is even readable by the object.
	var closest unsafe.Pointer
	r = comCall(client, 7, uintptr(audclntShareModeShared), uintptr(unsafe.Pointer(wf)), uintptr(unsafe.Pointer(&closest)))
	out = append(out, fmt.Sprintf("IsFormatSupported(shared, pcm16/44k) hr=0x%08x", uint32(r)))
	if closest != nil {
		m := (*extendedFormat)(closest)
		out = append(out, fmt.Sprintf("  closest: tag=%d ch=%d rate=%d bits=%d block=%d",
			m.FormatTag, m.Channels, m.SamplesPerSec, m.BitsPerSample, m.BlockAlign))
		procCoTaskMemFree.Call(uintptr(closest))
	}

	tryInit := func(label string, flags uintptr, dur int64, fmtPtr *waveFormatEx) {
		r := comCall(client, 3, initArgs(flags, uint64(dur), 0,
			uintptr(unsafe.Pointer(fmtPtr)), 0)...)
		out = append(out, fmt.Sprintf("Initialize[%s] flags=0x%08x dur=%d hr=0x%08x", label, uint64(flags), dur, uint32(r)))
	}

	tryInit("autocvt+srcdefault 100ms", audclntStreamFlagsAutoConvertPCM|audclntStreamFlagsSRCDefaultQuality, 1000000, wf)
	tryInit("autocvt only", audclntStreamFlagsAutoConvertPCM, 1000000, wf)
	tryInit("no flags", 0, 1000000, wf)
	tryInit("autocvt 200ms", audclntStreamFlagsAutoConvertPCM|audclntStreamFlagsSRCDefaultQuality, 2000000, wf)

	// Exact mix format, no conversion requested.
	var mix unsafe.Pointer
	if hr := comCall(client, 8, uintptr(unsafe.Pointer(&mix))); int32(hr) >= 0 && mix != nil {
		m := (*extendedFormat)(mix)
		out = append(out, fmt.Sprintf("mixFormat: tag=%d ch=%d rate=%d bits=%d block=%d cbSize=%d",
			m.FormatTag, m.Channels, m.SamplesPerSec, m.BitsPerSample, m.BlockAlign, m.CbSize))
		r := comCall(client, 3, initArgs(0, 1000000, 0, uintptr(mix), 0)...)
		out = append(out, fmt.Sprintf("Initialize[mixfmt exact] hr=0x%08x", uint32(r)))
		procCoTaskMemFree.Call(uintptr(mix))
	}
	procCoUninitialize.Call()
	return out
}

// DebugTrace enables verbose HRESULT tracing to stderr (used by nmprobe).
var DebugTrace bool

func tracef(format string, args ...any) {
	if DebugTrace {
		fmt.Fprintf(os.Stderr, "[wasapi] "+format+"\n", args...)
	}
}

// initArgs builds the IAudioClient::Initialize argument list. The two
// REFERENCE_TIME values are 64-bit in C, so they need two stack slots each on
// 32-bit Windows (see comArg64).
func initArgs(flags uintptr, bufferDuration, periodicity uint64, fmt, guid uintptr) []uintptr {
	args := make([]uintptr, 0, 12)
	args = append(args, uintptr(audclntShareModeShared), flags)
	args = append(args, comArg64(bufferDuration)...)
	args = append(args, comArg64(periodicity)...)
	args = append(args, fmt, guid)
	return args
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
	tracef("CoCreateInstance ok enum=%p", enum)

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

	a.wf = waveFormatEx{
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
	tracef("Initialize: wf=%+v sizeof=%d flags=0x%x dur=%d fmt=%p",
		a.wf, unsafe.Sizeof(a.wf), uint64(flags), uint64(hnsBuffer), unsafe.Pointer(&a.wf))
	// Try the auto-convert path first. It must be skipped entirely when forced
	// into the fallback: IAudioClient can only be initialized once, so a
	// "successful attempt followed by another attempt" yields
	// AUDCLNT_E_ALREADY_INITIALIZED.
	var autoErr error
	AutoAttempted := false
	if !ForceMixFormat {
		AutoAttempted = true
		r = comCall(client, 3, initArgs(flags, uint64(hnsBuffer), 0, uintptr(unsafe.Pointer(&a.wf)), 0)...)
		autoErr = hresult(r)
	}
	if AutoAttempted && autoErr == nil {
		a.Autoconvert = true
	} else {
		// Fallback: open the stream with the endpoint's exact mix format and do
		// the conversion ourselves. Windows 7 audio drivers are far less
		// forgiving about AUTOCONVERTPCM than modern ones.
		if autoErr != nil {
			tracef("autoconvert rejected (%v), trying exact mix format", autoErr)
		}
		var mix unsafe.Pointer
		if hr := comCall(client, 8, uintptr(unsafe.Pointer(&mix))); hresult(hr) != nil || mix == nil {
			return fmt.Errorf("IAudioClient.Initialize: %w", autoErr)
		}
		m := (*extendedFormat)(mix)
		dev := DevFormat{
			Rate:       int(m.SamplesPerSec),
			Channels:   int(m.Channels),
			Bits:       int(m.BitsPerSample),
			BlockAlign: int(m.BlockAlign),
		}
		// WAVE_FORMAT_EXTENSIBLE carries the real format in the SubFormat GUID's
		// first two bytes; 3 == IEEE float.
		if m.FormatTag == 0xFFFE {
			dev.IsFloat = m.SubFormat[0] == 3 && m.SubFormat[1] == 0
			if m.ValidBits != 0 {
				dev.Bits = int(m.ValidBits)
			}
		} else if m.FormatTag == waveFormatIEEE {
			dev.IsFloat = true
		}
		canConvert := dev.Channels == 2 && !dev.IsFloat && dev.Bits == 16
		canConvert = canConvert || (dev.IsFloat && (dev.Bits == 32 || dev.Bits == 64))
		canConvert = canConvert || (!dev.IsFloat && (dev.Bits == 24 || dev.Bits == 32))
		if !canConvert {
			procCoTaskMemFree.Call(uintptr(mix))
			if autoErr != nil {
				return fmt.Errorf("IAudioClient.Initialize: %w", autoErr)
			}
			return fmt.Errorf("IAudioClient.Initialize: unsupported mix format (%d bit, %d ch)", dev.Bits, dev.Channels)
		}
		tracef("fallback init with mix format %d Hz %d bit", dev.Rate, dev.Bits)
		r = comCall(client, 3, initArgs(0, uint64(hnsBuffer), 0, uintptr(mix), 0)...)
		initErr := hresult(r)
		if initErr != nil {
			procCoTaskMemFree.Call(uintptr(mix))
			return fmt.Errorf("IAudioClient.Initialize(%d Hz/%d bit): %w", dev.Rate, dev.Bits, initErr)
		}
		// Copy the format so the pointer can be freed; the value is only needed
		// for bookkeeping from here on.
		a.wf = waveFormatEx{
			FormatTag:      m.FormatTag,
			Channels:       uint16(dev.Channels),
			SamplesPerSec:  uint32(dev.Rate),
			BitsPerSample:  uint16(dev.Bits),
			BlockAlign:     uint16(dev.BlockAlign),
			AvgBytesPerSec: uint32(dev.Rate * dev.BlockAlign),
		}
		procCoTaskMemFree.Call(uintptr(mix))
		a.Autoconvert = false
		a.Dev = dev
		a.BytesPerFrame = dev.BlockAlign
		tracef("using mix format: rate=%d bits=%d float=%v block=%d", dev.Rate, dev.Bits, dev.IsFloat, dev.BlockAlign)
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
