// Package object provides bounded, read-only object access. Credentials and SDK
// transports are composed by callers. Revisions are opaque conditional validators,
// never content hashes; a failed condition never yields usable bytes.
package object

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"time"
)

var (
	ErrTooLarge        = errors.New("object: byte limit exceeded")
	ErrRevisionChanged = errors.New("object: revision changed")
	ErrInvalid         = errors.New("object: invalid request")
	ErrListingLimit    = errors.New("object: listing limit exceeded")
)

type Limits struct {
	MaxBytes   int64
	MaxObjects int
	MaxPages   int
}

func limits(in Limits) (Limits, error) {
	if in.MaxBytes < 0 || in.MaxObjects < 0 || in.MaxPages < 0 || in.MaxBytes == math.MaxInt64 {
		return in, ErrInvalid
	}
	if in.MaxBytes == 0 {
		in.MaxBytes = 256 << 20
	}
	if in.MaxObjects == 0 {
		in.MaxObjects = 1_000_000
	}
	if in.MaxPages == 0 {
		in.MaxPages = 10_000
	}
	return in, nil
}

type Metadata struct {
	Key          string    `json:"key"`
	LastModified time.Time `json:"lastModified"`
	Size         int64     `json:"size"`
	Revision     string    `json:"revision,omitempty"`
}

type ListRequest struct {
	Prefix string
	Cursor string
	Limit  int
}

type Page struct {
	Objects    []Metadata
	NextCursor string
	// ServerTime is the uncached remote response time. Zero means unavailable.
	ServerTime time.Time
}

type ReadRequest struct {
	Key      string
	Revision string
	// MaxBytes may lower the configured ceiling. Zero uses that ceiling.
	MaxBytes int64
	// PrefixBytes requests a leading range. Zero requests the entire object.
	PrefixBytes int64
}

type ReadResult struct {
	Data     []byte
	Complete bool
}

// Bucket is the complete read-only contract; optional interface assertions are
// unnecessary. Implementations must enforce bounds before accumulating bodies.
type Bucket interface {
	ListPage(context.Context, ListRequest) (Page, error)
	Stat(context.Context, string) (Metadata, error)
	Read(context.Context, ReadRequest) (ReadResult, error)
}

func readBound(req ReadRequest, cap int64) (int64, error) {
	if req.Key == "" || req.MaxBytes < 0 || req.PrefixBytes < 0 {
		return 0, ErrInvalid
	}
	if req.MaxBytes > 0 {
		cap = min(cap, req.MaxBytes)
	}
	if req.PrefixBytes > cap {
		return 0, ErrTooLarge
	}
	if req.PrefixBytes > 0 {
		cap = req.PrefixBytes
	}
	return cap, nil
}

func readBody(ctx context.Context, r io.Reader, cap int64) ([]byte, error) {
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
		return nil, ErrTooLarge
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

// ListAll stops on repeated tokens, empty advancing pages, and aggregate bounds.
// Failure returns no partial listing. It does not claim snapshot consistency.
func ListAll(ctx context.Context, b Bucket, prefix string, bounds Limits) ([]Metadata, error) {
	l, err := limits(bounds)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, ErrInvalid
	}
	out := []Metadata{}
	cursor := ""
	seen := map[string]bool{}
	for n := 0; n < l.MaxPages; n++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(out) == l.MaxObjects {
			return nil, ErrListingLimit
		}
		page, err := b.ListPage(ctx, ListRequest{Prefix: prefix, Cursor: cursor, Limit: min(1000, l.MaxObjects-len(out))})
		if err != nil {
			return nil, err
		}
		if len(page.Objects) > l.MaxObjects-len(out) {
			return nil, ErrListingLimit
		}
		out = append(out, page.Objects...)
		if page.NextCursor == "" {
			return out, nil
		}
		if seen[page.NextCursor] {
			return nil, fmt.Errorf("%w: repeated cursor", ErrListingLimit)
		}
		seen[page.NextCursor] = true
		cursor = page.NextCursor
	}
	return nil, ErrListingLimit
}

// ReadCurrent pins the read to exact metadata and rejects changed generations.
func ReadCurrent(ctx context.Context, b Bucket, key string, maxBytes int64) (Metadata, []byte, error) {
	if maxBytes <= 0 || b == nil {
		return Metadata{}, nil, ErrInvalid
	}
	meta, err := b.Stat(ctx, key)
	if err != nil {
		return Metadata{}, nil, err
	}
	if meta.Key != key || meta.Size < 0 || meta.LastModified.IsZero() || meta.Revision == "" {
		return Metadata{}, nil, ErrInvalid
	}
	if meta.Size > maxBytes {
		return Metadata{}, nil, ErrTooLarge
	}
	result, err := b.Read(ctx, ReadRequest{Key: key, Revision: meta.Revision, MaxBytes: maxBytes})
	if err != nil {
		return Metadata{}, nil, err
	}
	if !result.Complete || int64(len(result.Data)) != meta.Size {
		return Metadata{}, nil, ErrRevisionChanged
	}
	return meta, result.Data, nil
}
