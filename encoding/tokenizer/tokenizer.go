// Package tokenizer implements a pure-Go, dependency-free port of the
// HuggingFace tokenizers pipeline used by the bundled DeepSeek V4 and V4.1
// tokenizer.json files.
//
// The implementation mirrors the Rust tokenizers crate (v0.23.x, which backs
// the Python tokenizers==0.23.2 package) for the exact pipeline stored in
// static/tokenizers/{v4,v41}/tokenizer.json:
//
//   - normalizer:    empty Sequence (the identity)
//   - pre_tokenizer: a Sequence of Split("\\p{N}{1,3}", Isolated),
//     Split([\u4e00-\u9fa5\u3040-\u309f\u30a0-\u30ff]+, Isolated),
//     Split(<word regex>, Isolated) and
//     ByteLevel(add_prefix_space=false, use_regex=false)
//   - model:         BPE; the bundled files set no unk_token, no byte
//     fallback and no dropout, but those options are implemented as well
//   - added_tokens:  matched literally against the raw text (longest match
//     wins, ties broken by file order); tokens flagged "normalized" are
//     matched in a second pass over the pieces left by the first one
//   - decoder:       ByteLevel (whose add_prefix_space flag is ignored when
//     decoding, exactly as in the Rust implementation)
//
// Go's regexp package (RE2) does not support lookahead, so the "\s+(?!\S)"
// alternative of the word regex is implemented by hand in pretok.go with
// Perl-style backtracking semantics; see splitWord.
//
// The zero value is not usable; call Load or LoadBytes. A Tokenizer is safe
// for concurrent use once constructed: all methods are read-only.
package tokenizer

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Tokenizer holds a fully parsed tokenizer.json pipeline.
type Tokenizer struct {
	// --- BPE model -------------------------------------------------------

	// vocab maps a token string to its id.
	vocab map[string]uint32
	// vocabR maps an id to its token; indexes without a token hold "".
	vocabR []string
	// merges maps an adjacent pair of token ids to its merge information.
	// Keying by ids (exactly like the Rust reference builds its MergeMap)
	// keeps the hot loop free of string concatenation.
	merges map[mergeKey]mergeInfo

	// byteFallback, unkID, fuseUnk, contPrefix and endSuffix mirror the
	// corresponding BPE options. The bundled tokenizer.json files disable all
	// of them, but the code paths exist for fidelity with the Rust model.
	byteFallback  bool
	unkID         uint32
	hasUnk        bool
	fuseUnk       bool
	contPrefix    string
	hasContPrefix bool
	endSuffix     string
	hasEndSuffix  bool

	// --- added tokens ----------------------------------------------------

	// addedID maps an added token's content to its id.
	addedID map[string]uint32
	// addedContent maps an added token's id to the content used when decoding.
	addedContent map[uint32]string
	// special holds the content of every added token flagged "special"; it is
	// the set consulted by DecodeIDs when skipSpecialTokens is true.
	special map[string]struct{}
	// nonNorm and norm match added tokens whose "normalized" flag is false and
	// true respectively. HuggingFace extracts the non-normalized tokens from
	// the raw text first and only then the normalized ones from the remaining
	// pieces.
	nonNorm *addedTrie
	norm    *addedTrie

	// --- pre-tokenizer / decoder ----------------------------------------

	// addPrefixSpace mirrors ByteLevel.add_prefix_space of the pre-tokenizer.
	addPrefixSpace bool
	// byteLevelDecoder reports whether a ByteLevel decoder was configured.
	// When false, DecodeIDs joins the tokens with a single space, matching the
	// Rust fallback.
	byteLevelDecoder bool

	// vocabSize is the total vocabulary size, including added tokens, i.e. the
	// equivalent of HuggingFace's Tokenizer.get_vocab_size().
	vocabSize int
}

// Load reads and parses a HuggingFace tokenizer.json file.
func Load(path string) (*Tokenizer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	t, err := LoadBytes(data)
	if err != nil {
		return nil, fmt.Errorf("tokenizer: %s: %w", path, err)
	}
	return t, nil
}

// FromFile is an alias for Load and returns a new Tokenizer for the
// tokenizer.json file at path.
func (t *Tokenizer) FromFile(path string) (*Tokenizer, error) {
	return Load(path)
}

// LoadBytes parses a HuggingFace tokenizer.json document.
//
// The pipeline is validated while parsing: configurations this package does not
// implement (a normalizer, a different pre-tokenizer, a post-processor that
// inserts tokens, a non-BPE model or a non-zero dropout) are rejected with a
// descriptive error instead of being silently ignored.
func LoadBytes(data []byte) (*Tokenizer, error) {
	var f tokenizerFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("tokenizer: parse tokenizer.json: %w", err)
	}
	if f.Model.Type != "BPE" {
		return nil, fmt.Errorf("tokenizer: unsupported model type %q (want BPE)", f.Model.Type)
	}
	if f.Model.Dropout != nil && *f.Model.Dropout != 0 {
		return nil, fmt.Errorf("tokenizer: dropout %v is not supported (encoding would be random)", *f.Model.Dropout)
	}
	if f.Model.IgnoreMerges {
		return nil, fmt.Errorf("tokenizer: ignore_merges is not supported")
	}
	if err := validateNormalizer(f.Normalizer); err != nil {
		return nil, err
	}
	if err := validatePostProcessor(f.PostProcessor); err != nil {
		return nil, err
	}
	addPrefixSpace, err := validatePreTokenizer(f.PreTokenizer)
	if err != nil {
		return nil, err
	}
	hasDecoder, err := validateDecoder(f.Decoder)
	if err != nil {
		return nil, err
	}

	t := &Tokenizer{
		vocab:            make(map[string]uint32, len(f.Model.Vocab)),
		merges:           make(map[mergeKey]mergeInfo, len(f.Model.Merges)),
		addedID:          make(map[string]uint32, len(f.AddedTokens)),
		addedContent:     make(map[uint32]string, len(f.AddedTokens)),
		special:          make(map[string]struct{}),
		addPrefixSpace:   addPrefixSpace,
		byteLevelDecoder: hasDecoder,
	}

	// --- model -----------------------------------------------------------

	var maxID uint32
	for tok, id := range f.Model.Vocab {
		t.vocab[tok] = id
		if id > maxID {
			maxID = id
		}
	}
	t.vocabR = make([]string, maxID+1)
	for tok, id := range t.vocab {
		t.vocabR[id] = tok
	}
	if f.Model.UnkToken != nil {
		id, ok := t.vocab[*f.Model.UnkToken]
		if !ok {
			return nil, fmt.Errorf("tokenizer: unk_token %q is not in the vocabulary", *f.Model.UnkToken)
		}
		t.unkID, t.hasUnk = id, true
	}
	t.byteFallback = f.Model.ByteFallback
	t.fuseUnk = f.Model.FuseUnk
	if f.Model.ContinuingSubwordPrefix != nil {
		t.contPrefix, t.hasContPrefix = *f.Model.ContinuingSubwordPrefix, true
	}
	if f.Model.EndOfWordSuffix != nil {
		t.endSuffix, t.hasEndSuffix = *f.Model.EndOfWordSuffix, true
	}

	for rank, raw := range f.Model.Merges {
		left, right, err := parseMerge(raw)
		if err != nil {
			return nil, fmt.Errorf("tokenizer: merge %d: %w", rank, err)
		}
		leftID, ok := t.vocab[left]
		if !ok {
			return nil, fmt.Errorf("tokenizer: merge %d: token %q is not in the vocabulary", rank, left)
		}
		rightID, ok := t.vocab[right]
		if !ok {
			return nil, fmt.Errorf("tokenizer: merge %d: token %q is not in the vocabulary", rank, right)
		}
		merged := left + strings.TrimPrefix(right, t.contPrefix)
		mergedID, ok := t.vocab[merged]
		if !ok {
			return nil, fmt.Errorf("tokenizer: merge %d: merged token %q is not in the vocabulary", rank, merged)
		}
		t.merges[mergeKey{left: leftID, right: rightID}] = mergeInfo{rank: int32(rank), id: mergedID}
	}

	// --- added tokens ----------------------------------------------------

	t.nonNorm = newAddedTrie()
	t.norm = newAddedTrie()
	for _, at := range f.AddedTokens {
		if at.Content == "" {
			continue
		}
		// add_tokens() keeps the id of an already known content; the files
		// contain no duplicates, but stay faithful to that rule.
		id, ok := t.addedID[at.Content]
		if !ok {
			id = at.ID
			t.addedID[at.Content] = id
		}
		if _, ok := t.addedContent[id]; !ok {
			t.addedContent[id] = at.Content
		}
		if at.Special {
			t.special[at.Content] = struct{}{}
		}
		// The normalizer is the identity here, so a normalized token matches
		// its own content.
		if at.Normalized {
			t.norm.add(at.Content, id)
		} else {
			t.nonNorm.add(at.Content, id)
		}
	}

	// VocabSize counts distinct known ids, like HF's get_vocab_size().
	t.vocabSize = len(t.vocab)
	for id := range t.addedContent {
		if int(id) >= len(t.vocabR) || t.vocabR[id] == "" {
			t.vocabSize++
		}
	}
	return t, nil
}

// parseMerge decodes one entry of model.merges. HuggingFace serializes merges
// either as "left right" strings or as two-element arrays.
func parseMerge(raw json.RawMessage) (left, right string, err error) {
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", "", err
		}
		i := strings.IndexByte(s, ' ')
		if i < 0 {
			return "", "", fmt.Errorf("malformed merge %q", s)
		}
		return s[:i], s[i+1:], nil
	}
	var pair [2]string
	if err := json.Unmarshal(raw, &pair); err != nil {
		return "", "", err
	}
	return pair[0], pair[1], nil
}

// VocabSize returns the total number of tokens known to the tokenizer: the BPE
// vocabulary plus every added token. It is the equivalent of HuggingFace's
// Tokenizer.get_vocab_size() (129280 for the bundled V4/V4.1 files).
func (t *Tokenizer) VocabSize() int { return t.vocabSize }

// IDForToken returns the id of token, looking it up in the added vocabulary
// first and then in the BPE vocabulary, mirroring
// AddedVocabulary::token_to_id. The second result reports whether the token is
// known.
func (t *Tokenizer) IDForToken(token string) (uint32, bool) {
	if id, ok := t.addedID[token]; ok {
		return id, true
	}
	id, ok := t.vocab[token]
	return id, ok
}

// EncodeIDs tokenizes text and returns its token ids. It is equivalent to
// tokenizers.Tokenizer.encode(text, add_special_tokens=False).ids.
//
// The error return is always nil for a tokenizer built by Load/LoadBytes; it
// exists to satisfy the API contract and to leave room for future pipeline
// stages that can fail.
func (t *Tokenizer) EncodeIDs(text string) ([]uint32, error) {
	ids := make([]uint32, 0, len(text)/3+1)
	if text == "" {
		return ids, nil
	}
	// Pass 1: non-normalized added tokens are extracted from the raw text.
	for _, piece := range splitAdded(text, t.nonNorm) {
		if piece.isToken {
			ids = append(ids, piece.id)
			continue
		}
		// Pass 2: normalized added tokens are extracted from what is left.
		// The configured normalizer is the identity, so the piece text is
		// unchanged.
		for _, sub := range splitAdded(piece.text, t.norm) {
			if sub.isToken {
				ids = append(ids, sub.id)
				continue
			}
			ids = t.encodeSegment(sub.text, ids)
		}
	}
	return ids, nil
}

// encodeSegment runs the pre-tokenizer over one added-token-free segment and
// appends the BPE ids of every resulting split to ids.
func (t *Tokenizer) encodeSegment(segment string, ids []uint32) []uint32 {
	for _, digits := range splitDigits(segment) {
		for _, cjk := range splitCJK(digits) {
			for _, word := range splitWord(cjk) {
				if t.addPrefixSpace && !strings.HasPrefix(word, " ") {
					word = " " + word
				}
				ids = t.bpeEncode(byteLevelEncode(word), ids)
			}
		}
	}
	return ids
}

// DecodeIDs converts ids back to text. It is equivalent to
// tokenizers.Tokenizer.decode(ids, skip_special_tokens=skipSpecialTokens).
//
// Ids that are unknown to both the added vocabulary and the BPE vocabulary are
// skipped, exactly like the Rust implementation does. With a ByteLevel decoder
// (the bundled files) each token is mapped back to bytes, tokens without a pure
// byte-level content are emitted verbatim, and the concatenation is decoded as
// UTF-8 with invalid sequences replaced by U+FFFD.
func (t *Tokenizer) DecodeIDs(ids []uint32, skipSpecialTokens bool) (string, error) {
	tokens := make([]string, 0, len(ids))
	for _, id := range ids {
		tok, ok := t.addedContent[id]
		if !ok {
			if int(id) < len(t.vocabR) {
				tok = t.vocabR[id]
			}
			if tok == "" {
				continue
			}
		}
		if skipSpecialTokens {
			if _, isSpecial := t.special[tok]; isSpecial {
				continue
			}
		}
		tokens = append(tokens, tok)
	}
	if !t.byteLevelDecoder {
		return strings.Join(tokens, " "), nil
	}
	var buf []byte
	for _, tok := range tokens {
		buf = byteLevelDecodeToken(buf, tok)
	}
	return rustLossyUTF8(buf), nil
}
