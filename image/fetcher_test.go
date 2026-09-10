package image

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestFetchReturnsTheBodyAndReservesTheBudget(t *testing.T) {
	body := []byte{1, 2, 3, 4}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(body)
	}))
	defer server.Close()

	fetcher := NewHTTPImageFetcherWithMaxBytes(1024)
	budget := NewImageByteBudget(1024, 4, 0)
	fetched, err := fetcher.Fetch(context.Background(), server.URL, budget)
	requireNoError(t, err)
	if !bytes.Equal(fetched, body) {
		t.Fatalf("Fetch() = %v, want %v", fetched, body)
	}
	// The four bytes of the body were reserved from the budget.
	requireImageError(t, budget.Reserve(1), KindTotalSizeTooLarge)
}

func TestFetchReturnsAnEmptyBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	fetcher := NewHTTPImageFetcherWithMaxBytes(1024)
	fetched, err := fetcher.Fetch(context.Background(), server.URL, NewImageByteBudget(1024, 4096, 0))
	requireNoError(t, err)
	requireEqual(t, len(fetched), 0, "len(Fetch())")
}

func TestFetchRejectsANonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	fetcher := NewHTTPImageFetcherWithMaxBytes(1024)
	_, err := fetcher.Fetch(context.Background(), server.URL, NewImageByteBudget(1024, 4096, 0))
	imageErr := requireImageError(t, err, KindFetchStatus)
	requireEqual(t, imageErr.Status, 404, "Status")
	requireEqual(t, imageErr.URL, server.URL, "URL")
	requireEqual(t, imageErr.IsRetryable(), false, "IsRetryable()")
	requireEqual(t, err.Error(),
		"failed to fetch image from "+server.URL+": unexpected status 404", "Error()")
}

func TestFetchRejectsABodyOverTheContentLengthLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(make([]byte, 10))
	}))
	defer server.Close()

	fetcher := NewHTTPImageFetcherWithMaxBytes(1024)
	_, err := fetcher.Fetch(context.Background(), server.URL, NewImageByteBudget(4, 4096, 0))
	imageErr := requireImageError(t, err, KindImageTooLarge)
	requireEqual(t, imageErr.Size, 10, "Size")
	requireEqual(t, imageErr.Max, 4, "Max")
}

func TestFetchRejectsAChunkedBodyOverTheLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		w.Write(make([]byte, 10))
	}))
	defer server.Close()

	fetcher := NewHTTPImageFetcherWithMaxBytes(1024)
	_, err := fetcher.Fetch(context.Background(), server.URL, NewImageByteBudget(4, 4096, 0))
	imageErr := requireImageError(t, err, KindImageTooLarge)
	requireEqual(t, imageErr.Max, 4, "Max")
	if imageErr.Size <= 4 {
		t.Fatalf("Size = %d, want more than the limit of 4", imageErr.Size)
	}
}

func TestFetchHonorsTheSmallerLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(make([]byte, 10))
	}))
	defer server.Close()

	// The fetcher limit is smaller than the budget limit.
	fetcher := NewHTTPImageFetcherWithMaxBytes(4)
	_, err := fetcher.Fetch(context.Background(), server.URL, NewImageByteBudget(1024, 4096, 0))
	imageErr := requireImageError(t, err, KindImageTooLarge)
	requireEqual(t, imageErr.Max, 4, "Max")
	requireEqual(t, fetcher.MaxBytes(), 4, "MaxBytes()")
}

func TestFetchReleasesTheBudgetWhenTheBodyReadFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		w.Write(make([]byte, 10))
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}))
	defer server.Close()

	fetcher := NewHTTPImageFetcherWithMaxBytes(1024)
	budget := NewImageByteBudget(100, 100, 0)
	_, err := fetcher.Fetch(context.Background(), server.URL, budget)
	requireImageError(t, err, KindFetch)

	// The failed attempt released the ten bytes it had reserved.
	requireNoError(t, budget.Reserve(100))
}

func TestFetchFollowsAtMostFiveRedirects(t *testing.T) {
	body := []byte{9, 8, 7}
	redirects := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/"))
		if err != nil {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		if index >= redirects {
			w.Write(body)
			return
		}
		http.Redirect(w, r, "/"+strconv.Itoa(index+1), http.StatusFound)
	}))
	defer server.Close()

	fetcher := NewHTTPImageFetcherWithMaxBytes(1024)
	for _, test := range []struct {
		redirects int
		wantBody  bool
	}{
		{redirects: 5, wantBody: true},
		{redirects: 6, wantBody: false},
	} {
		redirects = test.redirects
		fetched, err := fetcher.Fetch(context.Background(), server.URL+"/0", NewImageByteBudget(1024, 4096, 0))
		if test.wantBody {
			requireNoError(t, err)
			if !bytes.Equal(fetched, body) {
				t.Fatalf("Fetch() = %v, want %v", fetched, body)
			}
			continue
		}
		requireImageError(t, err, KindFetch)
	}
}

func TestFetchUsesTheDefaultClientForANilClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte{1})
	}))
	defer server.Close()

	fetcher := NewHTTPImageFetcherFromClient(nil, 16)
	requireEqual(t, fetcher.MaxBytes(), 16, "MaxBytes()")
	fetched, err := fetcher.Fetch(context.Background(), server.URL, NewImageByteBudget(16, 4096, 0))
	requireNoError(t, err)
	requireEqual(t, len(fetched), 1, "len(Fetch())")
}

func TestFetchReportsACancelledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte{1})
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fetcher := NewHTTPImageFetcherWithMaxBytes(16)
	_, err := fetcher.Fetch(ctx, server.URL, NewImageByteBudget(16, 4096, 0))
	requireImageError(t, err, KindFetch)
}
