package httpclient

import (
	"errors"
	"fmt"
	"io"
	"math"
)

// DefaultMaxResponseBody is the limit used by ReadBounded and ReadBoundedWindow
// when the caller supplies a non-positive limit. Applications can supply a
// smaller or larger positive limit for each response.
const DefaultMaxResponseBody = 32 << 20

// ErrResponseTooLarge means the response exceeds the caller's byte limit.
var ErrResponseTooLarge = errors.New("httpclient: response body exceeds limit")

// ReadBounded reads r up to maxBytes plus one detection byte. It returns no data
// if reading fails or the body exceeds the limit. A non-positive limit uses
// DefaultMaxResponseBody. It neither closes r nor interprets HTTP status codes.
func ReadBounded(r io.Reader, maxBytes int64) ([]byte, error) {
	data, exceeded, err := ReadBoundedWindow(r, maxBytes)
	if err != nil {
		return nil, err
	}
	if exceeded {
		return nil, ErrResponseTooLarge
	}
	return data, nil
}

// ReadBoundedWindow retains the bounded read window, including one detection
// byte when the limit is exceeded. This lets diagnostics report the actual
// observed window while rejecting oversized input. Read errors retain partial
// bytes and their original cause; exceeded is false when reading fails. A
// non-positive limit uses DefaultMaxResponseBody. The caller owns closing r.
func ReadBoundedWindow(r io.Reader, maxBytes int64) (data []byte, exceeded bool, err error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxResponseBody
	}
	window := maxBytes
	if window < math.MaxInt64 {
		window++
	}
	data, err = io.ReadAll(io.LimitReader(r, window))
	if err != nil {
		return data, false, fmt.Errorf("httpclient: read body: %w", err)
	}
	return data, int64(len(data)) > maxBytes, nil
}
