package image

import (
	"context"
	"slices"
	"time"
)

// Default retry delays of DownloadRetryPolicy.
const (
	// DefaultDownloadRetryDelay0 is the first retry delay, 300 ms.
	DefaultDownloadRetryDelay0 = 300 * time.Millisecond
	// DefaultDownloadRetryDelay1 is the second retry delay, 600 ms.
	DefaultDownloadRetryDelay1 = 600 * time.Millisecond
	// DefaultDownloadRetryDelay2 is the third retry delay, 1 s.
	DefaultDownloadRetryDelay2 = time.Second
)

// RetryPolicy holds the delays applied before repeated attempts of an
// operation.
//
// A policy with n delays performs at most n + 1 attempts.
//
// The zero value performs one attempt, like NoRetryPolicy.
type RetryPolicy struct {
	delays []time.Duration
}

// NewRetryPolicy returns a policy that waits delays between attempts.
func NewRetryPolicy(delays []time.Duration) RetryPolicy {
	return RetryPolicy{delays: slices.Clone(delays)}
}

// NoRetryPolicy returns a policy that performs one attempt.
func NoRetryPolicy() RetryPolicy { return RetryPolicy{} }

// DownloadRetryPolicy returns the policy used for image downloads: one retry
// after 0.3 seconds, one after 0.6 seconds, and one after 1.0 second.
func DownloadRetryPolicy() RetryPolicy {
	return RetryPolicy{delays: []time.Duration{
		DefaultDownloadRetryDelay0,
		DefaultDownloadRetryDelay1,
		DefaultDownloadRetryDelay2,
	}}
}

// MaxAttempts returns the maximum number of attempts.
func (p RetryPolicy) MaxAttempts() int { return len(p.delays) + 1 }

// ExecuteRetry runs operation until it succeeds, fails without retrying, or
// runs out of attempts.
//
// retryable decides whether a failure starts another attempt. The attempt
// passed to operation is the zero-based attempt index. ExecuteRetry returns the
// error of the final attempt.
//
// ExecuteRetry is a package-level function rather than a method of RetryPolicy
// because Go methods cannot declare type parameters.
//
// The ctx parameter adapts the Rust method to Go: a cancellation during a retry
// delay returns ctx.Err() instead of the operation's error.
func ExecuteRetry[T any](
	ctx context.Context,
	policy RetryPolicy,
	operation func(attempt int) (T, error),
	retryable func(error) bool,
) (T, error) {
	var zero T
	attempt := 0
	for {
		value, err := operation(attempt)
		if err == nil {
			return value, nil
		}
		if attempt >= len(policy.delays) {
			return zero, err
		}
		if !retryable(err) {
			return zero, err
		}
		if err := sleepContext(ctx, policy.delays[attempt]); err != nil {
			return zero, err
		}
		attempt++
	}
}

// sleepContext waits for delay, or returns ctx.Err() when ctx is done first.
func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
