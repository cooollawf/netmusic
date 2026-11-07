// Package lrc parses LRC lyrics into a tiny immutable document. Only the
// timestamps and text are kept: a 5 minute song costs a few kilobytes.
package lrc

import (
	"strconv"
	"strings"
)

// Line is one timed lyric line.
type Line struct {
	Ms   int64
	Text string
}

// Doc is a parsed lyric file.
type Doc struct {
	Lines []Line
	// HasTrans is true when at least one line carries a translation, so the UI
	// knows it needs two text lines per row.
	HasTrans bool
}

// Parse accepts the NetEase LRC text, with optional translation text merged in
// by timestamp.
func Parse(raw, translated string) *Doc {
	d := &Doc{}
	if raw == "" {
		return d
	}
	// Pre-index translations by millisecond so merging stays O(n log n).
	tr := map[int64]string{}
	if translated != "" {
		for _, ln := range parseLines(translated) {
			tr[ln.Ms] = ln.Text
		}
	}
	d.Lines = parseLines(raw)
	if len(tr) > 0 {
		merged := false
		out := d.Lines[:0]
		for _, ln := range d.Lines {
			if t, ok := tr[ln.Ms]; ok && t != "" && t != ln.Text {
				ln.Text = ln.Text + "\n" + t
				merged = true
			}
			out = append(out, ln)
		}
		d.Lines = out
		d.HasTrans = merged
	}
	return d
}

func parseLines(raw string) []Line {
	lines := make([]Line, 0, 96)
	for _, ln := range strings.Split(raw, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if !strings.HasPrefix(ln, "[") {
			continue
		}
		// one line may carry several [mm:ss.xx] tags
		var stamps []int64
		rest := ln
		for strings.HasPrefix(rest, "[") {
			end := strings.IndexByte(rest, ']')
			if end < 0 {
				break
			}
			tag := rest[1:end]
			rest = rest[end+1:]
			ms, ok := parseStamp(tag)
			if !ok {
				// metadata tags like [ar:xxx] are simply dropped
				stamps = nil
				break
			}
			stamps = append(stamps, ms)
		}
		if len(stamps) == 0 {
			continue
		}
		text := strings.TrimSpace(rest)
		for _, ms := range stamps {
			lines = append(lines, Line{Ms: ms, Text: text})
		}
	}
	// sort by time (LRC is usually ordered, but multi-tag lines are not)
	for i := 1; i < len(lines); i++ {
		for j := i; j > 0 && lines[j-1].Ms > lines[j].Ms; j-- {
			lines[j-1], lines[j] = lines[j], lines[j-1]
		}
	}
	return lines
}

func parseStamp(tag string) (int64, bool) {
	// mm:ss.xx or mm:ss:xx or mm:ss
	if len(tag) < 4 || tag[2] != ':' {
		return 0, false
	}
	min, err := strconv.Atoi(tag[:2])
	if err != nil {
		return 0, false
	}
	rest := tag[3:]
	secStr := rest
	fracStr := ""
	for i := 0; i < len(rest); i++ {
		if rest[i] == '.' || rest[i] == ':' {
			secStr = rest[:i]
			fracStr = rest[i+1:]
			break
		}
	}
	sec, err := strconv.Atoi(strings.TrimSpace(secStr))
	if err != nil {
		return 0, false
	}
	frac := int64(0)
	if fracStr != "" {
		switch len(fracStr) {
		case 1:
			fracStr += "00"
		case 2:
			fracStr += "0"
		}
		if len(fracStr) > 3 {
			fracStr = fracStr[:3]
		}
		v, err := strconv.Atoi(fracStr)
		if err != nil {
			return 0, false
		}
		frac = int64(v)
	}
	return int64(min)*60000 + int64(sec)*1000 + frac, true
}

// Len returns the number of lines.
func (d *Doc) Len() int {
	if d == nil {
		return 0
	}
	return len(d.Lines)
}

// IndexAt returns the index of the line playing at ms, or -1 before the first.
func (d *Doc) IndexAt(ms int64) int {
	if d == nil || len(d.Lines) == 0 {
		return -1
	}
	lo, hi := 0, len(d.Lines)-1
	if ms < d.Lines[0].Ms {
		return -1
	}
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if d.Lines[mid].Ms <= ms {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}
