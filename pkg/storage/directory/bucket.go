package directory

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/formancehq/go-libs/v5/pkg/storage/internal/boundedio"
	"github.com/formancehq/go-libs/v5/pkg/storage/object"
)

// Bucket confines file access to a configured root. Open pins its
// identity until Close; NewBucket opens the configured path per operation.
type Bucket struct {
	root   *os.Root
	limits object.Limits
	path   string
}

// NewBucket preserves path-based composition without retaining a descriptor.
// Each operation opens and closes its root; use Open to pin a root identity.
func NewBucket(root string, bounds object.Limits) (*Bucket, error) {
	l, err := object.NormalizeLimits(bounds)
	if err != nil {
		return nil, err
	}
	return &Bucket{path: filepath.Clean(root), limits: l}, nil
}

func Open(root string, bounds object.Limits) (*Bucket, error) {
	l, err := object.NormalizeLimits(bounds)
	if err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	return &Bucket{root: r, limits: l}, nil
}
func (d *Bucket) Close() error {
	if d.root == nil {
		return nil
	}
	return d.root.Close()
}
func validKey(key string) bool {
	return key != "" && fs.ValidPath(key) && key != "." && !strings.Contains(key, `\`)
}
func revision(info fs.FileInfo) string {
	return fmt.Sprintf("%d:%d", info.ModTime().UnixNano(), info.Size())
}
func metadata(key string, info fs.FileInfo) object.Metadata {
	return object.Metadata{Key: key, Size: info.Size(), LastModified: info.ModTime().UTC(), Revision: revision(info)}
}
func (d *Bucket) Stat(ctx context.Context, key string) (object.Metadata, error) {
	if err := ctx.Err(); err != nil {
		return object.Metadata{}, err
	}
	if d.root == nil {
		opened, err := Open(d.path, d.limits)
		if err != nil {
			return object.Metadata{}, err
		}
		defer func() { _ = opened.Close() }()
		return opened.Stat(ctx, key)
	}
	if !validKey(key) {
		return object.Metadata{}, object.ErrInvalid
	}
	info, err := d.root.Stat(filepath.FromSlash(key))
	if err != nil {
		return object.Metadata{}, err
	}
	if !info.Mode().IsRegular() {
		return object.Metadata{}, object.ErrInvalid
	}
	return metadata(key, info), nil
}
func (d *Bucket) Read(ctx context.Context, req object.ReadRequest) (object.ReadResult, error) {
	if err := ctx.Err(); err != nil {
		return object.ReadResult{}, err
	}
	if d.root == nil {
		opened, err := Open(d.path, d.limits)
		if err != nil {
			return object.ReadResult{}, err
		}
		defer func() { _ = opened.Close() }()
		return opened.Read(ctx, req)
	}
	cap, err := boundedio.ReadBound(req, d.limits.MaxBytes)
	if err != nil {
		return object.ReadResult{}, err
	}
	if !validKey(req.Key) {
		return object.ReadResult{}, object.ErrInvalid
	}
	admitted, err := d.root.Stat(filepath.FromSlash(req.Key))
	if err != nil {
		return object.ReadResult{}, err
	}
	if !admitted.Mode().IsRegular() {
		return object.ReadResult{}, object.ErrInvalid
	}
	f, err := openRead(d.root, filepath.FromSlash(req.Key))
	if err != nil {
		return object.ReadResult{}, err
	}
	defer func() { _ = f.Close() }()
	before, err := f.Stat()
	if err != nil {
		return object.ReadResult{}, err
	}
	if !before.Mode().IsRegular() {
		return object.ReadResult{}, object.ErrInvalid
	}
	if !os.SameFile(admitted, before) {
		return object.ReadResult{}, object.ErrRevisionChanged
	}
	if req.Revision != "" && req.Revision != revision(before) {
		return object.ReadResult{}, object.ErrRevisionChanged
	}
	if req.PrefixBytes == 0 && before.Size() > cap {
		return object.ReadResult{}, object.ErrTooLarge
	}
	var reader io.Reader = f
	if req.PrefixBytes > 0 {
		reader = io.LimitReader(f, req.PrefixBytes)
	}
	data, err := boundedio.ReadBody(ctx, reader, cap)
	if err != nil {
		return object.ReadResult{}, err
	}
	after, err := f.Stat()
	if err != nil {
		return object.ReadResult{}, err
	}
	current, err := d.root.Stat(filepath.FromSlash(req.Key))
	if err != nil {
		return object.ReadResult{}, err
	}
	if !os.SameFile(before, current) || revision(before) != revision(after) || revision(after) != revision(current) {
		return object.ReadResult{}, object.ErrRevisionChanged
	}
	return object.ReadResult{Data: data, Complete: int64(len(data)) == before.Size()}, nil
}

// ListPage uses an opaque positional cursor over directory traversal.
// Each call is bounded; concurrent mutations may change enumeration positions.
// Symlinks are skipped; all opens remain constrained by os.Root.
func (d *Bucket) ListPage(ctx context.Context, req object.ListRequest) (object.Page, error) {
	if err := ctx.Err(); err != nil {
		return object.Page{}, err
	}
	if d.root == nil {
		opened, err := Open(d.path, d.limits)
		if err != nil {
			return object.Page{}, err
		}
		defer func() { _ = opened.Close() }()
		return opened.ListPage(ctx, req)
	}
	if req.Limit <= 0 {
		return object.Page{}, object.ErrInvalid
	}
	req.Limit = min(req.Limit, d.limits.MaxObjects)
	offset := 0
	if req.Cursor != "" {
		n, err := strconv.Atoi(req.Cursor)
		if err != nil || n < 0 || n > d.limits.MaxObjects {
			return object.Page{}, object.ErrInvalid
		}
		offset = n
	}
	out := object.Page{}
	matched, visited := 0, 0
	var walk func(string) error
	walk = func(dir string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		f, err := openRead(d.root, filepath.FromSlash(dir))
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return object.ErrInvalid
		}
		for {
			entries, readErr := f.ReadDir(256)
			for _, entry := range entries {
				if err := ctx.Err(); err != nil {
					return err
				}
				visited++
				if visited > d.limits.MaxObjects {
					return object.ErrListingLimit
				}
				key := path.Join(dir, entry.Name())
				if !validKey(key) {
					continue
				}
				if entry.Type()&fs.ModeSymlink != 0 {
					continue
				}
				if entry.IsDir() {
					if req.Prefix == "" || strings.HasPrefix(req.Prefix, key+"/") || strings.HasPrefix(key+"/", req.Prefix) {
						if err := walk(key); err != nil {
							return err
						}
					}
					continue
				}
				if !strings.HasPrefix(key, req.Prefix) {
					continue
				}
				info, err := entry.Info()
				if err != nil {
					return err
				}
				if !info.Mode().IsRegular() {
					continue
				}
				matched++
				if matched <= offset {
					continue
				}
				if len(out.Objects) == req.Limit {
					out.NextCursor = strconv.Itoa(matched - 1)
					return io.EOF
				}
				out.Objects = append(out.Objects, metadata(key, info))
			}
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			if readErr != nil {
				return readErr
			}
		}
	}
	err := walk(".")
	if err != nil && !errors.Is(err, io.EOF) {
		return object.Page{}, err
	}
	return out, nil
}

// Bounds returns the configured normalized ceilings.
func (b *Bucket) Bounds() object.Limits { return b.limits }

var _ object.Bucket = (*Bucket)(nil)

// ListAll enumerates the root once, preserving bounded admission and avoiding
// a fresh traversal for every remote-style page.
func (d *Bucket) ListAll(ctx context.Context, prefix string, bounds object.Limits) ([]object.Metadata, error) {
	l, err := object.NormalizeLimits(bounds)
	if err != nil {
		return nil, err
	}
	l.MaxObjects = min(l.MaxObjects, d.limits.MaxObjects)
	page, err := d.ListPage(ctx, object.ListRequest{Prefix: prefix, Limit: l.MaxObjects})
	if err != nil {
		return nil, err
	}
	if page.NextCursor != "" {
		return nil, object.ErrListingLimit
	}
	return page.Objects, nil
}
