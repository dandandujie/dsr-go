package tokenizer

import (
	"unicode"
	"unicode/utf8"
)

// This file implements the three Split pre-tokenizers of the pipeline. The
// first two are simple enough to scan by hand; the third one contains a
// negative-lookahead alternative, which Go's RE2-based regexp package cannot
// express, so it is implemented here with Perl-style backtracking semantics.
//
// All three use the "Isolated" behaviour: every match of the pattern becomes
// its own split while the text between matches is kept as separate splits.

// asciiPunct reports whether b is one of the ASCII punctuation characters of
// the first alternative of the word pattern:
//
//	[!"#$%&'()*+,\-./:;<=>?@\[\\\]^_`{|}~]
var asciiPunct = func() (t [128]bool) {
	for _, b := range []byte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~") {
		t[b] = true
	}
	return
}()

// splitDigits implements Split("\p{N}{1,3}", Isolated): every run of Unicode
// number characters is cut into chunks of at most three characters and each
// chunk becomes its own split (a run of ten digits yields 3+3+3+1).
func splitDigits(s string) []string {
	var out []string
	prev := 0
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !unicode.IsNumber(r) {
			i += size
			continue
		}
		if prev < i {
			out = append(out, s[prev:i])
		}
		// The quantifier is greedy: {1,3}.
		start := i
		n := 0
		for i < len(s) && n < 3 {
			r, size = utf8.DecodeRuneInString(s[i:])
			if !unicode.IsNumber(r) {
				break
			}
			i += size
			n++
		}
		out = append(out, s[start:i])
		prev = i
	}
	if prev < len(s) {
		out = append(out, s[prev:])
	}
	return out
}

// splitCJK implements Split("[\u4e00-\u9fa5\u3040-\u309f\u30a0-\u30ff]+",
// Isolated): every maximal run of CJK/Kana characters becomes its own split.
func splitCJK(s string) []string {
	var out []string
	prev := 0
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !isCJK(r) {
			i += size
			continue
		}
		if prev < i {
			out = append(out, s[prev:i])
		}
		start := i
		for i < len(s) {
			r, size = utf8.DecodeRuneInString(s[i:])
			if !isCJK(r) {
				break
			}
			i += size
		}
		out = append(out, s[start:i])
		prev = i
	}
	if prev < len(s) {
		out = append(out, s[prev:])
	}
	return out
}

// isCJK reports whether r is in one of the ranges of the CJK split pattern.
func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FA5) || (r >= 0x3040 && r <= 0x309F) || (r >= 0x30A0 && r <= 0x30FF)
}

// splitWord implements the third Split pre-tokenizer, whose pattern is
//
//	[!"#$%&'()*+,\-./:;<=>?@\[\\\]^_`{|}~][A-Za-z]+
//	|[^\r\n\p{L}\p{P}\p{S}]?[\p{L}\p{M}]+
//	| ?[\p{P}\p{S}]+[\r\n]*
//	|\s*[\r\n]+
//	|\s+(?!\S)
//	|\s+
//
// with Perl (leftmost-first) semantics: the scan looks for the earliest
// position at which any alternative matches and then takes the first
// alternative, in pattern order, that matches there.
//
// The "\s+(?!\S)" alternative is handled by hand: a run of whitespace is
// matched in full when it reaches the end of the text, otherwise the match is
// the run minus its last character, and the alternative fails for a
// single-character run (in which case the final "\s+" alternative takes over).
func splitWord(s string) []string {
	var out []string
	prev := 0
	i := 0
	for i < len(s) {
		n := matchWordPiece(s, i)
		if n > 0 {
			if prev < i {
				out = append(out, s[prev:i])
			}
			out = append(out, s[i:i+n])
			i += n
			prev = i
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	if prev < len(s) {
		out = append(out, s[prev:])
	}
	return out
}

// matchWordPiece returns the byte length of the first alternative of the word
// pattern that matches at byte offset i, or 0 when no alternative matches.
func matchWordPiece(s string, i int) int {
	r, size := utf8.DecodeRuneInString(s[i:])

	// Alternative 1: [!..~][A-Za-z]+
	if r < utf8.RuneSelf && asciiPunct[byte(r)] {
		j := i + size
		for j < len(s) && isASCIILetter(s[j]) {
			j++
		}
		if j > i+size {
			return j - i
		}
	}

	// Alternative 2: [^\r\n\p{L}\p{P}\p{S}]?[\p{L}\p{M}]+
	if isLetterOrMark(r) {
		return runOf(s, i, isLetterOrMark) - i
	}
	if r != '\r' && r != '\n' && !unicode.IsPunct(r) && !unicode.IsSymbol(r) {
		// The optional character can only be consumed when the character
		// after it starts a letter/mark run; otherwise the alternative fails,
		// because backtracking to an empty optional character would require
		// the current character itself to be a letter or mark.
		if j := runOf(s, i+size, isLetterOrMark); j > i+size {
			return j - i
		}
	}

	// Alternative 3:  ?[\p{P}\p{S}]+[\r\n]*
	if r == ' ' {
		if r2, size2 := utf8.DecodeRuneInString(s[i+size:]); isPunctOrSymbol(r2) {
			j := runOf(s, i+size+size2, isPunctOrSymbol)
			j += runOfCRLF(s, j)
			return j - i
		}
	} else if isPunctOrSymbol(r) {
		j := runOf(s, i+size, isPunctOrSymbol)
		j += runOfCRLF(s, j)
		return j - i
	}

	// Alternative 4: \s*[\r\n]+
	//
	// With backtracking, \s* greedily consumes the whole whitespace run and
	// then gives characters back until a CR/LF is found, so the match ends at
	// the end of the CR/LF run that starts at the last CR/LF of the whitespace
	// run.
	if unicode.IsSpace(r) {
		j := i
		lastCRLF := -1
		for j < len(s) {
			r2, size2 := utf8.DecodeRuneInString(s[j:])
			if !unicode.IsSpace(r2) {
				break
			}
			if r2 == '\r' || r2 == '\n' {
				lastCRLF = j
			}
			j += size2
		}
		if lastCRLF >= 0 {
			return lastCRLF + runOfCRLF(s, lastCRLF) - i
		}
	}

	// Alternative 5: \s+(?!\S)
	if unicode.IsSpace(r) {
		j := i
		lastSpace := i
		for j < len(s) {
			r2, size2 := utf8.DecodeRuneInString(s[j:])
			if !unicode.IsSpace(r2) {
				break
			}
			lastSpace = j
			j += size2
		}
		if j == len(s) {
			return j - i // the run reaches the end of the text
		}
		if lastSpace > i {
			return lastSpace - i // one character shorter: the lookahead succeeds
		}
		// A single whitespace character followed by a non-space character:
		// (?!\S) fails, so fall through to the final "\s+" alternative.
	}

	// Alternative 6: \s+
	if unicode.IsSpace(r) {
		return runOf(s, i, unicode.IsSpace) - i
	}
	return 0
}

// runOf returns the byte offset just past the longest run of characters
// satisfying pred that starts at i.
func runOf(s string, i int, pred func(rune) bool) int {
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !pred(r) {
			break
		}
		i += size
	}
	return i
}

// runOfCRLF returns the length of the run of CR and LF characters at i.
func runOfCRLF(s string, i int) int {
	start := i
	for i < len(s) && (s[i] == '\r' || s[i] == '\n') {
		i++
	}
	return i - start
}

// isASCIILetter reports whether b is one of [A-Za-z].
func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// isLetterOrMark reports whether r is in \p{L} or \p{M}.
func isLetterOrMark(r rune) bool { return unicode.IsLetter(r) || unicode.IsMark(r) }

// isPunctOrSymbol reports whether r is in \p{P} or \p{S}.
func isPunctOrSymbol(r rune) bool { return unicode.IsPunct(r) || unicode.IsSymbol(r) }
