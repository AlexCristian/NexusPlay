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

	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var cueParser parser
	for scanner.Scan() {
		if err := cueParser.line(scanner.Text()); err != nil {
			return nil, err
		}
	}
	cueParser.flush()
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(cueParser.cues) == 0 {
		return nil, ErrNoCues
	}
	return cueParser.cues, nil
}

// parser reads SRT and WebVTT a line at a time. Both formats are blocks
// separated by blank lines; a cue block is an optional identifier, a timing
// line and the text.
type parser struct {
	cues    []Cue
	current *Cue     // the cue being read, once its timing line is seen
	text    []string // its text lines so far
	inNote  bool     // inside a WebVTT header, NOTE, STYLE or REGION block
}

func (parser *parser) line(line string) error {
	trimmed := strings.TrimSpace(line)
	switch {
	case trimmed == "":
		parser.flush()
		parser.inNote = false
	case parser.inNote:
	case parser.current != nil:
		parser.text = append(parser.text, line)
	case strings.HasPrefix(trimmed, "WEBVTT"), strings.HasPrefix(trimmed, "STYLE"),
		strings.HasPrefix(trimmed, "REGION"), strings.HasPrefix(trimmed, "NOTE"):
		parser.inNote = true
	case strings.Contains(trimmed, "-->"):
		start, end, err := parseTiming(trimmed)
		if err != nil {
			return err
		}
		parser.current = &Cue{StartMS: start, EndMS: end}
	default:
		// A cue identifier (an SRT index or VTT id): skip it.
	}
	return nil
}

// flush ends the current block, keeping its cue if it has text.
func (parser *parser) flush() {
	if parser.current != nil {
		parser.current.Text = strings.TrimSpace(strings.Join(parser.text, "\n"))
		if parser.current.Text != "" {
			parser.cues = append(parser.cues, *parser.current)
		}
	}
	parser.current, parser.text = nil, nil
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
func parseTimestamp(timestamp string) (int64, error) {
	timestamp = strings.Replace(timestamp, ",", ".", 1)
	var frac int64
	if i := strings.IndexByte(timestamp, '.'); i >= 0 {
		fraction := timestamp[i+1:]
		for len(fraction) < 3 {
			fraction += "0"
		}
		fractionMilliseconds, err := strconv.ParseInt(fraction[:3], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("bad timestamp %q", timestamp)
		}
		frac, timestamp = fractionMilliseconds, timestamp[:i]
	}
	fields := strings.Split(timestamp, ":")
	if len(fields) < 2 || len(fields) > 3 {
		return 0, fmt.Errorf("bad timestamp %q", timestamp)
	}
	var total int64
	for _, field := range fields {
		fieldValue, err := strconv.ParseInt(field, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("bad timestamp %q", timestamp)
		}
		total = total*60 + fieldValue
	}
	return total*1000 + frac, nil
}

// WriteVTT renders cues as a WebVTT document.
func WriteVTT(cues []Cue) []byte {
	var buffer bytes.Buffer
	buffer.WriteString("WEBVTT\n\n")
	for _, cue := range cues {
		fmt.Fprintf(&buffer, "%s --> %s\n%s\n\n", formatTimestamp(cue.StartMS), formatTimestamp(cue.EndMS), sanitizeCueText(cue.Text))
	}
	return buffer.Bytes()
}

func formatTimestamp(milliseconds int64) string {
	hours := milliseconds / 3600000
	minutes := milliseconds / 60000 % 60
	seconds := milliseconds / 1000 % 60
	return fmt.Sprintf("%02d:%02d:%02d.%03d", hours, minutes, seconds, milliseconds%1000)
}

// sanitizeCueText keeps a cue from breaking out of its block: "-->" and blank
// lines would otherwise start a new cue.
func sanitizeCueText(text string) string {
	text = strings.ReplaceAll(text, "-->", "->")
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// latin1ToUTF8 maps each byte to the code point of the same value.
func latin1ToUTF8(latin1 []byte) []byte {
	var out bytes.Buffer
	for _, character := range latin1 {
		out.WriteRune(rune(character))
	}
	return out.Bytes()
}
