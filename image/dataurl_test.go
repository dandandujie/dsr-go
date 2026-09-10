package image

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestProcessDataURLDecodesBase64Bodies(t *testing.T) {
	body := []byte{0x00, 0x01, 0xfe, 0xff, 'h', 'i'}
	input := "data:image/webp;base64," + base64.StdEncoding.EncodeToString(body)
	parsed, err := processDataURL(input)
	requireNoError(t, err)
	requireEqual(t, parsed.mediaType, "image/webp", "mediaType")
	requireEqual(t, parsed.base64, true, "base64")

	decoded, err := parsed.decode()
	requireNoError(t, err)
	if !bytes.Equal(decoded, body) {
		t.Fatalf("decode() = %v, want %v", decoded, body)
	}
}

func TestProcessDataURLDecodesPercentEncodedBodies(t *testing.T) {
	parsed, err := processDataURL("data:image/png,%00%01%fe%ff")
	requireNoError(t, err)
	requireEqual(t, parsed.mediaType, "image/png", "mediaType")
	requireEqual(t, parsed.base64, false, "base64")

	decoded, err := parsed.decode()
	requireNoError(t, err)
	if !bytes.Equal(decoded, []byte{0x00, 0x01, 0xfe, 0xff}) {
		t.Fatalf("decode() = %v", decoded)
	}
}

func TestProcessDataURLKeepsAnIncompleteEscape(t *testing.T) {
	parsed, err := processDataURL("data:image/png,a%2")
	requireNoError(t, err)
	decoded, err := parsed.decode()
	requireNoError(t, err)
	requireEqual(t, string(decoded), "a%2", "decode()")
}

func TestProcessDataURLStopsAtTheFragment(t *testing.T) {
	parsed, err := processDataURL("data:image/png;base64,QUJD#fragment")
	requireNoError(t, err)
	decoded, err := parsed.decode()
	requireNoError(t, err)
	requireEqual(t, string(decoded), "ABC", "decode()")
}

func TestProcessDataURLParsesTheSchemeLoosely(t *testing.T) {
	for _, input := range []string{
		"data:image/png;base64,QUJD",
		" DATA:image/png;base64,QUJD ",
		"da\tta:image/png;base64,QUJD",
	} {
		parsed, err := processDataURL(input)
		requireNoError(t, err)
		decoded, err := parsed.decode()
		requireNoError(t, err)
		requireEqual(t, string(decoded), "ABC", "decode() of "+input)
	}
}

func TestProcessDataURLRejectsMalformedInput(t *testing.T) {
	if _, err := processDataURL("image/png;base64,QUJD"); err == nil || err.Error() != "not a valid data url" {
		t.Fatalf("processDataURL without a scheme = %v", err)
	}
	for _, input := range []string{"data:image/png;base64", "data:image/png#fragment,QUJD"} {
		if _, err := processDataURL(input); err == nil ||
			err.Error() != "data url is missing comma delimiting attributes and body" {
			t.Fatalf("processDataURL(%q) = %v", input, err)
		}
	}
}

func TestForgivingBase64Decoding(t *testing.T) {
	tests := []struct {
		name    string
		encoded string
		want    string
		wantErr string
	}{
		{name: "four symbols", encoded: "QUJD", want: "ABC"},
		{name: "two symbols without padding", encoded: "QQ", want: "A"},
		{name: "three symbols without padding", encoded: "QUI", want: "AB"},
		{name: "padding", encoded: "QQ==", want: "A"},
		{name: "whitespace", encoded: "Q U\nJ\tD", want: "ABC"},
		{name: "percent escapes", encoded: "QU%4AD", want: "ABC"},
		{name: "lone symbol", encoded: "Q", wantErr: "lone alphabet symbol present"},
		{name: "incorrect padding", encoded: "QUJD=", wantErr: "incorrect padding"},
		{name: "symbol after padding", encoded: "QQ==A", wantErr: "alphabet symbol present after padding"},
		{name: "unexpected symbol", encoded: "Q!", wantErr: "symbol with codepoint 33 not expected"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed := &dataURL{mediaType: "image/png", base64: true, body: test.encoded}
			decoded, err := parsed.decode()
			if test.wantErr != "" {
				requireEqual(t, err == nil, false, "decode() error")
				requireEqual(t, err.Error(), test.wantErr, "decode() error")
				return
			}
			requireNoError(t, err)
			requireEqual(t, string(decoded), test.want, "decode()")
		})
	}
}

func TestDataURLMediaTypeNormalization(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "data:image/webp;base64,AAAA", want: "image/webp"},
		{input: "data:IMAGE/PNG;base64,AAAA", want: "image/png"},
		{input: "data:,hello", want: defaultDataURLMediaType},
		{input: "data:;base64,AAAA", want: defaultDataURLMediaType},
		{input: "data:;charset=utf-8,x", want: "text/plain;charset=utf-8"},
		{input: "data:image/png;charset=utf-8;base64,AAAA", want: "image/png;charset=utf-8"},
		{input: "data:text/plain;base64,AAAA", want: "text/plain"},
		{input: "data:not a mime,x", want: defaultDataURLMediaType},
		{input: "data:image/,x", want: defaultDataURLMediaType},
		{input: "data:image/png;foo;base64,AAAA", want: defaultDataURLMediaType},
	}
	for _, test := range tests {
		parsed, err := processDataURL(test.input)
		requireNoError(t, err)
		requireEqual(t, parsed.mediaType, test.want, "mediaType of "+test.input)
	}
}

func TestEstimatedDecodedLen(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{input: "data:image/webp;base64,AAAAAAAA", want: 6},
		{input: "data:image/png,%41%42", want: 2},
		{input: "data:image/png,abcd", want: 4},
		{input: "data:image/png", want: 0},
		{input: "data:image/png,%%%", want: 0},
	}
	for _, test := range tests {
		requireEqual(t, estimatedDecodedLen(test.input), test.want, "estimatedDecodedLen("+test.input+")")
	}
}

func TestDecodeDataURLAppliesTheImageLimitBeforeDecoding(t *testing.T) {
	budget := NewImageByteBudget(4, 64, 0)
	_, err := decodeDataURL("data:image/webp;base64,"+strings.Repeat("A", 8), budget)

	imageErr := requireImageError(t, err, KindImageTooLarge)
	requireEqual(t, imageErr.Size, 6, "Size")
	requireEqual(t, imageErr.Max, 4, "Max")
}

func TestDecodeDataURLRejectsUnsupportedMediaTypes(t *testing.T) {
	budget := NewImageByteBudget(1024, 4096, 0)
	_, err := decodeDataURL("data:text/plain;base64,QUJD", budget)

	imageErr := requireImageError(t, err, KindUnsupportedMediaType)
	requireEqual(t, imageErr.Message, "text/plain", "Message")
}

func TestDecodeDataURLRejectsAMalformedBody(t *testing.T) {
	budget := NewImageByteBudget(1024, 4096, 0)
	_, err := decodeDataURL("data:image/png;base64,Q", budget)

	imageErr := requireImageError(t, err, KindInvalidDataURL)
	requireEqual(t, imageErr.Message, "lone alphabet symbol present", "Message")
}

func TestDecodeDataURLReservesTheDecodedBytes(t *testing.T) {
	budget := NewImageByteBudget(64, 4, 0)
	body := base64.StdEncoding.EncodeToString([]byte("abcdef"))
	_, err := decodeDataURL("data:image/png;base64,"+body, budget)

	imageErr := requireImageError(t, err, KindTotalSizeTooLarge)
	requireEqual(t, imageErr.Size, 6, "Size")
	requireEqual(t, imageErr.Max, 4, "Max")
}
