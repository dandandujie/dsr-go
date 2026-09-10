package core

import (
	"fmt"
	"math"
)

// ResizeResult is the result of fitting one image into the token budget.
type ResizeResult struct {
	// NLLMH is the number of token rows in the model token grid.
	NLLMH int
	// NLLMW is the number of token columns in the model token grid.
	NLLMW int
	// BestHeight is the resized image height in pixels.
	BestHeight int
	// BestWidth is the resized image width in pixels.
	BestWidth int
	// NumTokens is the total number of model tokens for the image.
	NumTokens int
}

// CalcResizeError reports that fitting an image into the token budget failed.
type CalcResizeError struct {
	// MaxIter is the iteration limit that was reached.
	MaxIter int
	// LastResult is the last resize result before giving up.
	LastResult ResizeResult
}

func (e *CalcResizeError) Error() string {
	return fmt.Sprintf(
		"image resize did not converge after %d iterations, last result: %+v",
		e.MaxIter, e.LastResult,
	)
}

// ImageTokenSpec is the DeepSeek V4.1 image token specification.
//
// The specification maps an image size to the number of model tokens the image
// occupies. V4.1 uses 14-pixel patches and a spatial downsample ratio of 3.
// Images with a positive area below 544 * 544 pixels are upscaled before patch
// alignment and token-budget fitting. This is an area target, not a minimum for
// each dimension; token-budget fitting can reduce it further.
type ImageTokenSpec struct {
	patchSize       int
	downsampleRatio int
	maxNToken       int
	minPixels       int
}

// V41ImageTokenSpec returns the specification of DeepSeek V4.1 image tokens.
//
// Images are divided into 14-pixel patches, downsampled by 3 in each dimension,
// and limited to 1024 model tokens. Images with a positive area below
// 544 * 544 pixels are upscaled before alignment and budget fitting.
func V41ImageTokenSpec() ImageTokenSpec {
	return ImageTokenSpec{
		patchSize:       14,
		downsampleRatio: 3,
		maxNToken:       1024,
		minPixels:       544 * 544,
	}
}

// MaxNToken returns the maximum number of model tokens one image may occupy.
func (s ImageTokenSpec) MaxNToken() int { return s.maxNToken }

// PatchSize returns the pixel size of one patch.
func (s ImageTokenSpec) PatchSize() int { return s.patchSize }

// DownsampleRatio returns the spatial downsampling ratio between the patch grid
// and the model token grid.
func (s ImageTokenSpec) DownsampleRatio() int { return s.downsampleRatio }

// CalcTokenLen returns the model tokens one image of width x height pixels
// occupies.
func (s ImageTokenSpec) CalcTokenLen(width, height int) (int, error) {
	result, err := s.CalcResize(width, height)
	if err != nil {
		return 0, err
	}
	return result.NumTokens, nil
}

// IsImageValid reports whether token-budget fitting leaves width x height
// unchanged. It checks dimensions only; it does not inspect encoded bytes.
func (s ImageTokenSpec) IsImageValid(width, height int) bool {
	result, err := s.CalcResize(width, height)
	if err != nil {
		return false
	}
	return result.BestWidth == width && result.BestHeight == height
}

// CalcResize returns the target size and token length for an image of
// width x height pixels.
func (s ImageTokenSpec) CalcResize(width, height int) (ResizeResult, error) {
	const maxIter = 10
	result := s.calcResizeOnce(width, height)
	for i := 1; i < maxIter; i++ {
		next := s.calcResizeOnce(result.BestWidth, result.BestHeight)
		if next == result {
			return result, nil
		}
		result = next
	}
	return ResizeResult{}, &CalcResizeError{MaxIter: maxIter, LastResult: result}
}

// calcResizeOnce upscales to the minimum area, aligns to patches, and fits the
// token budget.
func (s ImageTokenSpec) calcResizeOnce(width, height int) ResizeResult {
	currentPixels := width * height
	if currentPixels < s.minPixels && currentPixels > 0 {
		ratio := math.Sqrt(float64(s.minPixels) / float64(currentPixels))
		width = int(float64(width) * ratio)
		height = int(float64(height) * ratio)
	}
	bestWidth := divCeil(width, s.patchSize) * s.patchSize
	bestHeight := divCeil(height, s.patchSize) * s.patchSize
	return s.fitTokenBudget(height, width, bestHeight, bestWidth)
}

// fitTokenBudget reduces bestWidth x bestHeight until the token budget holds.
func (s ImageTokenSpec) fitTokenBudget(height, width, bestHeight, bestWidth int) ResizeResult {
	result := s.resizeResult(bestHeight, bestWidth)
	if result.NumTokens > s.maxNToken {
		result = s.solveResizeRatio(height, width, s.maxNToken)
		if result.NumTokens > s.maxNToken {
			panic("token budget must be satisfied after one solve")
		}
	}
	return result
}

// resizeResult returns the token length of a patch grid of bestHeight x
// bestWidth pixels.
func (s ImageTokenSpec) resizeResult(bestHeight, bestWidth int) ResizeResult {
	nLLMH := divCeil(bestHeight/s.patchSize, s.downsampleRatio)
	nLLMW := divCeil(bestWidth/s.patchSize, s.downsampleRatio)
	return ResizeResult{
		NLLMH:      nLLMH,
		NLLMW:      nLLMW,
		BestHeight: bestHeight,
		BestWidth:  bestWidth,
		NumTokens:  s.calcNumTokens(nLLMH, nLLMW),
	}
}

// calcNumTokens returns the model tokens of a nLLMH x nLLMW token grid.
//
// Each row holds one newline token in addition to its image tokens, and the
// image is delimited by a start and an end token.
func (s ImageTokenSpec) calcNumTokens(nLLMH, nLLMW int) int {
	return nLLMH*(nLLMW+1) + 2
}

// solveResizeRatio solves for the largest aspect-preserving size within
// maxNToken.
func (s ImageTokenSpec) solveResizeRatio(height, width, maxNToken int) ResizeResult {
	ratio := float64(height) / float64(width)
	maxWFloat := math.Sqrt((float64(maxNToken)-2.0)/ratio+0.25) - 0.5
	maxHFloat := maxWFloat * ratio

	var bestHeight, bestWidth int
	switch {
	case maxWFloat < 1.0:
		maxW := 1
		maxH := (maxNToken - 2) / (maxW + 1)
		bestWidth = maxW * s.patchSize * s.downsampleRatio
		bestHeight = maxH * s.patchSize * s.downsampleRatio
	case maxHFloat < 1.0:
		maxH := 1
		maxW := (maxNToken-2)/maxH - 1
		if maxW <= 1 {
			panic("token budget must allow a two-column grid")
		}
		bestWidth = maxW * s.patchSize * s.downsampleRatio
		bestHeight = maxH * s.patchSize * s.downsampleRatio
	default:
		maxW := int(maxWFloat)
		maxH := int(maxHFloat)
		betaW := float64(maxW*s.patchSize*s.downsampleRatio) / float64(width)
		betaH := float64(maxH*s.patchSize*s.downsampleRatio) / float64(height)
		beta := math.Min(betaW, betaH)
		bestWidth = int(float64(width)*beta/float64(s.patchSize)) * s.patchSize
		bestHeight = int(float64(height)*beta/float64(s.patchSize)) * s.patchSize
	}
	return s.resizeResult(bestHeight, bestWidth)
}

func divCeil(a, b int) int {
	if a%b == 0 {
		return a / b
	}
	if (a < 0) != (b < 0) {
		return a/b - 1
	}
	return a/b + 1
}
