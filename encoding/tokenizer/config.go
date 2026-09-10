package tokenizer

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// The pre-tokenizer patterns this package implements. They are compared byte
// for byte against the pattern stored in tokenizer.json so that an unexpected
// file is rejected rather than silently mistokenized.
//
// Note that tokenizer.json stores the word pattern with literal CR and LF
// control characters instead of the regex escapes, so the Go literal below
// spells them with "\r" and "\n"; both forms describe the same characters.
const (
	expectedDigitsPattern = `\p{N}{1,3}`
	expectedCJKPattern    = "[\u4e00-\u9fa5\u3040-\u309f\u30a0-\u30ff]+"
	expectedWordPattern   = "[!\"#$%&'()*+,\\-./:;<=>?@\\[\\\\\\]^_\x60{|}~][A-Za-z]+|[^\r\n\\p{L}\\p{P}\\p{S}]?[\\p{L}\\p{M}]+| ?[\\p{P}\\p{S}]+[\r\n]*|\\s*[\r\n]+|\\s+(?!\\S)|\\s+"
)

// tokenizerFile is the subset of tokenizer.json this package reads.
type tokenizerFile struct {
	Version       string           `json:"version"`
	AddedTokens   []addedTokenFile `json:"added_tokens"`
	Normalizer    json.RawMessage  `json:"normalizer"`
	PreTokenizer  json.RawMessage  `json:"pre_tokenizer"`
	PostProcessor json.RawMessage  `json:"post_processor"`
	Decoder       json.RawMessage  `json:"decoder"`
	Model         modelFile        `json:"model"`
}

// addedTokenFile mirrors one entry of the "added_tokens" array.
type addedTokenFile struct {
	ID         uint32 `json:"id"`
	Content    string `json:"content"`
	SingleWord bool   `json:"single_word"`
	LStrip     bool   `json:"lstrip"`
	RStrip     bool   `json:"rstrip"`
	Normalized bool   `json:"normalized"`
	Special    bool   `json:"special"`
}

// modelFile mirrors the BPE section of tokenizer.json.
type modelFile struct {
	Type                    string            `json:"type"`
	Dropout                 *float64          `json:"dropout"`
	UnkToken                *string           `json:"unk_token"`
	ContinuingSubwordPrefix *string           `json:"continuing_subword_prefix"`
	EndOfWordSuffix         *string           `json:"end_of_word_suffix"`
	FuseUnk                 bool              `json:"fuse_unk"`
	ByteFallback            bool              `json:"byte_fallback"`
	IgnoreMerges            bool              `json:"ignore_merges"`
	Vocab                   map[string]uint32 `json:"vocab"`
	Merges                  []json.RawMessage `json:"merges"`
}

// normalizerFile mirrors a normalizer node.
type normalizerFile struct {
	Type        string            `json:"type"`
	Normalizers []json.RawMessage `json:"normalizers"`
}

// preTokenizerFile mirrors a pre-tokenizer node.
type preTokenizerFile struct {
	Type          string            `json:"type"`
	Pretokenizers []json.RawMessage `json:"pretokenizers"`
}

// splitFile mirrors the Split pre-tokenizer.
type splitFile struct {
	Type    string `json:"type"`
	Pattern struct {
		String *string `json:"String"`
		Regex  *string `json:"Regex"`
	} `json:"pattern"`
	Behavior string `json:"behavior"`
	Invert   bool   `json:"invert"`
}

// byteLevelFile mirrors the ByteLevel pre-tokenizer, decoder and
// post-processor.
type byteLevelFile struct {
	Type           string `json:"type"`
	AddPrefixSpace bool   `json:"add_prefix_space"`
	TrimOffsets    bool   `json:"trim_offsets"`
	UseRegex       bool   `json:"use_regex"`
}

// validateNormalizer accepts only a missing/nil normalizer or an empty
// Sequence, i.e. the identity normalizer.
func validateNormalizer(raw json.RawMessage) error {
	if isNull(raw) {
		return nil
	}
	var n normalizerFile
	if err := json.Unmarshal(raw, &n); err != nil {
		return fmt.Errorf("tokenizer: parse normalizer: %w", err)
	}
	if n.Type == "Sequence" && len(n.Normalizers) == 0 {
		return nil
	}
	return fmt.Errorf("tokenizer: unsupported normalizer %q with %d step(s): only an empty Sequence is implemented", n.Type, len(n.Normalizers))
}

// validatePostProcessor accepts only a missing/nil post-processor or a
// ByteLevel one. ByteLevel only rewrites offsets, so it never changes the ids
// produced with add_special_tokens=false.
func validatePostProcessor(raw json.RawMessage) error {
	if isNull(raw) {
		return nil
	}
	var p byteLevelFile
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("tokenizer: parse post_processor: %w", err)
	}
	if p.Type == "ByteLevel" {
		return nil
	}
	return fmt.Errorf("tokenizer: unsupported post_processor %q: it could add tokens to the encoding", p.Type)
}

// validateDecoder accepts a missing/nil decoder or a ByteLevel one and reports
// whether a decoder is configured.
func validateDecoder(raw json.RawMessage) (bool, error) {
	if isNull(raw) {
		return false, nil
	}
	var d byteLevelFile
	if err := json.Unmarshal(raw, &d); err != nil {
		return false, fmt.Errorf("tokenizer: parse decoder: %w", err)
	}
	if d.Type != "ByteLevel" {
		return false, fmt.Errorf("tokenizer: unsupported decoder %q", d.Type)
	}
	// ByteLevel's decoder ignores add_prefix_space (see the Rust
	// implementation of Decoder for ByteLevel), so only its presence matters.
	return true, nil
}

// validatePreTokenizer checks the exact pre-tokenizer pipeline and returns the
// ByteLevel add_prefix_space flag.
func validatePreTokenizer(raw json.RawMessage) (bool, error) {
	if isNull(raw) {
		return false, fmt.Errorf("tokenizer: missing pre_tokenizer")
	}
	var pt preTokenizerFile
	if err := json.Unmarshal(raw, &pt); err != nil {
		return false, fmt.Errorf("tokenizer: parse pre_tokenizer: %w", err)
	}
	if pt.Type != "Sequence" {
		return false, fmt.Errorf("tokenizer: unsupported pre_tokenizer %q (want Sequence)", pt.Type)
	}
	want := []string{expectedDigitsPattern, expectedCJKPattern, expectedWordPattern}
	if len(pt.Pretokenizers) != len(want)+1 {
		return false, fmt.Errorf("tokenizer: pre_tokenizer Sequence has %d steps, want %d", len(pt.Pretokenizers), len(want)+1)
	}
	for i, pattern := range want {
		var s splitFile
		if err := json.Unmarshal(pt.Pretokenizers[i], &s); err != nil {
			return false, fmt.Errorf("tokenizer: parse pre_tokenizer step %d: %w", i, err)
		}
		if s.Type != "Split" {
			return false, fmt.Errorf("tokenizer: pre_tokenizer step %d is %q, want Split", i, s.Type)
		}
		if s.Pattern.Regex == nil {
			return false, fmt.Errorf("tokenizer: pre_tokenizer step %d is not a Regex pattern", i)
		}
		if *s.Pattern.Regex != pattern {
			return false, fmt.Errorf("tokenizer: pre_tokenizer step %d has pattern %q, want %q", i, *s.Pattern.Regex, pattern)
		}
		if s.Behavior != "Isolated" {
			return false, fmt.Errorf("tokenizer: pre_tokenizer step %d has behavior %q, want Isolated", i, s.Behavior)
		}
		if s.Invert {
			return false, fmt.Errorf("tokenizer: pre_tokenizer step %d is inverted", i)
		}
	}
	var bl byteLevelFile
	if err := json.Unmarshal(pt.Pretokenizers[len(want)], &bl); err != nil {
		return false, fmt.Errorf("tokenizer: parse pre_tokenizer step %d: %w", len(want), err)
	}
	if bl.Type != "ByteLevel" {
		return false, fmt.Errorf("tokenizer: pre_tokenizer step %d is %q, want ByteLevel", len(want), bl.Type)
	}
	if bl.UseRegex {
		return false, fmt.Errorf("tokenizer: ByteLevel(use_regex=true) is not implemented")
	}
	return bl.AddPrefixSpace, nil
}

// isNull reports whether a raw JSON field is absent or JSON null.
func isNull(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}
