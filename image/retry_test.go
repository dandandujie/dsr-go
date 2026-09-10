package image

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dandandujie/dsr-go/core"
)

func TestRetryPolicyMaxAttempts(t *testing.T) {
	requireEqual(t, NoRetryPolicy().MaxAttempts(), 1, "NoRetryPolicy().MaxAttempts()")
	requireEqual(t, DownloadRetryPolicy().MaxAttempts(), 4, "DownloadRetryPolicy().MaxAttempts()")
	requireEqual(t, NewRetryPolicy([]time.Duration{time.Second}).MaxAttempts(), 2, "NewRetryPolicy(...).MaxAttempts()")
}

func TestNewRetryPolicyCopiesItsDelays(t *testing.T) {
	delays := []time.Duration{time.Millisecond}
	policy := NewRetryPolicy(delays)
	delays[0] = time.Hour
	requireEqual(t, policy.delays[0], time.Millisecond, "policy.delays[0]")
}

func TestExecuteRetryAttemptsUntilSuccess(t *testing.T) {
	const failures = 2
	policy := NewRetryPolicy([]time.Duration{time.Millisecond, time.Millisecond})
	lastAttempt := -1
	value, err := ExecuteRetry(context.Background(), policy, func(attempt int) (string, error) {
		lastAttempt = attempt
		if attempt < failures {
			return "", NewFetchStatusError("https://example.com/image.webp", 503)
		}
		return "ok", nil
	}, retryableImageError)
	requireNoError(t, err)
	requireEqual(t, value, "ok", "value")
	requireEqual(t, lastAttempt, failures, "the attempt index of the successful attempt")
}

func TestExecuteRetryStopsOnANonRetryableError(t *testing.T) {
	calls := 0
	policy := NewRetryPolicy([]time.Duration{time.Millisecond, time.Millisecond})
	_, err := ExecuteRetry(context.Background(), policy, func(int) (string, error) {
		calls++
		return "", NewFetchStatusError("https://example.com/image.webp", 404)
	}, retryableImageError)
	requireImageError(t, err, KindFetchStatus)
	requireEqual(t, calls, 1, "calls")
}

func TestExecuteRetryReturnsTheLastError(t *testing.T) {
	calls := 0
	policy := NewRetryPolicy([]time.Duration{time.Millisecond, time.Millisecond})
	_, err := ExecuteRetry(context.Background(), policy, func(int) (string, error) {
		calls++
		return "", NewFetchError("https://example.com/image.webp", errors.New("boom"))
	}, retryableImageError)
	requireImageError(t, err, KindFetch)
	requireEqual(t, calls, 3, "calls")
}

// transientFetcher fails with a retryable status a fixed number of times.
type transientFetcher struct {
	failures int
	status   int
	calls    atomic.Int64
	body     []byte
}

// Fetch returns the configured body after the configured number of failures.
func (f *transientFetcher) Fetch(context.Context, string, *ImageByteBudget) ([]byte, error) {
	if int(f.calls.Add(1)) <= f.failures {
		status := f.status
		if status == 0 {
			status = 503
		}
		return nil, NewFetchStatusError("https://example.com/image.webp", status)
	}
	return f.body, nil
}

func TestTheResolverRetriesImageDownloads(t *testing.T) {
	fetcher := &transientFetcher{failures: 2, body: []byte{1, 2, 3, 4}}
	resolver := NewImageResolver(fetcher, acceptingPreprocessor{}).
		WithLimits(limits(8, 64)).
		WithRetry(NewRetryPolicy([]time.Duration{time.Millisecond, time.Millisecond}))
	quota := NewImageQuota()
	data, err := resolver.Resolve([]core.ImageSource{urlSource()}, quota)
	requireNoError(t, err)

	requireEqual(t, len(data.Images), 1, "len(Images)")
	requireEqual(t, fetcher.calls.Load(), int64(3), "calls")
	requireEqual(t, quota.ByteSize(), 4, "ByteSize()")
}

func TestTheResolverDoesNotRetryAClientStatus(t *testing.T) {
	fetcher := &transientFetcher{failures: 1, status: 404, body: []byte{1}}
	resolver := NewImageResolver(fetcher, acceptingPreprocessor{}).WithLimits(limits(8, 64))
	_, err := resolver.Resolve([]core.ImageSource{urlSource()}, NewImageQuota())
	requireImageError(t, err, KindFetchStatus)

	requireEqual(t, fetcher.calls.Load(), int64(1), "calls")
}

// errorFetcher returns an error that is not an ImageError.
type errorFetcher struct{}

// Fetch always fails.
func (errorFetcher) Fetch(context.Context, string, *ImageByteBudget) ([]byte, error) {
	return nil, errors.New("network down")
}

func TestFetcherErrorsAreWrapped(t *testing.T) {
	resolver := NewImageResolver(errorFetcher{}, acceptingPreprocessor{}).
		WithLimits(limits(8, 64)).
		WithRetry(NoRetryPolicy())
	_, err := resolver.Resolve([]core.ImageSource{urlSource()}, NewImageQuota())

	imageErr := requireImageError(t, err, KindFetch)
	requireEqual(t, imageErr.URL, "https://example.com/image.webp", "URL")
	requireEqual(t, imageErr.Message, "network down", "Message")
}
