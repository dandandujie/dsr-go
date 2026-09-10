package tokenizer

import (
	"strings"
	"unicode/utf8"
)

// The byte-level alphabet (GPT-2's bytes_to_unicode): every byte is mapped to a
// printable Unicode character so that arbitrary byte sequences survive the BPE
// vocabulary. Bytes 0x21..0x7E, 0xA1..0xAC and 0xAE..0xFF map to themselves;
// the remaining 68 bytes map to U+0100..U+0143 in increasing byte order.
//
// The largest mapped rune is 0x143, which allows the reverse lookup to be a
// plain array.
const byteLevelMaxRune = 0x143

var (
	// byteLevelRune maps a byte to its byte-level character.
	byteLevelRune [256]rune
	// byteLevelUTF8 holds the UTF-8 encoding of byteLevelRune and its length.
	byteLevelUTF8    [256][utf8.UTFMax]byte
	byteLevelUTF8Len [256]int
	// byteLevelByte is the reverse map: a byte-level character's code point to
	// the byte it represents, or -1 when the rune is not part of the alphabet.
	byteLevelByte [byteLevelMaxRune + 1]int16
)

func init() {
	for i := range byteLevelByte {
		byteLevelByte[i] = -1
	}
	var mapped [256]bool
	for b := 0x21; b <= 0x7E; b++ {
		mapped[b] = true
	}
	for b := 0xA1; b <= 0xAC; b++ {
		mapped[b] = true
	}
	for b := 0xAE; b <= 0xFF; b++ {
		mapped[b] = true
	}
	next := rune(0x100)
	for b := 0; b < 256; b++ {
		r := rune(b)
		if !mapped[b] {
			r = next
			next++
		}
		byteLevelRune[b] = r
		byteLevelByte[r] = int16(b)
		n := utf8.EncodeRune(byteLevelUTF8[b][:], r)
		byteLevelUTF8Len[b] = n
	}
}

// byteLevelEncode rewrites every byte of s as its byte-level character. The
// result is a valid UTF-8 string that is looked up in the BPE vocabulary.
func byteLevelEncode(s string) string {
	// Printable ASCII maps to itself; this covers most source text.
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7E {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}
	out := make([]byte, 0, len(s)*2)
	for i := 0; i < len(s); i++ {
		b := s[i]
		out = append(out, byteLevelUTF8[b][:byteLevelUTF8Len[b]]...)
	}
	return string(out)
}

// byteLevelDecodeToken appends the bytes represented by the byte-level token
// tok to dst and returns the extended slice.
//
// Mirroring the Rust decoder, a token that contains any character outside the
// byte-level alphabet is emitted verbatim as its own UTF-8 bytes instead of
// being partially translated; this is what makes added tokens such as
// "<|begin_of_sentence|>" decode back to their literal text.
func byteLevelDecodeToken(dst []byte, tok string) []byte {
	start := len(dst)
	for _, r := range tok {
		if r <= byteLevelMaxRune {
			if b := byteLevelByte[r]; b >= 0 {
				dst = append(dst, byte(b))
				continue
			}
		}
		return append(dst[:start], tok...)
	}
	return dst
}

// rustLossyUTF8 converts b to a string the way Rust's String::from_utf8_lossy
// does: each maximal invalid subpart is replaced by one U+FFFD. This differs
// from strings.ToValidUTF8, which collapses a whole run of invalid bytes into a
// single replacement character, and from a naive per-byte replacement.
func rustLossyUTF8(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var sb strings.Builder
	sb.Grow(len(b))
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r != utf8.RuneError || size > 1 {
			sb.Write(b[:size])
			b = b[size:]
			continue
		}
		// b[0] starts an invalid sequence: compute the length of the maximal
		// subpart exactly like Rust's UTF-8 validation loop.
		errLen, incomplete := rustErrorLen(b)
		if incomplete {
			sb.WriteRune(utf8.RuneError)
			break
		}
		sb.WriteRune(utf8.RuneError)
		b = b[errLen:]
	}
	return sb.String()
}

// rustErrorLen returns the length of the maximal invalid subpart at the start of
// b, or incomplete=true when b is a truncated-but-so-far-valid prefix (Rust's
// Utf8Error::error_len() == None).
func rustErrorLen(b []byte) (int, bool) {
	const (
		contMin = 0x80
		contMax = 0xBF
	)
	isCont := func(c byte) bool { return c >= contMin && c <= contMax }
	first := b[0]
	switch {
	case first < 0xE0: // 2-byte sequence (first >= 0x80 here)
		if first < 0xC2 {
			return 1, false
		}
		if len(b) < 2 {
			return 0, true
		}
		if !isCont(b[1]) {
			return 1, false
		}
		return 2, false
	case first < 0xF0: // 3-byte sequence
		if len(b) < 2 {
			return 0, true
		}
		second := b[1]
		if !isCont(second) || (first == 0xE0 && second < 0xA0) || (first == 0xED && second >= 0xA0) {
			return 1, false
		}
		if len(b) < 3 {
			return 0, true
		}
		if !isCont(b[2]) {
			return 2, false
		}
		return 3, false
	default: // 4-byte sequence
		if first > 0xF4 {
			return 1, false
		}
		if len(b) < 2 {
			return 0, true
		}
		second := b[1]
		if !isCont(second) || (first == 0xF0 && second < 0x90) || (first == 0xF4 && second >= 0x90) {
			return 1, false
		}
		if len(b) < 3 {
			return 0, true
		}
		if !isCont(b[2]) {
			return 2, false
		}
		if len(b) < 4 {
			return 0, true
		}
		if !isCont(b[3]) {
			return 3, false
		}
		return 4, false // unreachable for invalid input
	}
}
