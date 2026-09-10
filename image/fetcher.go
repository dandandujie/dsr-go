package image

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// Settings of the HTTP clients of HTTPImageFetcher. They match the Rust
// crate's ReqwestImageFetcher.
const (
	// defaultConnectTimeout bounds the connection setup of one download.
	defaultConnectTimeout = 10 * time.Second
	// defaultRequestTimeout bounds one download, including reading its body.
	defaultRequestTimeout = 60 * time.Second
	// defaultMaxRedirects is the number of redirects one download follows.
	defaultMaxRedirects = 5
	// fetchChunkSize is the size of one read from a response body.
	fetchChunkSize = 32 * 1024
)

// HTTPImageFetcher downloads images from external URLs with net/http. It
// replaces ReqwestImageFetcher, which the Rust crate builds on reqwest.
//
// Clients created by NewHTTPImageFetcher and NewHTTPImageFetcherWithMaxBytes
// follow at most five redirects and use a ten-second connection timeout and a
// sixty-second request timeout. NewHTTPImageFetcherFromClient preserves the
// supplied client's settings. All constructors enforce the configured response
// body size limit. A response body is read as a stream, and the bytes that are
// kept are reserved from the budget of the resolve call, so a body that would
// exceed the total limit of the request stops the download.
//
// Like the Rust fetcher, HTTPImageFetcher does not filter its targets and does
// not check the media type of a response: the media type is validated from the
// image header during preprocessing. We strongly recommend that callers
// implement SSRF protection, such as rejecting private, loopback, and
// link-local addresses, when callers may supply image URLs.
type HTTPImageFetcher struct {
	// client performs the downloads.
	client *http.Client
	// maxBytes is the encoded size limit of one image.
	maxBytes int
}

// NewHTTPImageFetcher returns a fetcher with the default image size limit.
//
// Unlike the Rust constructor, which reports a client that cannot be
// constructed, this constructor cannot fail.
func NewHTTPImageFetcher() *HTTPImageFetcher {
	return NewHTTPImageFetcherWithMaxBytes(DefaultImageLimits().MaxImageBytes)
}

// NewHTTPImageFetcherWithMaxBytes returns a fetcher with an explicit size limit
// in bytes.
func NewHTTPImageFetcherWithMaxBytes(maxBytes int) *HTTPImageFetcher {
	return NewHTTPImageFetcherFromClient(newDefaultHTTPClient(), maxBytes)
}

// NewHTTPImageFetcherFromClient returns a fetcher that downloads with client and
// applies maxBytes. A nil client uses the default client.
func NewHTTPImageFetcherFromClient(client *http.Client, maxBytes int) *HTTPImageFetcher {
	if client == nil {
		client = newDefaultHTTPClient()
	}
	return &HTTPImageFetcher{client: client, maxBytes: maxBytes}
}

// MaxBytes returns the encoded size limit this fetcher applies to one image.
//
// The resolver can apply a smaller limit through the ImageByteBudget of the
// resolve call.
func (f *HTTPImageFetcher) MaxBytes() int { return f.maxBytes }

// Fetch downloads url and returns its body.
//
// Fetch returns a KindFetchStatus error for a non-success status and a
// KindImageTooLarge error when the body exceeds the smaller of the fetcher
// limit and the budget's image limit. The bytes it keeps are reserved from
// budget before they are kept.
func (f *HTTPImageFetcher) Fetch(ctx context.Context, url string, budget *ImageByteBudget) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, NewFetchError(url, err)
	}
	response, err := f.client.Do(request)
	if err != nil {
		return nil, NewFetchError(url, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, NewFetchStatusError(url, response.StatusCode)
	}

	// The limit of the fetcher and the limit of the resolve call both apply.
	maxBytes := min(f.maxBytes, budget.MaxImageBytes())
	if response.ContentLength > int64(maxBytes) {
		return nil, NewImageTooLargeError(int(response.ContentLength), maxBytes)
	}

	var body []byte
	buffer := make([]byte, fetchChunkSize)
	for {
		read, readErr := response.Body.Read(buffer)
		if read > 0 {
			imageBytes := len(body) + read
			if imageBytes > maxBytes {
				return nil, NewImageTooLargeError(imageBytes, maxBytes)
			}
			if err := budget.Reserve(read); err != nil {
				return nil, err
			}
			body = append(body, buffer[:read]...)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			// A response that fails while its body is read is the retryable
			// failure that can occur after bytes were reserved, so the retry
			// reserves them once.
			budget.Release(len(body))
			return nil, NewFetchError(url, readErr)
		}
	}
	return body, nil
}

// newDefaultHTTPClient returns the client of NewHTTPImageFetcher: a ten-second
// connection timeout, a sixty-second request timeout, and at most five
// redirects.
func newDefaultHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{Timeout: defaultConnectTimeout, KeepAlive: 30 * time.Second}
	transport.DialContext = dialer.DialContext
	transport.TLSHandshakeTimeout = defaultConnectTimeout
	return &http.Client{
		Transport:     transport,
		Timeout:       defaultRequestTimeout,
		CheckRedirect: limitRedirects(defaultMaxRedirects),
	}
}

// limitRedirects returns a redirect policy that follows at most max redirects.
func limitRedirects(maxRedirects int) func(*http.Request, []*http.Request) error {
	return func(_ *http.Request, via []*http.Request) error {
		// via holds the requests that were already made, so the (maxRedirects+1)th
		// redirect is the first one that is refused.
		if len(via) > maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		return nil
	}
}
