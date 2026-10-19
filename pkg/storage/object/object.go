// Package object provides bounded, read-only object access. Credentials and SDK
// transports are composed by callers. Revisions are opaque conditional validators,
// never content hashes; a failed condition never yields usable bytes.
package object

import (
	"context"
	"errors"
	"fmt"
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

// NormalizeLimits validates ceilings and applies bounded defaults.
func NormalizeLimits(in Limits) (Limits, error) {
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

// Reader is the bounded read capability used by payload-only consumers.
type Reader interface {
	Read(context.Context, ReadRequest) (ReadResult, error)
}

// Bucket is the complete read-only contract. Implementations enforce bounds
// before accumulating bodies.
type Bucket interface {
	// Bounds returns normalized configured ceilings; callers can only lower them.
	Bounds() Limits
	ListPage(context.Context, ListRequest) (Page, error)
	Stat(context.Context, string) (Metadata, error)
	Read(context.Context, ReadRequest) (ReadResult, error)
}

// ListAll stops on repeated tokens, empty advancing pages, and aggregate bounds.
// Failure returns no partial listing. It does not claim snapshot consistency.
func ListAll(ctx context.Context, b Bucket, prefix string, bounds Limits) ([]Metadata, error) {
	l, err := NormalizeLimits(bounds)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// A caller can lower, but never raise, the concrete bucket's configured bounds.
	configured := b.Bounds()
	if configured.MaxObjects > 0 {
		l.MaxObjects = min(l.MaxObjects, configured.MaxObjects)
		l.MaxPages = min(l.MaxPages, configured.MaxPages)
	}
	// A whole-list implementation can preserve bounded single-traversal local IO.
	if whole, ok := b.(interface {
		ListAll(context.Context, string, Limits) ([]Metadata, error)
	}); ok {
		objects, err := whole.ListAll(ctx, prefix, l)
		if err != nil {
			return nil, err
		}
		if len(objects) > l.MaxObjects {
			return nil, ErrListingLimit
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return objects, nil
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

// ReadCurrent reads exact metadata and applies its revision condition when present.
// Consumers that require generation validators must reject absent revisions.
func ReadCurrent(ctx context.Context, b Bucket, key string, maxBytes int64) (Metadata, []byte, error) {
	if err := ctx.Err(); err != nil {
		return Metadata{}, nil, err
	}
	if maxBytes <= 0 || b == nil || key == "" {
		return Metadata{}, nil, ErrInvalid
	}
	meta, err := b.Stat(ctx, key)
	if err != nil {
		return Metadata{}, nil, err
	}
	if err := ctx.Err(); err != nil {
		return Metadata{}, nil, err
	}
	if meta.Key != key || meta.Size < 0 || meta.LastModified.IsZero() {
		return Metadata{}, nil, ErrInvalid
	}
	if meta.Size > maxBytes {
		return Metadata{}, nil, ErrTooLarge
	}
	result, err := b.Read(ctx, ReadRequest{Key: key, Revision: meta.Revision, MaxBytes: maxBytes})
	if err != nil {
		return Metadata{}, nil, err
	}
	if err := ctx.Err(); err != nil {
		return Metadata{}, nil, err
	}
	if int64(len(result.Data)) > maxBytes {
		return Metadata{}, nil, ErrTooLarge
	}
	if !result.Complete || int64(len(result.Data)) != meta.Size {
		return Metadata{}, nil, ErrRevisionChanged
	}
	return meta, result.Data, nil
}
