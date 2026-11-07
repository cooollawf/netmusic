package audio

import "math"

// EQBands is the number of equaliser bands. Kept in sync with config.EQBands.
const EQBands = 10

// EQFreqs are the band centre frequencies in Hz (one octave apart).
var EQFreqs = [EQBands]float64{31, 62, 125, 250, 500, 1000, 2000, 4000, 8000, 16000}

// eqQ is the bandwidth of each peaking band: Q = sqrt(2) is roughly one octave,
// which makes ten of them cover the audible range without huge overlaps.
const eqQ = 1.41

type eqCoef struct {
	b0, b1, b2 float32
	a1, a2     float32
}

type eqState struct {
	x1, x2 float32
	y1, y2 float32
}

// EQ is a ten band peaking equaliser working on interleaved int16 stereo.
//
// Implementation notes that matter on the target machine (a Core 2 era
// ThinkPad): coefficients are precomputed per stream rate, flat bands are
// skipped entirely (an all-flat EQ costs zero), and the whole chain is bypassed
// when disabled, so the common case adds no CPU at all.
type EQ struct {
	enabled bool
	rate    int

	coef   [EQBands]eqCoef
	state  [EQBands][2]eqState
	active [EQBands]int // indices of bands that actually do something
	nActive int
}

// Configure recomputes the filters for the given gains (dB) and sample rate.
func (e *EQ) Configure(gains [EQBands]int8, enabled bool, sampleRate int) {
	e.enabled = enabled
	if sampleRate <= 0 {
		sampleRate = 44100
	}
	if sampleRate != e.rate {
		e.rate = sampleRate
		e.Reset()
	}
	e.nActive = 0
	for i := 0; i < EQBands; i++ {
		g := float64(gains[i])
		if g > 12 {
			g = 12
		}
		if g < -12 {
			g = -12
		}
		if !enabled || (g > -0.5 && g < 0.5) {
			continue
		}
		f := EQFreqs[i]
		if f > float64(sampleRate)*0.45 {
			continue // above Nyquist for this stream
		}
		a := math.Pow(10, g/40)
		w0 := 2 * math.Pi * f / float64(sampleRate)
		alpha := math.Sin(w0) / (2 * eqQ)
		cosw := math.Cos(w0)
		a0 := 1 + alpha/a
		e.coef[i] = eqCoef{
			b0: float32((1 + alpha*a) / a0),
			b1: float32((-2 * cosw) / a0),
			b2: float32((1 - alpha*a) / a0),
			a1: float32((-2 * cosw) / a0),
			a2: float32((1 - alpha/a) / a0),
		}
		e.active[e.nActive] = i
		e.nActive++
	}
	if e.nActive == 0 {
		e.Reset()
	}
}

// Active reports whether processing changes anything.
func (e *EQ) Active() bool { return e != nil && e.enabled && e.nActive > 0 }

// Reset clears the filter state (new stream / seek).
func (e *EQ) Reset() {
	for i := range e.state {
		e.state[i][0] = eqState{}
		e.state[i][1] = eqState{}
	}
}

// Process filters interleaved int16 stereo samples in place.
func (e *EQ) Process(buf []byte) {
	if !e.Active() {
		return
	}
	for i := 0; i+3 < len(buf); i += 4 {
		l := int16(uint16(buf[i]) | uint16(buf[i+1])<<8)
		r := int16(uint16(buf[i+2]) | uint16(buf[i+3])<<8)
		l, r = e.frame(l, r)
		buf[i] = byte(uint16(l))
		buf[i+1] = byte(uint16(l) >> 8)
		buf[i+2] = byte(uint16(r))
		buf[i+3] = byte(uint16(r) >> 8)
	}
}

// ProcessFrame filters a single stereo frame.
func (e *EQ) ProcessFrame(l, r int16) (int16, int16) {
	if !e.Active() {
		return l, r
	}
	return e.frame(l, r)
}

func (e *EQ) frame(l, r int16) (int16, int16) {
	x := float32(l)
	y := float32(r)
	for k := 0; k < e.nActive; k++ {
		b := e.active[k]
		c := &e.coef[b]
		s := &e.state[b]

		xo := c.b0*x + c.b1*s[0].x1 + c.b2*s[0].x2 - c.a1*s[0].y1 - c.a2*s[0].y2
		s[0].x2, s[0].x1 = s[0].x1, x
		s[0].y2, s[0].y1 = s[0].y1, xo
		x = xo

		yo := c.b0*y + c.b1*s[1].x1 + c.b2*s[1].x2 - c.a1*s[1].y1 - c.a2*s[1].y2
		s[1].x2, s[1].x1 = s[1].x1, y
		s[1].y2, s[1].y1 = s[1].y1, yo
		y = yo
	}
	return clamp16(x), clamp16(y)
}

func clamp16(v float32) int16 {
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return int16(v)
}
