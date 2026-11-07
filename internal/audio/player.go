package audio

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"github.com/hajimehoshi/go-mp3"
)

// Player owns one decode + render goroutine. Everything that touches COM or
// the decoder happens on that goroutine; the UI only posts commands and reads
// atomics, so no locking shows up on the paint path.
type Player struct {
	cmds    chan command
	vol     atomic.Int32
	playing atomic.Bool
	posMs   atomic.Int64
	durMs   atomic.Int64
	closed  atomic.Bool

	mu   sync.Mutex
	note string // last state note, e.g. "缓冲中…" (UI reads it rarely)

	OnEnd   func()
	OnError func(error)

	done chan struct{}
}

const (
	cmdLoad = iota
	cmdToggle
	cmdPlay
	cmdPause
	cmdSeek
	cmdQuit
	cmdVolume
)

type command struct {
	kind int
	data []byte
	frac float64
	vol  int
}

// NewPlayer creates the player and starts its goroutine.
func NewPlayer() *Player {
	p := &Player{cmds: make(chan command, 8), done: make(chan struct{})}
	p.vol.Store(70)
	go p.loop()
	return p
}

func (p *Player) SetOnEnd(f func())   { p.OnEnd = f }
func (p *Player) SetOnError(f func(error)) { p.OnError = f }

// Load hands a compressed MP3 buffer to the player. The buffer is owned by the
// player and released when the next song loads.
func (p *Player) Load(data []byte, seconds int) {
	select {
	case p.cmds <- command{kind: cmdLoad, data: data}:
	default:
		// Queue full (rapid track switching): drop the oldest by draining one.
		select {
		case <-p.cmds:
		default:
		}
		p.cmds <- command{kind: cmdLoad, data: data}
	}
	p.durMs.Store(int64(seconds) * 1000)
}

func (p *Player) Toggle()      { p.post(command{kind: cmdToggle}) }
func (p *Player) Play()        { p.post(command{kind: cmdPlay}) }
func (p *Player) Pause()       { p.post(command{kind: cmdPause}) }
func (p *Player) SeekFrac(f float64) { p.post(command{kind: cmdSeek, frac: f}) }

func (p *Player) SetVolume(v int) {
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	p.vol.Store(int32(v))
}

func (p *Player) Volume() int { return int(p.vol.Load()) }

func (p *Player) post(c command) {
	select {
	case p.cmds <- c:
	default:
	}
}

// Position returns the playback position in seconds.
func (p *Player) Position() float64 { return float64(p.posMs.Load()) / 1000 }

// Duration returns the track length in seconds.
func (p *Player) Duration() float64 { return float64(p.durMs.Load()) / 1000 }

func (p *Player) IsPlaying() bool { return p.playing.Load() }

func (p *Player) Close() {
	if p.closed.Swap(true) {
		return
	}
	p.post(command{kind: cmdQuit})
	<-p.done
}

var errNoDevice = errors.New("audio: 无法打开音频设备 (WASAPI)")

// loop is the audio owner thread.
func (p *Player) loop() {
	defer close(p.done)

	var (
		out     *AudioOut
		dec     *mp3.Decoder
		src     *bytes.Reader
		payload []byte
		rate    int
		base    int64 // absolute sample frame where decoding resumed
		written int64 // frames written since the last reset
		eof     bool
		ending  bool
	)

	cleanup := func() {
		if out != nil {
			out.Close()
			out = nil
		}
	}

	openFor := func(sampleRate int) error {
		if out != nil && out.SampleRate == sampleRate {
			return nil
		}
		cleanup()
		o, err := OpenStream(sampleRate, 2)
		if err != nil {
			return err
		}
		out = o
		return nil
	}

	reset := func() {
		if out != nil {
			out.Stop()
			out.Reset()
		}
		written = 0
		eof = false
		ending = false
	}

	for {
		// Handle every pending command first, without blocking.
		quit := false
	drain:
		for {
			select {
			case c := <-p.cmds:
				switch c.kind {
				case cmdQuit:
					quit = true
					break drain
				case cmdLoad:
					reset()
					p.playing.Store(false)
					dec = nil
					src = nil
					payload = c.data
					if payload == nil {
						continue
					}
					src = bytes.NewReader(payload)
					d, err := mp3.NewDecoder(src)
					if err != nil {
						p.playing.Store(false)
						if p.OnError != nil {
							go p.OnError(err)
						}
						continue
					}
					dec = d
					rate = d.SampleRate()
					base = 0
					eof = false
					if err := openFor(rate); err != nil {
						p.playing.Store(false)
						if p.OnError != nil {
							go p.OnError(err)
						}
						continue
					}
					p.posMs.Store(0)
					p.playing.Store(true)
					out.Start()
				case cmdToggle:
					if dec == nil {
						continue
					}
					if p.playing.Load() {
						p.playing.Store(false)
						if out != nil {
							out.Stop()
						}
					} else if !eof {
						p.playing.Store(true)
						if out != nil {
							out.Start()
						}
					}
				case cmdPlay:
					if dec != nil && !eof {
						p.playing.Store(true)
						if out != nil {
							out.Start()
						}
					}
				case cmdPause:
					p.playing.Store(false)
					if out != nil {
						out.Stop()
					}
				case cmdSeek:
					if dec == nil || out == nil {
						continue
					}
					total := dec.Length()
					target := int64(c.frac * float64(total))
					target -= target % 4 // whole stereo frames only
					if target < 0 {
						target = 0
					}
					wasPlaying := p.playing.Load()
					out.Stop()
					out.Reset()
					if _, err := dec.Seek(target, io.SeekStart); err != nil {
						// Seeking is unsupported for this stream; restart it.
						src.Seek(0, io.SeekStart)
						if d2, err2 := mp3.NewDecoder(src); err2 == nil {
							dec = d2
						}
						target = 0
					}
					base = target / 4
					written = 0
					eof = false
					ending = false
					p.posMs.Store(base * 1000 / int64(rate))
					if wasPlaying {
						out.Start()
					}
				case cmdVolume:
					// volume lives in an atomic; nothing to do here
				}
			default:
				break drain
			}
		}
		if quit {
			break
		}

		if dec == nil || out == nil || !p.playing.Load() || eof {
			nanosleep(8)
			continue
		}

		// Feed the device buffer directly from the decoder.
		avail := out.BufferFrames - out.Padding()
		if avail <= 0 {
			nanosleep(2)
			continue
		}
		if avail > out.BufferFrames {
			avail = out.BufferFrames
		}
		ptr, err := out.Acquire(avail)
		if err != nil {
			nanosleep(4)
			continue
		}
		buf := Slice(ptr, avail*out.BytesPerFrame)
		n, rerr := io.ReadFull(dec, buf)
		if n < len(buf) {
			// silence pad at end of stream
			for i := n; i < len(buf); i++ {
				buf[i] = 0
			}
			eof = true
		}
		applyGain(buf[:n], int(p.vol.Load()))
		out.Release(avail)
		written += int64(avail)

		// Position = decoded frames minus what is still queued.
		inflight := out.Padding()
		frames := base + written - int64(inflight)
		if frames < 0 {
			frames = 0
		}
		if rate > 0 {
			p.posMs.Store(frames * 1000 / int64(rate))
		}

		if (eof || rerr != nil) && !ending {
			ending = true
			p.playing.Store(false)
			if out != nil {
				out.Stop()
			}
			if p.OnEnd != nil {
				go p.OnEnd()
			}
		}
	}

	cleanup()
}

// applyGain scales interleaved int16 samples in place. The curve is squared so
// the slider feels linear to the ear.
func applyGain(buf []byte, vol int) {
	if vol >= 100 || len(buf) < 2 {
		return
	}
	if vol <= 0 {
		for i := range buf {
			buf[i] = 0
		}
		return
	}
	g := int32(vol)
	g = g * g // 0..10000
	const half = 50 // gain = (vol^2) / 10000 * 32768, pre-scaled by 50
	// remaining is signed 16-bit; keep the arithmetic on int32 to avoid
	// per-sample overflow surprises.
	rem := g
	_ = half
	for i := 0; i+1 < len(buf); i += 2 {
		s := int32(int16(uint16(buf[i]) | uint16(buf[i+1])<<8))
		v := s * rem / 10000
		buf[i] = byte(uint16(v))
		buf[i+1] = byte(uint16(v) >> 8)
	}
}
