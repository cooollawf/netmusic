package audio

import (
	"encoding/binary"
	"io"
	"math"
)

// converter turns the decoder's 16-bit stereo stream into whatever the endpoint
// mix format is (16/24/32-bit integer or 32/64-bit float, any sample rate).
//
// It only runs when IAudioClient::Initialize rejected the AUTOCONVERTPCM path,
// which is exactly what old Windows 7 audio drivers do. Keeping it in its own
// file makes the fast path in player.go stay readable.
type converter struct {
	dev     DevFormat
	srcRate int

	buf     []int16 // pending source samples, interleaved stereo
	n       int     // valid samples in buf
	frac    float64 // fractional read position, in source frames
	staging []byte
	eq      *EQ
	scratch []int16
	eof     bool
}

func newConverter(dev DevFormat, srcRate int) *converter {
	return &converter{dev: dev, srcRate: srcRate}
}

// reset drops buffered source samples; called on seek and on new track.
func (c *converter) reset() {
	c.n = 0
	c.frac = 0
	c.eof = false
}

// fill produces exactly avail device frames into dst and reports whether the
// source is exhausted.
func (c *converter) fill(dec io.Reader, avail int, dst []byte, vol int) (bool, error) {
	if avail <= 0 {
		return false, nil
	}
	ratio := float64(c.srcRate) / float64(c.dev.Rate)

	// Source samples needed to interpolate `avail` output frames.
	needSamples := (int(float64(avail)*ratio) + 2) * 2
	if cap(c.buf) < needSamples {
		nb := make([]int16, needSamples, needSamples*2)
		copy(nb, c.buf[:c.n])
		c.buf = nb
	}
	c.buf = c.buf[:cap(c.buf)]
	if cap(c.staging) < needSamples*2 {
		c.staging = make([]byte, needSamples*2)
	}

	for c.n < needSamples && !c.eof {
		want := needSamples - c.n
		k, err := io.ReadFull(dec, c.staging[:want*2])
		for i := 0; i+1 < k; i += 2 {
			c.buf[c.n] = int16(uint16(c.staging[i]) | uint16(c.staging[i+1])<<8)
			c.n++
		}
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				c.eof = true
			} else {
				return c.eof, err
			}
		}
	}

	// Volume curve is applied here so the manual path matches the fast path.
	gain := int32(vol) * int32(vol)
	if vol >= 100 {
		gain = 10000
	}

	frames := c.n / 2
	o := 0
	var l, r int16
	for i := 0; i < avail; i++ {
		if frames == 0 {
			l, r = 0, 0
		} else {
			pos := c.frac + float64(i)*ratio
			i0 := int(pos)
			if i0 >= frames {
				i0 = frames - 1
			}
			if i0 < 0 {
				i0 = 0
			}
			i1 := i0 + 1
			if i1 >= frames {
				i1 = frames - 1
			}
			f := pos - float64(i0)
			l = int16(lerp16(c.buf[i0*2], c.buf[i1*2], f) * gain / 10000)
			r = int16(lerp16(c.buf[i0*2+1], c.buf[i1*2+1], f) * gain / 10000)
		}
		if c.eq != nil && c.eq.Active() {
			l, r = c.eq.ProcessFrame(l, r)
		}
		c.writeFrame(dst[o:], l, r)
		o += c.dev.BlockAlign
	}

	// Drop the frames we have fully passed, keep the fractional remainder.
	next := c.frac + float64(avail)*ratio
	consume := int(next)
	if consume > frames {
		consume = frames
	}
	if consume > 0 {
		copy(c.buf, c.buf[consume*2:c.n])
		c.n -= consume * 2
	}
	c.frac = next - float64(consume)
	return c.eof && c.n < 2, nil
}

func lerp16(a, b int16, f float64) int32 {
	if f <= 0 {
		return int32(a)
	}
	if f >= 1 {
		return int32(b)
	}
	return int32(float64(a) + (float64(b)-float64(a))*f)
}

func (c *converter) writeFrame(dst []byte, l, r int16) {
	switch {
	case c.dev.IsFloat && c.dev.Bits == 32:
		binary.LittleEndian.PutUint32(dst[0:], math.Float32bits(float32(l)/32768))
		binary.LittleEndian.PutUint32(dst[4:], math.Float32bits(float32(r)/32768))
	case c.dev.IsFloat && c.dev.Bits == 64:
		binary.LittleEndian.PutUint64(dst[0:], math.Float64bits(float64(l)/32768))
		binary.LittleEndian.PutUint64(dst[8:], math.Float64bits(float64(r)/32768))
	case c.dev.Bits == 16:
		binary.LittleEndian.PutUint16(dst[0:], uint16(l))
		binary.LittleEndian.PutUint16(dst[2:], uint16(r))
	case c.dev.Bits == 24:
		v := int32(l) << 8
		dst[0], dst[1], dst[2] = byte(v), byte(uint32(v)>>8), byte(uint32(v)>>16)
		v = int32(r) << 8
		dst[3], dst[4], dst[5] = byte(v), byte(uint32(v)>>8), byte(uint32(v)>>16)
	default: // 32-bit integer
		binary.LittleEndian.PutUint32(dst[0:], uint32(int32(l)<<16))
		binary.LittleEndian.PutUint32(dst[4:], uint32(int32(r)<<16))
	}
}
