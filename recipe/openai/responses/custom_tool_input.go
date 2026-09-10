package responses

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"
)

// customToolInputParserState identifies the parser position.
type customToolInputParserState uint8

// Parser states.
const (
	// customToolInputPrefix matches the leading {"input":" prefix.
	customToolInputPrefix customToolInputParserState = iota
	// customToolInputValue decodes the JSON string value.
	customToolInputValue
	// customToolInputPassthrough copies nonmatching arguments unchanged.
	customToolInputPassthrough
	// customToolInputDone ignores all further input.
	customToolInputDone
)

// unicodeEscapePrefix is the two-byte prefix of a JSON unicode escape.
const unicodeEscapePrefix = "\\" + "u"

// customToolInputPrefixParts is the leading prefix of a custom tool input
// object.
var customToolInputPrefixParts = []string{"{", `"input"`, ":", `"`}

// customToolInputParser decodes the leading JSON input string of a custom tool
// call, preserving nonmatching arguments. Content after the closing quote is
// ignored.
type customToolInputParser struct {
	state        customToolInputParserState
	prefix       string
	prefixPart   int
	prefixOffset int
	escape       string
}

// feed decodes available value text, retaining incomplete prefixes and
// escapes.
func (p *customToolInputParser) feed(chunk string) string {
	var output strings.Builder
	for _, ch := range chunk {
		switch p.state {
		case customToolInputPrefix:
			p.feedPrefix(ch, &output)
		case customToolInputValue:
			p.feedValue(ch, &output)
		case customToolInputPassthrough:
			output.WriteRune(ch)
		default:
			return output.String()
		}
	}
	return output.String()
}

// finish ends parsing and returns any unfinished prefix or escape unchanged.
//
// Repeated calls and subsequent input produce no further output.
func (p *customToolInputParser) finish() string {
	p.state = customToolInputDone
	output := p.prefix + p.escape
	p.prefix = ""
	p.escape = ""
	return output
}

// feedPrefix matches one prefix character.
func (p *customToolInputParser) feedPrefix(ch rune, output *strings.Builder) {
	p.prefix += string(ch)
	part := customToolInputPrefixParts[p.prefixPart]
	if ch == rune(part[p.prefixOffset]) {
		p.prefixOffset++
		if p.prefixOffset == len(part) {
			p.prefixPart++
			p.prefixOffset = 0
			if p.prefixPart == len(customToolInputPrefixParts) {
				p.prefix = ""
				p.state = customToolInputValue
			}
		}
	} else if p.prefixOffset != 0 || !isASCIIWhitespace(ch) {
		output.WriteString(p.prefix)
		p.prefix = ""
		p.state = customToolInputPassthrough
	}
}

// feedValue decodes one character of the JSON string value.
func (p *customToolInputParser) feedValue(ch rune, output *strings.Builder) {
	if p.escape == "" {
		switch ch {
		case '\\':
			p.escape = "\\"
		case '"':
			p.state = customToolInputDone
		default:
			output.WriteRune(ch)
		}
		return
	}
	if ch == '"' && !strings.HasSuffix(p.escape, "\\") {
		output.WriteString(p.escape)
		p.escape = ""
		p.state = customToolInputDone
		return
	}
	p.escape += string(ch)
	if incompleteEscape(p.escape) {
		return
	}
	raw := p.escape
	p.escape = ""
	var decoded string
	if err := json.Unmarshal([]byte(`"`+raw+`"`), &decoded); err == nil {
		output.WriteString(decoded)
	} else {
		output.WriteString(raw)
	}
}

// incompleteEscape reports whether a pending escape sequence may still be
// completed by later input.
func incompleteEscape(raw string) bool {
	if len(raw) < 6 {
		return partialUnicodeEscape(raw)
	}
	// Validate the first ASCII escape before inspecting a surrogate pair.
	// Invalid escapes can contain multibyte characters at any position.
	if len(raw) > 6 && !utf8.RuneStart(raw[6]) {
		return false
	}
	first := raw[:6]
	if !partialUnicodeEscape(first) {
		return false
	}
	value, err := strconv.ParseUint(first[2:], 16, 16)
	highSurrogate := err == nil && value >= 0xD800 && value <= 0xDBFF
	return highSurrogate && len(raw) < 12 && partialUnicodeEscape(raw[6:])
}

// partialUnicodeEscape reports whether raw is an incomplete or complete
// unicode escape prefix.
func partialUnicodeEscape(raw string) bool {
	if len(raw) > 6 {
		return false
	}
	if strings.HasPrefix(unicodeEscapePrefix, raw) {
		return true
	}
	digits, ok := strings.CutPrefix(raw, unicodeEscapePrefix)
	if !ok {
		return false
	}
	for _, ch := range digits {
		if !isASCIIHexDigit(ch) {
			return false
		}
	}
	return true
}

// isASCIIWhitespace reports whether ch is ASCII whitespace.
func isASCIIWhitespace(ch rune) bool {
	switch ch {
	case ' ', '\t', '\n', '\f', '\r':
		return true
	}
	return false
}

// isASCIIHexDigit reports whether ch is an ASCII hexadecimal digit.
func isASCIIHexDigit(ch rune) bool {
	return ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f' || ch >= 'A' && ch <= 'F'
}
