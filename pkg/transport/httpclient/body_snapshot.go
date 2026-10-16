package httpclient

import (
	"fmt"
	"io"
	"net/http"
)

// SnapshotResponseBody reads at most maxBytes+1 bytes and restores them before
// the unread tail, including any read error. The extra byte lets a caller
// distinguish an exact-sized body from an overrun. It never closes the body;
// its owner must still close it. The caller owns time/cancellation admission.
// A nil or absent body produces no window. Negative and overflowing limits are
// refused before reading. This is for optional bounded diagnostics, not decoding.
func SnapshotResponseBody(response *http.Response, maxBytes int64) ([]byte, error) {
	if maxBytes < 0 || maxBytes == 1<<63-1 {
		return nil, fmt.Errorf("httpclient: invalid response snapshot limit")
	}
	if response == nil || response.Body == nil || response.Body == http.NoBody {
		return nil, nil
	}
	head, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	response.Body = &joinReadCloser{head: head, tail: response.Body, pendingErr: err}
	return head, err
}
