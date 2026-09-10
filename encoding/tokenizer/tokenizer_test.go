package tokenizer

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	v41Path = "../../static/tokenizers/v41/tokenizer.json"
	v4Path  = "../../static/tokenizers/v4/tokenizer.json"
)

// goldenCase is one entry of testdata/tokenizer_cases*.json, generated from the
// Python tokenizers==0.23.2 oracle by testdata/gen_cases.py.
type goldenCase struct {
	Text               string   `json:"text"`
	IDs                []uint32 `json:"ids"`
	Decoded            string   `json:"decoded"`
	DecodedSkipSpecial string   `json:"decoded_skip_special"`
}

// goldenFile is the document stored in testdata/tokenizer_cases*.json.
type goldenFile struct {
	Tokenizer string       `json:"tokenizer"`
	Cases     []goldenCase `json:"cases"`
}

func loadGolden(t *testing.T, path string) goldenFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}
	var g goldenFile
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatalf("parse golden file: %v", err)
	}
	if len(g.Cases) < 120 {
		t.Fatalf("golden file %s has only %d cases, want at least 120", path, len(g.Cases))
	}
	return g
}

func runGolden(t *testing.T, modelPath, goldenPath string) {
	t.Helper()
	tok, err := Load(modelPath)
	if err != nil {
		t.Fatalf("Load(%s): %v", modelPath, err)
	}
	g := loadGolden(t, goldenPath)
	for i, c := range g.Cases {
		gotIDs, err := tok.EncodeIDs(c.Text)
		if err != nil {
			t.Fatalf("case %d %q: EncodeIDs: %v", i, c.Text, err)
		}
		if !equalIDs(gotIDs, c.IDs) {
			t.Errorf("case %d %q:\n got ids %v\nwant ids %v", i, c.Text, gotIDs, c.IDs)
			continue
		}
		got, err := tok.DecodeIDs(c.IDs, false)
		if err != nil {
			t.Fatalf("case %d %q: DecodeIDs: %v", i, c.Text, err)
		}
		if got != c.Decoded {
			t.Errorf("case %d %q: DecodeIDs(skip=false)\n got %q\nwant %q", i, c.Text, got, c.Decoded)
		}
		gotSkip, err := tok.DecodeIDs(c.IDs, true)
		if err != nil {
			t.Fatalf("case %d %q: DecodeIDs(skip): %v", i, c.Text, err)
		}
		if gotSkip != c.DecodedSkipSpecial {
			t.Errorf("case %d %q: DecodeIDs(skip=true)\n got %q\nwant %q", i, c.Text, gotSkip, c.DecodedSkipSpecial)
		}
	}
}

func equalIDs(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestGoldenV41 checks EncodeIDs and DecodeIDs against the 170 cases produced
// by tokenizers==0.23.2 for static/tokenizers/v41/tokenizer.json.
func TestGoldenV41(t *testing.T) {
	runGolden(t, v41Path, "testdata/tokenizer_cases.json")
}

// TestGoldenV4 runs the same corpus (regenerated for the V4 added tokens)
// against static/tokenizers/v4/tokenizer.json.
func TestGoldenV4(t *testing.T) {
	runGolden(t, v4Path, "testdata/tokenizer_cases_v4.json")
}

// TestImageTokenV41 asserts that the image placeholder is a single added token.
func TestImageTokenV41(t *testing.T) {
	tok, err := Load(v41Path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ids, err := tok.EncodeIDs("<｜image｜>")
	if err != nil {
		t.Fatalf("EncodeIDs: %v", err)
	}
	if len(ids) != 1 || ids[0] != 129264 {
		t.Fatalf("EncodeIDs(<｜image｜>) = %v, want [129264]", ids)
	}
	if id, ok := tok.IDForToken("<｜image｜>"); !ok || id != 129264 {
		t.Fatalf("IDForToken(<｜image｜>) = %d, %v; want 129264, true", id, ok)
	}
}

// TestVocabSize checks the model plus added vocabulary size reported by
// HuggingFace's get_vocab_size().
func TestVocabSize(t *testing.T) {
	for _, path := range []string{v41Path, v4Path} {
		tok, err := Load(path)
		if err != nil {
			t.Fatalf("Load(%s): %v", path, err)
		}
		if got := tok.VocabSize(); got != 129280 {
			t.Errorf("VocabSize(%s) = %d, want 129280", path, got)
		}
	}
}

// TestFromFile checks that the FromFile method alias works.
func TestFromFile(t *testing.T) {
	tok, err := (&Tokenizer{}).FromFile(v41Path)
	if err != nil {
		t.Fatalf("FromFile: %v", err)
	}
	ids, err := tok.EncodeIDs("Hello")
	if err != nil {
		t.Fatalf("EncodeIDs: %v", err)
	}
	if len(ids) != 1 || ids[0] != 19923 {
		t.Fatalf("EncodeIDs(Hello) = %v, want [19923]", ids)
	}
}

// TestWordSplitLookahead documents the Perl-style backtracking semantics of the
// "\s+(?!\S)" alternative that Go's RE2 regexp cannot express. Each expected
// split list was verified against the Python oracle end to end.
func TestWordSplitLookahead(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"a  b", []string{"a", " ", " b"}},
		{"a \n\n b", []string{"a", " \n\n", " b"}},
		{"trailing   ", []string{"trailing", "   "}},
		{"  leading", []string{" ", " leading"}},
		{"   ", []string{"   "}},
		{"\n\n", []string{"\n\n"}},
		{"a\n b", []string{"a", "\n", " b"}},
		{"a\r \nb", []string{"a", "\r \n", "b"}},
		{" \n", []string{" \n"}},
		{"\n ", []string{"\n", " "}},
		{"a ", []string{"a", " "}},
		{"a \nb", []string{"a", " \n", "b"}},
		{"a\n\n\n\nb", []string{"a", "\n\n\n\n", "b"}},
		{"a\n \nb", []string{"a", "\n \n", "b"}},
		{"x\n \n\ny", []string{"x", "\n \n\n", "y"}},
		// A tab is a valid "[^\r\n\p{L}\p{P}\p{S}]" optional character, so it
		// attaches to the letters that follow it.
		{"a\tb", []string{"a", "\tb"}},
		{"a\t\tb", []string{"a", "\t", "\tb"}},
		{"\t", []string{"\t"}},
		{"a\r\nb", []string{"a", "\r\n", "b"}},
		{"a\r\n\r\nb", []string{"a", "\r\n\r\n", "b"}},
		{"a \r\n b", []string{"a", " \r\n", " b"}},
		{"a!\n", []string{"a", "!\n"}},
	}
	for _, tc := range tests {
		got := splitWord(tc.in)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("splitWord(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestMatchWordPiece exercises the individual alternatives of the word regex,
// including the lookahead alternative that is implemented by hand.
func TestMatchWordPiece(t *testing.T) {
	tests := []struct {
		in   string
		at   int
		n    int
		desc string
	}{
		{"abc", 0, 3, "letter run"},
		{"abc def", 0, 3, "letter run stops at space"},
		{"'s", 0, 2, "apostrophe + ASCII letters"},
		{"!abc!!", 0, 4, "punctuation + ASCII letters"},
		{"!!!", 0, 3, "punctuation run"},
		{" (x)", 0, 2, "optional space + punctuation"},
		{" a", 0, 2, "optional space + letters"},
		{"123", 0, 0, "digits are not matched by the word pattern"},
		{"\x01", 0, 0, "control characters match nothing"},
		{"\x01a", 0, 2, "control character as optional prefix"},
		{"\n\n", 0, 2, "CR/LF alternative"},
		{"\t\t\n", 0, 3, "whitespace then CR/LF"},
		{" \n\nx", 0, 3, "last CR/LF of the whitespace run"},
		{" x", 0, 2, "optional space + letter is one split"},
		{"  x", 0, 1, "two spaces: the lookahead keeps one for the next split"},
		{"   ", 0, 3, "run reaching the end of the text"},
		{"\t", 0, 1, "single tab at the end of the text"},
		{"\tx", 0, 2, "optional tab + letter is one split"},
	}
	for _, tc := range tests {
		if got := matchWordPiece(tc.in, tc.at); got != tc.n {
			t.Errorf("matchWordPiece(%q, %d) = %d, want %d (%s)", tc.in, tc.at, got, tc.n, tc.desc)
		}
	}
}

// TestSplitDigits checks the \p{N}{1,3} split.
func TestSplitDigits(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"1", "1"},
		{"123", "123"},
		{"1234", "123|4"},
		{"1234567890", "123|456|789|0"},
		{"a1b22c333d", "a|1|b|22|c|333|d"},
		{"١٢٣٤", "١٢٣|٤"},
		{"½Ⅷ", "½Ⅷ"},
	}
	for _, tc := range tests {
		if got := strings.Join(splitDigits(tc.in), "|"); got != tc.want {
			t.Errorf("splitDigits(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestSplitCJK checks the CJK/Kana split.
func TestSplitCJK(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"中文测试", "中文测试"},
		{"a中b", "a|中|b"},
		{"中文abc日", "中文|abc|日"},
		{"ひらがなカタカナ", "ひらがなカタカナ"},
		{"한국어", "한국어"},
	}
	for _, tc := range tests {
		if got := strings.Join(splitCJK(tc.in), "|"); got != tc.want {
			t.Errorf("splitCJK(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestAddedTokenPrecedence checks that the longest added token wins and that a
// non-normalized (special) token takes precedence over a normalized one.
func TestAddedTokenPrecedence(t *testing.T) {
	tok, err := Load(v41Path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	tests := []struct {
		text string
		want []uint32
	}{
		{"<｜image｜>", []uint32{129264}},
		{"｜DSML｜", []uint32{128825}},
		{"<｜DSML｜ calls>", []uint32{30, 128825, 10699, 32}},
		{"<｜User｜>", []uint32{128803}},
		{"<|image|>", nil}, // ASCII pipes: not an added token, tokenized normally
	}
	for _, tc := range tests {
		got, err := tok.EncodeIDs(tc.text)
		if err != nil {
			t.Fatalf("EncodeIDs(%q): %v", tc.text, err)
		}
		if tc.want != nil && !equalIDs(got, tc.want) {
			t.Errorf("EncodeIDs(%q) = %v, want %v", tc.text, got, tc.want)
		}
		if tc.want == nil && len(got) < 2 {
			t.Errorf("EncodeIDs(%q) = %v, want a multi-token encoding", tc.text, got)
		}
	}
}

// TestDecodeUnknownAndSpecial checks decoding of unknown ids, of ids that are
// both model and added tokens, and of the skip_special_tokens flag.
func TestDecodeUnknownAndSpecial(t *testing.T) {
	tok, err := Load(v41Path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	tests := []struct {
		ids  []uint32
		skip bool
		want string
	}{
		{[]uint32{19923}, false, "Hello"},
		{[]uint32{19923}, true, "Hello"},
		{[]uint32{999999}, false, ""},
		{[]uint32{999999, 19923}, false, "Hello"},
		{[]uint32{0}, false, "<｜begin▁of▁sentence｜>"},
		{[]uint32{0}, true, ""},
		{[]uint32{0, 19923, 1}, false, "<｜begin▁of▁sentence｜>Hello<｜end▁of▁sentence｜>"},
		{[]uint32{0, 19923, 1}, true, "Hello"},
		{[]uint32{128803}, true, "<｜User｜>"}, // not flagged special
		{[]uint32{129264}, true, ""},         // <｜image｜> is special
		{nil, false, ""},
	}
	for _, tc := range tests {
		got, err := tok.DecodeIDs(tc.ids, tc.skip)
		if err != nil {
			t.Fatalf("DecodeIDs(%v, %v): %v", tc.ids, tc.skip, err)
		}
		if got != tc.want {
			t.Errorf("DecodeIDs(%v, skip=%v) = %q, want %q", tc.ids, tc.skip, got, tc.want)
		}
	}
}

// TestByteLevelDecodeFallback checks the per-token fallback of the ByteLevel
// decoder: a token containing a character outside the byte-level alphabet is
// emitted verbatim, while ordinary tokens are byte-decoded.
func TestByteLevelDecodeFallback(t *testing.T) {
	tests := []struct {
		token string
		want  string
	}{
		{"Hello", "Hello"},
		{"ĠHello", " Hello"},
		{"<｜begin▁of▁sentence｜>", "<｜begin▁of▁sentence｜>"},
		{"Ġ<｜User｜>", "Ġ<｜User｜>"}, // unmapped rune: the whole token is verbatim
		{"čĊ", "\r\n"},
	}
	for _, tc := range tests {
		got := rustLossyUTF8(byteLevelDecodeToken(nil, tc.token))
		if got != tc.want {
			t.Errorf("decodeToken(%q) = %q, want %q", tc.token, got, tc.want)
		}
	}
}

// TestRustLossyUTF8 checks the maximal-subpart replacement behaviour of Rust's
// String::from_utf8_lossy, which differs from Go's strings.ToValidUTF8.
func TestRustLossyUTF8(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"hello", "hello"},
		{"\xe2\x82\x28", "\uFFFD("},            // 3-byte prefix invalid at the third byte
		{"\xe2\x28\xa1", "\uFFFD(\uFFFD"},      // invalid continuation right away
		{"\xe2\x82", "\uFFFD"},                 // truncated sequence at the end
		{"\x80\x80", "\uFFFD\uFFFD"},           // lone continuation bytes
		{"a\xffb", "a\uFFFDb"},                 // invalid byte
		{"\xed\xa0\x80", "\uFFFD\uFFFD\uFFFD"}, // UTF-8 encoded surrogate
		{"\xf0\x9f\x98\x80", "😀"},
	}
	for _, tc := range tests {
		if got := rustLossyUTF8([]byte(tc.in)); got != tc.want {
			t.Errorf("rustLossyUTF8(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestLoadRejectsUnsupportedConfigurations checks that a pipeline this package
// does not implement is rejected instead of silently mistokenized.
func TestLoadRejectsUnsupportedConfigurations(t *testing.T) {
	data, err := os.ReadFile(v41Path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(data)
	tests := []struct {
		name string
		from string
		to   string
	}{
		{"normalizer", "\"normalizers\": []", "\"normalizers\": [{\"type\": \"Lowercase\"}]"},
		{"model", "\"type\": \"BPE\"", "\"type\": \"WordPiece\""},
		{"pretokenizer", "\"behavior\": \"Isolated\"", "\"behavior\": \"Removed\""},
	}
	for _, tc := range tests {
		if !strings.Contains(text, tc.from) {
			t.Fatalf("%s: fixture %q not found in tokenizer.json", tc.name, tc.from)
		}
		broken := strings.Replace(text, tc.from, tc.to, 1)
		if _, err := LoadBytes([]byte(broken)); err == nil {
			t.Errorf("%s: LoadBytes accepted an unsupported configuration", tc.name)
		}
	}
}

// TestLoadPerformance is a coarse guard: the 6.3 MB file must load quickly and a
// 2 KB prompt must encode in well under 100 ms. The bounds are deliberately
// generous so that the test does not flake on loaded machines; the benchmarks
// report the real numbers.
func TestLoadPerformance(t *testing.T) {
	start := time.Now()
	tok, err := Load(v41Path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	load := time.Since(start)
	if load > 5*time.Second {
		t.Errorf("Load took %v, want at most a few seconds", load)
	}
	prompt := strings.Repeat("The quick brown fox jumps over the lazy dog. 你好，世界！😀 1234567890 ", 32)
	if len(prompt) < 2048 {
		t.Fatalf("prompt is only %d bytes", len(prompt))
	}
	start = time.Now()
	ids, err := tok.EncodeIDs(prompt)
	if err != nil {
		t.Fatalf("EncodeIDs: %v", err)
	}
	encode := time.Since(start)
	if encode > 100*time.Millisecond {
		t.Errorf("encoding %d bytes took %v, want well under 100ms", len(prompt), encode)
	}
	t.Logf("loaded in %v, encoded %d bytes to %d ids in %v", load, len(prompt), len(ids), encode)
}

// BenchmarkLoad measures parsing and index construction of tokenizer.json.
func BenchmarkLoad(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := Load(v41Path); err != nil {
			b.Fatal(err)
		}
	}
}

// TestConcurrentUse verifies that a loaded Tokenizer is safe for concurrent
// encoding and decoding. Run with -race to make the test meaningful.
func TestConcurrentUse(t *testing.T) {
	tok, err := Load(v41Path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	const workers = 8
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				ids, err := tok.EncodeIDs("Hello 世界 😀 1234 <｜User｜>")
				if err != nil {
					t.Errorf("EncodeIDs: %v", err)
					return
				}
				if _, err := tok.DecodeIDs(ids, false); err != nil {
					t.Errorf("DecodeIDs: %v", err)
					return
				}
				if _, ok := tok.IDForToken("Hello"); !ok {
					t.Errorf("IDForToken(Hello) not found")
					return
				}
				_ = tok.VocabSize()
			}
		}(w)
	}
	wg.Wait()
}

// BenchmarkEncode2KB measures encoding of a mixed 2 KB prompt.
func BenchmarkEncode2KB(b *testing.B) {
	tok, err := Load(v41Path)
	if err != nil {
		b.Fatal(err)
	}
	prompt := strings.Repeat("The quick brown fox jumps over the lazy dog. 你好，世界！😀 1234567890 ", 32)
	b.SetBytes(int64(len(prompt)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := tok.EncodeIDs(prompt); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEncodeASCII measures encoding of pure ASCII text.
func BenchmarkEncodeASCII(b *testing.B) {
	tok, err := Load(v41Path)
	if err != nil {
		b.Fatal(err)
	}
	prompt := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 48)
	b.SetBytes(int64(len(prompt)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := tok.EncodeIDs(prompt); err != nil {
			b.Fatal(err)
		}
	}
}
