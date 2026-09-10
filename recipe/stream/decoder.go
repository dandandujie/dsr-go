package stream

import "strings"

// replacementCharacter is emitted while a multi-token character is incomplete.
const replacementCharacter = "\uFFFD"

// TokenizerDecoder decodes token IDs into text.
type TokenizerDecoder interface {
	// DecodeIDs decodes ids into text, skipping special tokens when requested.
	DecodeIDs(ids []uint32, skipSpecialTokens bool) (string, error)
}

// StreamDecoder buffers token IDs until they decode into text.
type StreamDecoder struct {
	decoder TokenizerDecoder
	ids     []uint32
	pending int
}

// NewStreamDecoder wraps a tokenizer decoder.
func NewStreamDecoder(decoder TokenizerDecoder) *StreamDecoder {
	return &StreamDecoder{decoder: decoder}
}

// Decode returns the text contributed by the buffered token IDs and the number
// of IDs it accounts for. ok is false while no text is available yet.
//
// Special tokens drive the parser's state machine, so they stay in the decoded
// text.
func (d *StreamDecoder) Decode(tokenID uint32) (content string, count int, ok bool, err error) {
	d.pending++
	d.ids = append(d.ids, tokenID)
	decoded, err := d.decoder.DecodeIDs(d.ids, false)
	if err != nil {
		return "", 0, false, &StreamError{DecodeDetail: err.Error()}
	}
	if decoded == "" || strings.HasSuffix(decoded, replacementCharacter) {
		return "", 0, false, nil
	}
	d.ids = d.ids[:0]
	count = d.pending
	d.pending = 0
	return decoded, count, true, nil
}
