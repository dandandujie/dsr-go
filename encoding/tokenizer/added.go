package tokenizer

// addedPiece is one piece of an input string produced by splitAdded: either a
// literal added token (isToken) or a stretch of ordinary text.
type addedPiece struct {
	text    string
	id      uint32
	isToken bool
}

// addedTrie matches added-token contents literally. HuggingFace builds a
// leftmost-longest Aho-Corasick automaton (daachorse, MatchKind::LeftmostLongest)
// over the added tokens, which is equivalent to this byte trie: scanning from
// the left, the longest token starting at the current position wins, and the
// scan resumes after it. When two tokens with the same content exist the first
// one added (file order) wins.
type addedTrie struct {
	children []map[byte]int32
	term     []int32 // index into ids, or -1
	ids      []uint32
	size     int
}

// newAddedTrie returns an empty trie with its root node.
func newAddedTrie() *addedTrie {
	return &addedTrie{children: []map[byte]int32{{}}, term: []int32{-1}}
}

// add inserts content, mapping it to id. Contents already present keep their
// first id, mirroring AddedVocabulary::add_tokens.
func (tr *addedTrie) add(content string, id uint32) {
	if content == "" {
		return
	}
	node := int32(0)
	for i := 0; i < len(content); i++ {
		b := content[i]
		child, ok := tr.children[node][b]
		if !ok {
			tr.children = append(tr.children, make(map[byte]int32))
			tr.term = append(tr.term, -1)
			child = int32(len(tr.children) - 1)
			tr.children[node][b] = child
		}
		node = child
	}
	if tr.term[node] < 0 {
		tr.term[node] = int32(len(tr.ids))
		tr.ids = append(tr.ids, id)
		tr.size++
	}
}

// matchAt returns the longest token of the trie that starts at byte offset at.
func (tr *addedTrie) matchAt(s string, at int) (id uint32, n int, ok bool) {
	if tr == nil || tr.size == 0 {
		return 0, 0, false
	}
	node := int32(0)
	for i := at; i < len(s); i++ {
		child, found := tr.children[node][s[i]]
		if !found {
			break
		}
		node = child
		if t := tr.term[node]; t >= 0 {
			id, n, ok = tr.ids[t], i-at+1, true
		}
	}
	return id, n, ok
}

// splitAdded splits s around every literal added token known to trie, keeping
// the text between matches as separate pieces. It is the equivalent of
// AddedVocabulary::find_matches: the returned pieces cover s in order, and an
// empty input yields no pieces (the reference filters empty splits out).
func splitAdded(s string, trie *addedTrie) []addedPiece {
	if s == "" || trie == nil || trie.size == 0 {
		if s == "" {
			return nil
		}
		return []addedPiece{{text: s}}
	}
	var out []addedPiece
	prev := 0
	i := 0
	for i < len(s) {
		id, n, ok := trie.matchAt(s, i)
		if !ok {
			i++
			continue
		}
		if prev < i {
			out = append(out, addedPiece{text: s[prev:i]})
		}
		out = append(out, addedPiece{text: s[i : i+n], id: id, isToken: true})
		i += n
		prev = i
	}
	if prev < len(s) {
		out = append(out, addedPiece{text: s[prev:]})
	}
	return out
}
