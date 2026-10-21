package objectio

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/formancehq/go-libs/v5/pkg/storage/internal/boundedio"
	"github.com/formancehq/go-libs/v5/pkg/storage/object"
)

// ReadResponse validates a leading range rather than trusting its byte count.
func ReadResponse(ctx context.Context, body io.Reader, length int64, contentRange string, req object.ReadRequest, cap int64) (object.ReadResult, error) {
	if length < 0 {
		return object.ReadResult{}, object.ErrInvalid
	}
	if length > cap {
		return object.ReadResult{}, object.ErrTooLarge
	}
	data, err := boundedio.ReadBody(ctx, body, cap)
	if err != nil {
		return object.ReadResult{}, err
	}
	if int64(len(data)) != length {
		return object.ReadResult{}, io.ErrUnexpectedEOF
	}
	complete := true
	if contentRange != "" {
		var last, total int64
		if !strings.HasPrefix(contentRange, "bytes 0-") {
			return object.ReadResult{}, object.ErrInvalid
		}
		parts := strings.Split(strings.TrimPrefix(contentRange, "bytes 0-"), "/")
		if len(parts) != 2 {
			return object.ReadResult{}, object.ErrInvalid
		}
		last, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return object.ReadResult{}, object.ErrInvalid
		}
		total, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || total < 0 || last < 0 || last >= total || last != int64(len(data))-1 {
			return object.ReadResult{}, object.ErrInvalid
		}
		complete = last == total-1
	} else if req.PrefixBytes > 0 && int64(len(data)) == req.PrefixBytes {
		// A full-size body without Content-Range could be an unqualified prefix.
		complete = false
	}
	if req.PrefixBytes == 0 && !complete {
		return object.ReadResult{}, object.ErrInvalid
	}
	return object.ReadResult{Data: data, Complete: complete}, nil
}

func ServerTime(raw *http.Response) time.Time {
	if raw == nil || raw.Header.Get("Age") != "" {
		return time.Time{}
	}
	dates := raw.Header.Values("Date")
	if len(dates) != 1 {
		return time.Time{}
	}
	t, err := http.ParseTime(dates[0])
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}
