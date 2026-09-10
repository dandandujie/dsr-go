package core

import "testing"

// resizeCase is one expected V4.1 preprocessing result:
// width, height, numTokens, bestWidth, bestHeight, nLLMH, nLLMW.
type resizeCase struct {
	width, height         int
	numTokens             int
	bestWidth, bestHeight int
	nLLMH, nLLMW          int
}

var resizeCases = []resizeCase{
	{978, 479, 302, 980, 490, 12, 24},
	{272, 300, 198, 518, 574, 14, 13},
	{516, 517, 184, 546, 546, 13, 13},
	{1091, 397, 272, 1092, 406, 10, 26},
	{743, 870, 401, 756, 882, 21, 18},
	{906, 740, 416, 910, 742, 18, 22},
	{1107, 873, 590, 1120, 882, 21, 27},
	{493, 243, 202, 784, 392, 10, 19},
	{354, 476, 197, 476, 630, 15, 12},
	{766, 642, 322, 770, 644, 16, 19},
	{972, 672, 402, 980, 672, 16, 24},
	{1167, 486, 350, 1176, 490, 12, 28},
	{1134, 389, 282, 1134, 392, 10, 27},
	{275, 346, 197, 490, 616, 15, 12},
	{1033, 141, 187, 1484, 210, 5, 36},
	{125, 751, 226, 224, 1344, 32, 6},
	{560, 1029, 377, 560, 1036, 25, 14},
	{482, 725, 236, 490, 728, 18, 12},
	{782, 879, 422, 784, 882, 21, 19},
	{434, 272, 200, 700, 434, 11, 17},
	{2, 165, 356, 70, 4942, 118, 2},
	{165, 2, 240, 4942, 70, 2, 118},
	{158, 10000, 762, 126, 7966, 190, 3},
	{10000, 158, 962, 10010, 168, 4, 239},
	{40, 50, 197, 490, 616, 15, 12},
	{50, 40, 194, 616, 490, 12, 15},
	{1, 1, 184, 546, 546, 13, 13},
	{100, 100, 184, 546, 546, 13, 13},
	{16809, 11841, 990, 1540, 1092, 26, 37},
	{1920, 330, 378, 1932, 336, 8, 46},
	{8192, 8192, 994, 1302, 1302, 31, 31},
	{4096, 4096, 994, 1302, 1302, 31, 31},
	{1920, 1080, 968, 1708, 966, 23, 41},
	{1080, 1920, 986, 966, 1708, 41, 23},
	{3024, 4032, 1010, 1134, 1512, 36, 27},
	{640, 480, 206, 644, 490, 12, 16},
}

func TestCalcResizeMatchesV41Preprocessing(t *testing.T) {
	spec := V41ImageTokenSpec()
	for _, tc := range resizeCases {
		result, err := spec.CalcResize(tc.width, tc.height)
		if err != nil {
			t.Fatalf("CalcResize(%d, %d): %v", tc.width, tc.height, err)
		}
		want := ResizeResult{
			NLLMH:      tc.nLLMH,
			NLLMW:      tc.nLLMW,
			BestHeight: tc.bestHeight,
			BestWidth:  tc.bestWidth,
			NumTokens:  tc.numTokens,
		}
		if result != want {
			t.Errorf("CalcResize(%d, %d) = %+v, want %+v", tc.width, tc.height, result, want)
		}
		if result.NumTokens > spec.MaxNToken() {
			t.Errorf("(%d, %d) exceeds the budget", tc.width, tc.height)
		}
	}
}

func TestFittedImagesAreValid(t *testing.T) {
	spec := V41ImageTokenSpec()
	for _, tc := range resizeCases {
		if !spec.IsImageValid(tc.bestWidth, tc.bestHeight) {
			t.Errorf("%dx%d is not valid", tc.bestWidth, tc.bestHeight)
		}
	}
}

func TestTokenLenHasFloorAndCeiling(t *testing.T) {
	spec := V41ImageTokenSpec()
	if got, err := spec.CalcTokenLen(1, 1); err != nil || got != 184 {
		t.Errorf("CalcTokenLen(1, 1) = %d, %v; want 184", got, err)
	}
	if got, err := spec.CalcTokenLen(16809, 11841); err != nil || got != 990 {
		t.Errorf("CalcTokenLen(16809, 11841) = %d, %v; want 990", got, err)
	}
}

func TestImageTokenAdjustmentCountsOnePlaceholderPerImage(t *testing.T) {
	spec := V41ImageTokenSpec()
	data := MultiModalData{Images: []ImageInfo{
		{Width: 1920, Height: 1080},
		{Width: 640, Height: 480},
	}}
	a, err := spec.CalcTokenLen(1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	b, err := spec.CalcTokenLen(640, 480)
	if err != nil {
		t.Fatal(err)
	}
	got, err := data.ImageTokenAdjustment()
	if err != nil {
		t.Fatal(err)
	}
	if want := a + b - 2; got != want {
		t.Errorf("ImageTokenAdjustment() = %d, want %d", got, want)
	}
	empty := MultiModalData{}
	if got, err := empty.ImageTokenAdjustment(); err != nil || got != 0 {
		t.Errorf("empty ImageTokenAdjustment() = %d, %v; want 0", got, err)
	}
}

func TestImageMediaTypeFromMIME(t *testing.T) {
	cases := []struct {
		in   string
		want ImageMediaType
		ok   bool
	}{
		{"image/jpeg", ImageJPEG, true},
		{"IMAGE/JPG", ImageJPEG, true},
		{"image/png; charset=utf-8", ImagePNG, true},
		{" image/webp ", ImageWebP, true},
		{"image/gif", ImageGIF, true},
		{"image/bmp", 0, false},
		{"text/plain", 0, false},
		{"image", 0, false},
	}
	for _, tc := range cases {
		got, ok := ImageMediaTypeFromMIME(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("ImageMediaTypeFromMIME(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
