package image

import (
	"errors"
	"testing"
)

// requireImageError asserts that err is an ImageError of the given kind and
// returns it.
func requireImageError(t *testing.T, err error, kind ImageErrorKind) *ImageError {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error of kind %s, got nil", kind)
	}
	var imageErr *ImageError
	if !errors.As(err, &imageErr) {
		t.Fatalf("expected an *ImageError, got %T: %v", err, err)
	}
	if imageErr.Kind != kind {
		t.Fatalf("expected an error of kind %s, got %s: %v", kind, imageErr.Kind, err)
	}
	return imageErr
}

// requireNoError fails the test when err is not nil.
func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// requireEqual fails the test when got differs from want.
func requireEqual[T comparable](t *testing.T, got, want T, what string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
}
