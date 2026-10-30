package subtitle

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// [Parse] reads a subtitle file in SRT or WebVTT format into its cues. The
// format is detected from the content, not the file name. A leading UTF-8
// byte order mark is skipped, Windows and old Mac line endings are accepted,
// and input that is not valid UTF-8 is decoded as Latin-1, a best effort for
// the legacy encodings older subtitle files often use. Cues with no text are
// dropped.
//
// Parameters:
//   - [data]: the file's contents.
//
// Returns:
//   - [[]Cue]: the cues in file order, or [nil] if [error] is not [nil].
//   - [error]: [nil] on success; [ErrNoCues] if nothing in [data] is a cue
//     with text; an error quoting the bad text for a malformed timing line;
//     or [bufio.ErrTooLong] for a line longer than 1 MiB.
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

// [parser] reads SRT and WebVTT a line at a time, collecting cues. Both
// formats are blocks separated by blank lines; a cue block is an optional
// identifier, a timing line and the text. Feed it every line with
// [parser.line], then call [parser.flush] once to keep the last cue.
type parser struct {
	// [cues] holds the finished cues in file order.
	cues []Cue
	// [current] is the cue being read once its timing line has been seen, or
	// [nil] between cues.
	current *Cue
	// [text] holds [current]'s text lines read so far.
	text []string
	// [inNote] is [true] inside a WebVTT header, NOTE, STYLE or REGION block,
	// whose lines are skipped until the next blank line.
	inNote bool
}

// [line] reads the next line of the file. A blank line ends the current
// block; a line containing "-->" starts a cue; other lines are cue text,
// cue identifiers (skipped) or WebVTT metadata (skipped).
//
// Parameters:
//   - [line]: one line, without its line break.
//
// Returns:
//   - [error]: [nil] on success, or an error quoting the bad text if a
//     timing line is malformed.
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

// [flush] ends the current block: if a cue was being read and has text, it
// is added to [parser.cues]. Calling it between blocks does nothing.
func (parser *parser) flush() {
	if parser.current != nil {
		parser.current.Text = strings.TrimSpace(strings.Join(parser.text, "\n"))
		if parser.current.Text != "" {
			parser.cues = append(parser.cues, *parser.current)
		}
	}
	parser.current, parser.text = nil, nil
}

// [parseTiming] reads a cue's timing line. Settings after the end time,
// such as "align:start", are ignored.
//
// Parameters:
//   - [line]: a line such as "00:00:01,000 --> 00:00:04,250". It must
//     contain "-->"; [parseTiming] panics otherwise.
//
// Returns:
//   - [int64] (start): the start time in milliseconds, or 0 if [error] is
//     not [nil].
//   - [int64] (end): the end time in milliseconds, or 0 if [error] is not
//     [nil].
//   - [error]: [nil] on success, or an error quoting the bad text if either
//     time is missing or malformed.
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

// [parseTimestamp] reads one cue time in hh:mm:ss.mmm or mm:ss.mmm form, with
// a comma accepted instead of the dot, as SRT writes it. Fractions shorter
// than three digits are padded ("1.5" is 1500 ms) and longer ones are cut to
// three.
//
// Parameters:
//   - [timestamp]: the time, such as "00:01:05,5".
//
// Returns:
//   - [int64]: the time in milliseconds, or 0 if [error] is not [nil].
//   - [error]: [nil] on success, or an error quoting the timestamp if it has
//     fewer than two or more than three colon-separated parts or a part that
//     is not a number. Once the fraction has been read, the quoted text no
//     longer includes it.
func parseTimestamp(timestamp string) (int64, error) {
	timestamp = strings.Replace(timestamp, ",", ".", 1)
	var fractionMilliseconds int64
	if i := strings.IndexByte(timestamp, '.'); i >= 0 {
		fractionDigits := timestamp[i+1:]
		for len(fractionDigits) < 3 {
			fractionDigits += "0"
		}
		parsed, err := strconv.ParseInt(fractionDigits[:3], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("bad timestamp %q", timestamp)
		}
		fractionMilliseconds, timestamp = parsed, timestamp[:i]
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
	return total*1000 + fractionMilliseconds, nil
}

// [WriteVTT] renders cues as a WebVTT document, the format browsers' <track>
// element plays. Each cue's text is cleaned with [sanitizeCueText] so it cannot
// break the document's structure.
//
// Parameters:
//   - [cues]: the cues to write, in the order they should appear.
//
// Returns:
//   - [[]byte]: the document, starting with "WEBVTT". For no cues, just the
//     header.
func WriteVTT(cues []Cue) []byte {
	var buffer bytes.Buffer
	buffer.WriteString("WEBVTT\n\n")
	for _, cue := range cues {
		fmt.Fprintf(&buffer, "%s --> %s\n%s\n\n", formatTimestamp(cue.StartMS), formatTimestamp(cue.EndMS), sanitizeCueText(cue.Text))
	}
	return buffer.Bytes()
}

// [formatTimestamp] writes a cue time the way WebVTT expects it.
//
// Parameters:
//   - [milliseconds]: the time in milliseconds from the start of the video.
//     It must not be negative.
//
// Returns:
//   - [string]: the time as hh:mm:ss.mmm, such as "00:01:05.500". Hours are
//     not limited to two digits.
func formatTimestamp(milliseconds int64) string {
	hours := milliseconds / 3600000
	minutes := milliseconds / 60000 % 60
	seconds := milliseconds / 1000 % 60
	return fmt.Sprintf("%02d:%02d:%02d.%03d", hours, minutes, seconds, milliseconds%1000)
}

// [sanitizeCueText] keeps a cue's text from breaking out of its WebVTT block:
// "-->" would start a new timing line and a blank line would end the cue.
//
// Parameters:
//   - [text]: the cue's text, with lines separated by "\n".
//
// Returns:
//   - [string]: [text] with every "-->" replaced by "->" and every blank or
//     whitespace-only line removed.
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

// [latin1ToUTF8] decodes text in Latin-1 (ISO 8859-1), where each byte is one
// character, into UTF-8.
//
// Parameters:
//   - [latin1]: the Latin-1 bytes.
//
// Returns:
//   - [[]byte]: the same text as UTF-8, where each byte became the code point
//     of the same value.
func latin1ToUTF8(latin1 []byte) []byte {
	var out bytes.Buffer
	for _, character := range latin1 {
		out.WriteRune(rune(character))
	}
	return out.Bytes()
}
