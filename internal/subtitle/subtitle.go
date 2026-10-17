// Package subtitle parses SRT and WebVTT files into cues and writes WebVTT,
// and names languages for track labels. It holds pure functions only: it
// reads no files and knows nothing about the database.
package subtitle

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Cue is one timed subtitle line (possibly several text lines).
type Cue struct {
	StartMS int64
	EndMS   int64
	Text    string
}

// ErrNoCues is returned when a file holds no recognizable cues.
var ErrNoCues = errors.New("no subtitle cues found")

// Parse reads SRT or WebVTT. The format is detected from the content, not the
// file name. Input that is not valid UTF-8 is decoded as Latin-1, a best
// effort for the legacy encodings older subtitle files often use. It returns
// ErrNoCues if nothing in data is a cue, and an error quoting the bad text
// for a malformed timestamp.
func Parse(data []byte) ([]Cue, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(data) {
		data = latin1ToUTF8(data)
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	var p parser
	for sc.Scan() {
		if err := p.line(sc.Text()); err != nil {
			return nil, err
		}
	}
	p.flush()
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(p.cues) == 0 {
		return nil, ErrNoCues
	}
	return p.cues, nil
}

// parser reads SRT and WebVTT a line at a time. Both formats are blocks
// separated by blank lines; a cue block is an optional identifier, a timing
// line and the text.
type parser struct {
	cues   []Cue
	cur    *Cue     // the cue being read, once its timing line is seen
	text   []string // its text lines so far
	inNote bool     // inside a WebVTT header, NOTE, STYLE or REGION block
}

func (p *parser) line(line string) error {
	trimmed := strings.TrimSpace(line)
	switch {
	case trimmed == "":
		p.flush()
		p.inNote = false
	case p.inNote:
	case p.cur != nil:
		p.text = append(p.text, line)
	case strings.HasPrefix(trimmed, "WEBVTT"), strings.HasPrefix(trimmed, "STYLE"),
		strings.HasPrefix(trimmed, "REGION"), strings.HasPrefix(trimmed, "NOTE"):
		p.inNote = true
	case strings.Contains(trimmed, "-->"):
		start, end, err := parseTiming(trimmed)
		if err != nil {
			return err
		}
		p.cur = &Cue{StartMS: start, EndMS: end}
	default:
		// A cue identifier (an SRT index or VTT id): skip it.
	}
	return nil
}

// flush ends the current block, keeping its cue if it has text.
func (p *parser) flush() {
	if p.cur != nil {
		p.cur.Text = strings.TrimSpace(strings.Join(p.text, "\n"))
		if p.cur.Text != "" {
			p.cues = append(p.cues, *p.cur)
		}
	}
	p.cur, p.text = nil, nil
}

// parseTiming reads a "start --> end [settings]" line.
func parseTiming(line string) (int64, int64, error) {
	parts := strings.SplitN(line, "-->", 2)
	start, err := parseTimestamp(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, err
	}
	endField := strings.Fields(strings.TrimSpace(parts[1]))
	if len(endField) == 0 {
		return 0, 0, fmt.Errorf("bad cue timing %q", line)
	}
	end, err := parseTimestamp(endField[0])
	if err != nil {
		return 0, 0, err
	}
	return start, end, nil
}

// parseTimestamp accepts hh:mm:ss.mmm, mm:ss.mmm and the SRT comma form.
func parseTimestamp(s string) (int64, error) {
	s = strings.Replace(s, ",", ".", 1)
	var frac int64
	if i := strings.IndexByte(s, '.'); i >= 0 {
		f := s[i+1:]
		for len(f) < 3 {
			f += "0"
		}
		n, err := strconv.ParseInt(f[:3], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("bad timestamp %q", s)
		}
		frac, s = n, s[:i]
	}
	fields := strings.Split(s, ":")
	if len(fields) < 2 || len(fields) > 3 {
		return 0, fmt.Errorf("bad timestamp %q", s)
	}
	var total int64
	for _, f := range fields {
		n, err := strconv.ParseInt(f, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("bad timestamp %q", s)
		}
		total = total*60 + n
	}
	return total*1000 + frac, nil
}

// WriteVTT renders cues as a WebVTT document.
func WriteVTT(cues []Cue) []byte {
	var b bytes.Buffer
	b.WriteString("WEBVTT\n\n")
	for _, c := range cues {
		fmt.Fprintf(&b, "%s --> %s\n%s\n\n", formatTimestamp(c.StartMS), formatTimestamp(c.EndMS), sanitizeCueText(c.Text))
	}
	return b.Bytes()
}

func formatTimestamp(ms int64) string {
	h := ms / 3600000
	m := ms / 60000 % 60
	s := ms / 1000 % 60
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms%1000)
}

// sanitizeCueText keeps a cue from breaking out of its block: "-->" and blank
// lines would otherwise start a new cue.
func sanitizeCueText(t string) string {
	t = strings.ReplaceAll(t, "-->", "->")
	var out []string
	for _, l := range strings.Split(t, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// latin1ToUTF8 maps each byte to the code point of the same value.
func latin1ToUTF8(b []byte) []byte {
	var out bytes.Buffer
	for _, c := range b {
		out.WriteRune(rune(c))
	}
	return out.Bytes()
}
