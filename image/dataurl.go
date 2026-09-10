package image

import (
	"errors"
	"fmt"
	"strings"
)

// Errors of the data URL parser. Their messages match the Rust data-url crate,
// which the Rust resolver includes in an ImageError of kind KindInvalidDataURL.
var (
	// errNotADataURL reports input without the "data:" scheme.
	errNotADataURL = errors.New("not a valid data url")
	// errNoComma reports a data URL without a comma before its body.
	errNoComma = errors.New("data url is missing comma delimiting attributes and body")
)

// defaultDataURLMediaType is the media type of a data URL that carries none,
// matching the WHATWG default applied by the Rust data-url crate.
const defaultDataURLMediaType = "text/plain;charset=US-ASCII"

// dataURL is a parsed data: URL: its normalized media type, whether the body is
// base64-encoded, and the encoded body that may be followed by a fragment.
type dataURL struct {
	// mediaType is the normalized media type, such as "image/webp".
	mediaType string
	// base64 reports whether the body is base64-encoded.
	base64 bool
	// body is the encoded body followed by an optional fragment.
	body string
}

// processDataURL parses a data URL.
//
// It ports DataUrl::process of the Rust data-url crate, which follows the
// WHATWG Fetch data: URL processor: leading C0 controls and spaces and the
// scheme are matched case-insensitively, ASCII tabs and newlines are ignored
// while the scheme is read, and the URL is trimmed of trailing C0 controls and
// spaces. The body ends at the first '#'.
func processDataURL(input string) (*dataURL, error) {
	afterColon, ok := pretendParseDataURL(input)
	if !ok {
		return nil, errNotADataURL
	}
	header, body, ok := findCommaBeforeFragment(afterColon)
	if !ok {
		return nil, errNoComma
	}
	mediaType, base64 := parseDataURLHeader(header)
	return &dataURL{mediaType: mediaType, base64: base64, body: body}, nil
}

// pretendParseDataURL returns the part of input after the "data:" scheme.
//
// The second result is false when input is not a data URL.
func pretendParseDataURL(input string) (string, bool) {
	trimmed := strings.TrimLeftFunc(input, func(r rune) bool { return r <= ' ' })
	const scheme = "data:"
	index := 0
	matched := 0
	for index < len(trimmed) && matched < len(scheme) {
		byteValue := trimmed[index]
		index++
		// ASCII tabs and newlines are ignored while the scheme is read.
		if byteValue == '\t' || byteValue == '\n' || byteValue == '\r' {
			continue
		}
		if !asciiEqualFold(byteValue, scheme[matched]) {
			return "", false
		}
		matched++
	}
	if matched < len(scheme) {
		return "", false
	}
	return strings.TrimRightFunc(trimmed[index:], func(r rune) bool { return r <= ' ' }), true
}

// findCommaBeforeFragment splits the text after the scheme at the first comma
// that precedes any fragment. It reports false when a fragment starts first.
func findCommaBeforeFragment(afterColon string) (string, string, bool) {
	for index := 0; index < len(afterColon); index++ {
		switch afterColon[index] {
		case ',':
			return afterColon[:index], afterColon[index+1:], true
		case '#':
			return "", "", false
		}
	}
	return "", "", false
}

// parseDataURLHeader returns the normalized media type and whether the body is
// base64-encoded.
func parseDataURLHeader(header string) (string, bool) {
	trimmed := strings.Trim(header, " \t\n\r")
	mimeType := trimmed
	base64 := false
	if withoutSuffix, ok := removeBase64Suffix(trimmed); ok {
		mimeType = withoutSuffix
		base64 = true
	}
	return parseDataURLMIME(mimeType), base64
}

// removeBase64Suffix strips a trailing ";base64" marker.
//
// The marker is matched like the Rust data-url crate: ASCII tabs and newlines
// are ignored, the letters are matched case-insensitively, and spaces may
// separate the semicolon from the marker.
func removeBase64Suffix(value string) (string, bool) {
	const reversed = "46esab" // "base64", compared from the end.
	position := len(value)
	for index := 0; index < len(reversed); index++ {
		found := previousSignificantByte(value, position)
		if found < 0 || !asciiEqualFold(value[found], reversed[index]) {
			return "", false
		}
		position = found
	}
	for {
		found := previousSignificantByte(value, position)
		if found < 0 {
			return "", false
		}
		switch value[found] {
		case ' ':
			position = found
		case ';':
			return value[:found], true
		default:
			return "", false
		}
	}
}

// previousSignificantByte returns the index of the last byte before position
// that is not an ASCII tab or newline, or -1 when there is none.
func previousSignificantByte(value string, position int) int {
	for index := position - 1; index >= 0; index-- {
		switch value[index] {
		case '\t', '\n', '\r':
			continue
		default:
			return index
		}
	}
	return -1
}

// parseDataURLMIME normalizes the media type of a data URL header.
//
// It ports parse_header of the Rust data-url crate: ASCII tabs and newlines are
// removed, C0 controls and bytes of at least 0x7F are percent-encoded, a header
// that starts with ';' is read as a parameter of "text/plain", and a header the
// parser cannot read falls back to "text/plain;charset=US-ASCII".
func parseDataURLMIME(mimeType string) string {
	var builder strings.Builder
	if strings.HasPrefix(mimeType, ";") {
		builder.WriteString("text/plain")
	}
	inQuery := false
	for index := 0; index < len(mimeType); index++ {
		byteValue := mimeType[index]
		switch {
		case byteValue == '\t' || byteValue == '\n' || byteValue == '\r':
			continue
		case byteValue <= 0x1f || byteValue >= 0x7f:
			percentEncode(byteValue, &builder)
		case inQuery && (byteValue == ' ' || byteValue == '"' || byteValue == '<' || byteValue == '>'):
			percentEncode(byteValue, &builder)
		case byteValue == '?':
			inQuery = true
			builder.WriteByte('?')
		default:
			builder.WriteByte(byteValue)
		}
	}
	return parseMIMEType(builder.String())
}

// percentEncode writes the percent-encoded form of one byte.
func percentEncode(byteValue byte, builder *strings.Builder) {
	const hexUpper = "0123456789ABCDEF"
	builder.WriteByte('%')
	builder.WriteByte(hexUpper[byteValue>>4])
	builder.WriteByte(hexUpper[byteValue&0x0f])
}

// parseMIMEType normalizes a media type string.
//
// It is a simplification of the mime crate the Rust data-url crate uses: the
// type and subtype are lowercased and validated, parameters are validated and
// lowercased, and an unreadable value falls back to
// "text/plain;charset=US-ASCII". Only the type and subtype of the result are
// significant to this package, because the media type check ignores parameters.
func parseMIMEType(raw string) string {
	if raw == "" {
		return defaultDataURLMediaType
	}
	essence, parameters, hasParameters := strings.Cut(raw, ";")
	typeName, subtype, found := strings.Cut(essence, "/")
	if !found || !isMIMEToken(typeName) || !isMIMEToken(subtype) {
		return defaultDataURLMediaType
	}
	normalized := strings.ToLower(typeName) + "/" + strings.ToLower(subtype)
	if !hasParameters {
		return normalized
	}
	parsed, ok := parseMIMEParameters(parameters)
	if !ok {
		return defaultDataURLMediaType
	}
	return normalized + parsed
}

// parseMIMEParameters normalizes the parameters of a media type. It reports
// false when a parameter is malformed.
func parseMIMEParameters(parameters string) (string, bool) {
	var builder strings.Builder
	for _, parameter := range strings.Split(parameters, ";") {
		name, value, found := strings.Cut(parameter, "=")
		name = strings.Trim(name, " \t")
		if !found || !isMIMEToken(name) {
			return "", false
		}
		normalized, ok := parseMIMEValue(value)
		if !ok {
			return "", false
		}
		builder.WriteByte(';')
		builder.WriteString(strings.ToLower(name))
		builder.WriteByte('=')
		builder.WriteString(normalized)
	}
	return builder.String(), true
}

// parseMIMEValue normalizes one parameter value, which is either a token or a
// quoted string.
func parseMIMEValue(value string) (string, bool) {
	value = strings.Trim(value, " \t")
	if len(value) >= 2 && value[0] == '"' {
		if value[len(value)-1] != '"' {
			return "", false
		}
		var builder strings.Builder
		escaped := false
		for index := 1; index < len(value)-1; index++ {
			byteValue := value[index]
			if escaped {
				builder.WriteByte(byteValue)
				escaped = false
				continue
			}
			switch byteValue {
			case '\\':
				escaped = true
			default:
				builder.WriteByte(byteValue)
			}
		}
		if escaped {
			return "", false
		}
		return builder.String(), true
	}
	if !isMIMEToken(value) {
		return "", false
	}
	return value, true
}

// isMIMEToken reports whether value is a non-empty HTTP token, the character
// class the mime crate accepts for type, subtype, and parameter names.
func isMIMEToken(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		if !isMIMETokenByte(value[index]) {
			return false
		}
	}
	return true
}

// isMIMETokenByte reports whether byteValue is an HTTP token character. The
// token characters are the alphanumerics and "!#$%&'*+-.^_|~" plus the
// backtick (0x60).
func isMIMETokenByte(byteValue byte) bool {
	switch {
	case byteValue >= '0' && byteValue <= '9':
		return true
	case byteValue >= 'a' && byteValue <= 'z':
		return true
	case byteValue >= 'A' && byteValue <= 'Z':
		return true
	}
	switch byteValue {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', 0x60, '|', '~':
		return true
	}
	return false
}

// asciiEqualFold reports whether the ASCII byteValue equals the ASCII byte want,
// ignoring case.
func asciiEqualFold(byteValue, want byte) bool {
	if byteValue == want {
		return true
	}
	if byteValue >= 'A' && byteValue <= 'Z' {
		byteValue += 'a' - 'A'
	}
	if want >= 'A' && want <= 'Z' {
		want += 'a' - 'A'
	}
	return byteValue == want
}

// decode returns the decoded body of the data URL.
//
// The encoded body is percent-decoded and ends at the first '#'; a base64 body
// is then decoded with the forgiving algorithm of the Rust data-url crate.
func (u *dataURL) decode() ([]byte, error) {
	if u.base64 {
		decoder := &forgivingBase64Decoder{}
		if _, _, err := decodePercentEncoded(u.body, decoder.feed); err != nil {
			return nil, err
		}
		if err := decoder.finish(); err != nil {
			return nil, err
		}
		return decoder.decoded, nil
	}
	var body []byte
	if _, _, err := decodePercentEncoded(u.body, func(chunk []byte) error {
		body = append(body, chunk...)
		return nil
	}); err != nil {
		return nil, err
	}
	return body, nil
}

// decodePercentEncoded percent-decodes a data URL body and passes the decoded
// bytes to write. It stops at the first '#' and returns the fragment after it.
//
// It ports decode_without_base64 of the Rust data-url crate: ASCII tabs and
// newlines are ignored, an incomplete percent escape is left as it is, and the
// fragment is not decoded.
func decodePercentEncoded(encoded string, write func([]byte) error) (string, bool, error) {
	bytes := []byte(encoded)
	sliceStart := 0
	for index := 0; index < len(bytes); index++ {
		byteValue := bytes[index]
		if byteValue != '%' && byteValue != '#' && byteValue != '\t' && byteValue != '\n' && byteValue != '\r' {
			continue
		}
		if index > sliceStart {
			if err := write(bytes[sliceStart:index]); err != nil {
				return "", false, err
			}
			sliceStart = index
		}
		switch byteValue {
		case '%':
			high, highOK := hexDigitValue(bytes, index+1)
			low, lowOK := hexDigitValue(bytes, index+2)
			if highOK && lowOK {
				if err := write([]byte{high<<4 | low}); err != nil {
					return "", false, err
				}
				sliceStart = index + 3
			}
			// An incomplete escape is part of the next slice.
		case '#':
			return string(bytes[index+1:]), true, nil
		default:
			// An ignored tab or newline.
			sliceStart = index + 1
		}
	}
	if sliceStart < len(bytes) {
		if err := write(bytes[sliceStart:]); err != nil {
			return "", false, err
		}
	}
	return "", false, nil
}

// hexDigitValue returns the value of the ASCII hex digit at index.
func hexDigitValue(bytes []byte, index int) (byte, bool) {
	if index >= len(bytes) {
		return 0, false
	}
	byteValue := bytes[index]
	switch {
	case byteValue >= '0' && byteValue <= '9':
		return byteValue - '0', true
	case byteValue >= 'a' && byteValue <= 'f':
		return byteValue - 'a' + 10, true
	case byteValue >= 'A' && byteValue <= 'F':
		return byteValue - 'A' + 10, true
	}
	return 0, false
}

// base64DecodeTable maps a byte to its position in the base64 alphabet, or -1
// for a byte that is not part of it.
var base64DecodeTable = func() [256]int8 {
	var table [256]int8
	for index := range table {
		table[index] = -1
	}
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	for index := 0; index < len(alphabet); index++ {
		table[alphabet[index]] = int8(index)
	}
	return table
}()

// forgivingBase64Decoder ports forgiving_base64::Decoder of the Rust data-url
// crate, which implements the WHATWG forgiving-base64 algorithm: ASCII
// whitespace is ignored, padding may be omitted, and an alphabet symbol after
// padding is an error.
type forgivingBase64Decoder struct {
	// decoded holds the bytes produced so far.
	decoded []byte
	// bitBuffer accumulates the six-bit symbols that are not yet a byte.
	bitBuffer uint32
	// bufferBitLength is the number of valid bits in bitBuffer.
	bufferBitLength uint8
	// paddingSymbols counts the '=' symbols read so far.
	paddingSymbols uint8
}

// feed decodes a chunk of the encoded body.
func (d *forgivingBase64Decoder) feed(input []byte) error {
	for _, byteValue := range input {
		value := base64DecodeTable[byteValue]
		if value < 0 {
			// A character that is not part of the alphabet.
			if byteValue == ' ' || byteValue == '\t' || byteValue == '\n' || byteValue == '\r' || byteValue == '\x0c' {
				continue
			}
			if byteValue == '=' {
				if d.paddingSymbols < 255 {
					d.paddingSymbols++
				}
				continue
			}
			return fmt.Errorf("symbol with codepoint %d not expected", byteValue)
		}
		if d.paddingSymbols > 0 {
			return errors.New("alphabet symbol present after padding")
		}
		d.bitBuffer <<= 6
		d.bitBuffer |= uint32(value)
		if d.bufferBitLength < 18 {
			d.bufferBitLength += 6
			continue
		}
		// Four six-bit symbols equal three bytes.
		d.decoded = append(d.decoded,
			byte(d.bitBuffer>>16), byte(d.bitBuffer>>8), byte(d.bitBuffer))
		d.bufferBitLength = 0
	}
	return nil
}

// finish completes the decoding and reports truncated or excessive padding.
func (d *forgivingBase64Decoder) finish() error {
	switch {
	case d.bufferBitLength == 0 && d.paddingSymbols == 0:
		// A multiple of four alphabet symbols and nothing else.
		return nil
	case d.bufferBitLength == 12 && (d.paddingSymbols == 2 || d.paddingSymbols == 0):
		d.decoded = append(d.decoded, byte(d.bitBuffer>>4))
		return nil
	case d.bufferBitLength == 18 && (d.paddingSymbols == 1 || d.paddingSymbols == 0):
		d.decoded = append(d.decoded, byte(d.bitBuffer>>10), byte(d.bitBuffer>>2))
		return nil
	case d.bufferBitLength == 6:
		return errors.New("lone alphabet symbol present")
	default:
		return errors.New("incorrect padding")
	}
}

// estimatedDecodedLen returns the decoded size a data URL body reports before it
// is decoded, or zero when the URL has no comma.
//
// A base64 body occupies four characters for every three bytes it carries, so
// the result can be up to two bytes smaller than the decoded body. A
// percent-encoded byte occupies three characters.
func estimatedDecodedLen(dataURL string) int {
	header, body, found := strings.Cut(dataURL, ",")
	if !found {
		return 0
	}
	if strings.HasSuffix(header, ";base64") {
		return len(body) / 4 * 3
	}
	estimated := len(body) - 2*strings.Count(body, "%")
	if estimated < 0 {
		return 0
	}
	return estimated
}
