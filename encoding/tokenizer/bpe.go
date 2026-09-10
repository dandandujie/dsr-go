package tokenizer

import (
	"container/heap"
	"fmt"
	"unicode/utf8"
)

// mergeKey identifies an adjacent pair of vocabulary ids. The reference Rust
// implementation keys its merge map by token ids as well
// (MergeMap = HashMap<(u32, u32), (u32, u32)>), which keeps the BPE hot loop
// free of string concatenation.
type mergeKey struct {
	left, right uint32
}

// mergeInfo is the rank of a merge (its index in model.merges) together with
// the id of the token it produces.
type mergeInfo struct {
	rank int32
	id   uint32
}

// bpeSymbol is one symbol of the linked list BPE operates on. Symbols are
// removed by setting length to zero, exactly like the Rust Word type does.
type bpeSymbol struct {
	c      uint32 // vocabulary id
	prev   int32
	next   int32
	length int // byte length; 0 marks a removed symbol
}

// bpeEncode byte-level-encodes nothing itself: it takes the byte-level encoded
// piece and appends the ids of the resulting BPE tokens to out.
func (t *Tokenizer) bpeEncode(piece string, out []uint32) []uint32 {
	if piece == "" {
		return out
	}
	symbols := t.splitSymbols(piece)
	if len(symbols) == 0 {
		return out
	}
	t.mergeSymbols(symbols)
	for i := range symbols {
		if symbols[i].length != 0 {
			out = append(out, symbols[i].c)
		}
	}
	return out
}

// splitSymbols turns a byte-level encoded piece into one symbol per character,
// applying the BPE options (continuing_subword_prefix, end_of_word_suffix,
// byte_fallback and unk_token) exactly like BPE::merge_word does. Characters
// that are neither in the vocabulary nor covered by byte fallback or an unk
// token are dropped, again like the reference.
func (t *Tokenizer) splitSymbols(piece string) []bpeSymbol {
	symbols := make([]bpeSymbol, 0, len(piece))
	add := func(c uint32, length int) {
		prev, next := int32(-1), int32(-1)
		if n := len(symbols); n > 0 {
			prev = int32(n - 1)
			symbols[n-1].next = int32(n)
		}
		symbols = append(symbols, bpeSymbol{c: c, prev: prev, next: next, length: length})
	}

	var unkID uint32
	unkLen := 0
	unkPending := false

	for i := 0; i < len(piece); {
		_, size := utf8.DecodeRuneInString(piece[i:])
		sub := piece[i : i+size]
		if len(symbols) > 0 && t.hasContPrefix {
			sub = t.contPrefix + sub
		}
		if t.hasEndSuffix && i+size == len(piece) {
			sub += t.endSuffix
		}
		if id, ok := t.vocab[sub]; ok {
			if unkPending {
				add(unkID, unkLen)
				unkPending = false
			}
			add(id, size)
			i += size
			continue
		}
		if t.byteFallback {
			bytes := make([]uint32, 0, size)
			complete := true
			for j := 0; j < size; j++ {
				code := fmt.Sprintf("<0x%02X>", piece[i+j])
				id, ok := t.vocab[code]
				if !ok {
					complete = false
					break
				}
				bytes = append(bytes, id)
			}
			if complete {
				for _, id := range bytes {
					add(id, 1)
				}
				i += size
				continue
			}
		}
		if !t.hasUnk {
			// No unk token and no byte fallback: the character is dropped.
			i += size
			continue
		}
		if unkPending && t.fuseUnk {
			unkLen += size
		} else {
			if unkPending {
				add(unkID, unkLen)
			}
			unkID, unkLen, unkPending = t.unkID, size, true
		}
		i += size
	}
	if unkPending {
		add(unkID, unkLen)
	}
	return symbols
}

// mergeSymbols runs the BPE merge loop over symbols. It mirrors
// Word::merge_all: a min-heap ordered by (rank, position) yields the next
// merge, stale heap entries are recognised by comparing the id the pair would
// produce, and the two symbols adjacent to a merge are re-examined.
func (t *Tokenizer) mergeSymbols(symbols []bpeSymbol) {
	h := make(mergeHeap, 0, len(symbols))
	for i := 0; i+1 < len(symbols); i++ {
		if info, ok := t.merges[mergeKey{symbols[i].c, symbols[i+1].c}]; ok {
			h = append(h, mergeEntry{pos: int32(i), rank: info.rank, id: info.id})
		}
	}
	heap.Init(&h)

	for h.Len() > 0 {
		top := heap.Pop(&h).(mergeEntry)
		current := &symbols[top.pos]
		if current.length == 0 {
			continue // the symbol was merged away
		}
		if current.next < 0 {
			continue // last symbol
		}
		right := &symbols[current.next]
		info, ok := t.merges[mergeKey{current.c, right.c}]
		if !ok || info.id != top.id {
			continue // expired heap entry
		}

		// Merge right into current.
		current.c = top.id
		current.length += right.length
		current.next = right.next
		right.length = 0
		if right.next >= 0 {
			symbols[right.next].prev = top.pos
		}

		if current.prev >= 0 {
			prev := &symbols[current.prev]
			if info, ok := t.merges[mergeKey{prev.c, current.c}]; ok {
				heap.Push(&h, mergeEntry{pos: current.prev, rank: info.rank, id: info.id})
			}
		}
		if current.next >= 0 {
			next := &symbols[current.next]
			if info, ok := t.merges[mergeKey{current.c, next.c}]; ok {
				heap.Push(&h, mergeEntry{pos: top.pos, rank: info.rank, id: info.id})
			}
		}
	}
}

// mergeEntry is a pending BPE merge in the priority queue.
type mergeEntry struct {
	pos  int32
	rank int32
	id   uint32
}

// mergeHeap is a min-heap ordered by merge rank and then by position, matching
// the Ord implementation of the Rust Merge type.
type mergeHeap []mergeEntry

func (h mergeHeap) Len() int { return len(h) }
func (h mergeHeap) Less(i, j int) bool {
	if h[i].rank != h[j].rank {
		return h[i].rank < h[j].rank
	}
	return h[i].pos < h[j].pos
}
func (h mergeHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *mergeHeap) Push(x any)   { *h = append(*h, x.(mergeEntry)) }
func (h *mergeHeap) Pop() any {
	old := *h
	n := len(old)
	e := old[n-1]
	*h = old[:n-1]
	return e
}
