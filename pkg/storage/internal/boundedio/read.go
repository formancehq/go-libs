package boundedio

import (
	"context"
	"io"

	"github.com/formancehq/go-libs/v5/pkg/storage/object"
)

// ReadBound applies a per-read bound without raising the configured byte limit.
func ReadBound(req object.ReadRequest, cap int64) (int64, error) {
	if req.Key == "" || req.MaxBytes < 0 || req.PrefixBytes < 0 {
		return 0, object.ErrInvalid
	}
	if req.MaxBytes > 0 {
		cap = min(cap, req.MaxBytes)
	}
	if req.PrefixBytes > cap {
		return 0, object.ErrTooLarge
	}
	if req.PrefixBytes > 0 {
		cap = req.PrefixBytes
	}
	return cap, nil
}

// ReadBody admits at most cap bytes and returns no data on failure.
func ReadBody(ctx context.Context, r io.Reader, cap int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, r: r}, cap+1))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if int64(len(b)) > cap {
		return nil, object.ErrTooLarge
	}
	return b, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
