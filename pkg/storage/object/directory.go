package object

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
)

// Directory confines file access to a configured root. OpenDirectory pins its
// identity until Close; NewDirectory opens the configured path per operation.
type Directory struct {
	root   *os.Root
	limits Limits
	path   string
}

// NewDirectory preserves path-based composition without retaining a descriptor.
// Each operation opens and closes its root; use OpenDirectory to pin a root identity.
func NewDirectory(root string, bounds Limits) (*Directory, error) {
	l, err := limits(bounds)
	if err != nil {
		return nil, err
	}
	return &Directory{path: filepath.Clean(root), limits: l}, nil
}

func OpenDirectory(root string, bounds Limits) (*Directory, error) {
	l, err := limits(bounds)
	if err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	return &Directory{root: r, limits: l}, nil
}
func (d *Directory) Close() error {
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
func metadata(key string, info fs.FileInfo) Metadata {
	return Metadata{Key: key, Size: info.Size(), LastModified: info.ModTime().UTC(), Revision: revision(info)}
}
func (d *Directory) Stat(ctx context.Context, key string) (Metadata, error) {
	if d.root == nil {
		opened, err := OpenDirectory(d.path, d.limits)
		if err != nil {
			return Metadata{}, err
		}
		defer func() { _ = opened.Close() }()
		return opened.Stat(ctx, key)
	}
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	if !validKey(key) {
		return Metadata{}, ErrInvalid
	}
	info, err := d.root.Stat(filepath.FromSlash(key))
	if err != nil {
		return Metadata{}, err
	}
	if !info.Mode().IsRegular() {
		return Metadata{}, ErrInvalid
	}
	return metadata(key, info), nil
}
func (d *Directory) Read(ctx context.Context, req ReadRequest) (ReadResult, error) {
	if d.root == nil {
		opened, err := OpenDirectory(d.path, d.limits)
		if err != nil {
			return ReadResult{}, err
		}
		defer func() { _ = opened.Close() }()
		return opened.Read(ctx, req)
	}
	if err := ctx.Err(); err != nil {
		return ReadResult{}, err
	}
	cap, err := readBound(req, d.limits.MaxBytes)
	if err != nil {
		return ReadResult{}, err
	}
	if !validKey(req.Key) {
		return ReadResult{}, ErrInvalid
	}
	admitted, err := d.root.Stat(filepath.FromSlash(req.Key))
	if err != nil {
		return ReadResult{}, err
	}
	if !admitted.Mode().IsRegular() {
		return ReadResult{}, ErrInvalid
	}
	f, err := openRead(d.root, filepath.FromSlash(req.Key))
	if err != nil {
		return ReadResult{}, err
	}
	defer func() { _ = f.Close() }()
	before, err := f.Stat()
	if err != nil {
		return ReadResult{}, err
	}
	if !before.Mode().IsRegular() {
		return ReadResult{}, ErrInvalid
	}
	if !os.SameFile(admitted, before) {
		return ReadResult{}, ErrRevisionChanged
	}
	if req.Revision != "" && req.Revision != revision(before) {
		return ReadResult{}, ErrRevisionChanged
	}
	if req.PrefixBytes == 0 && before.Size() > cap {
		return ReadResult{}, ErrTooLarge
	}
	var reader io.Reader = f
	if req.PrefixBytes > 0 {
		reader = io.LimitReader(f, req.PrefixBytes)
	}
	data, err := readBody(ctx, reader, cap)
	if err != nil {
		return ReadResult{}, err
	}
	after, err := f.Stat()
	if err != nil {
		return ReadResult{}, err
	}
	current, err := d.root.Stat(filepath.FromSlash(req.Key))
	if err != nil {
		return ReadResult{}, err
	}
	if !os.SameFile(before, current) || revision(before) != revision(after) || revision(after) != revision(current) {
		return ReadResult{}, ErrRevisionChanged
	}
	return ReadResult{Data: data, Complete: int64(len(data)) == before.Size()}, nil
}

// ListPage uses an opaque positional cursor over directory traversal.
// Each call is bounded; concurrent mutations may change enumeration positions.
// Symlinks are skipped; all opens remain constrained by os.Root.
func (d *Directory) ListPage(ctx context.Context, req ListRequest) (Page, error) {
	if d.root == nil {
		opened, err := OpenDirectory(d.path, d.limits)
		if err != nil {
			return Page{}, err
		}
		defer func() { _ = opened.Close() }()
		return opened.ListPage(ctx, req)
	}
	if err := ctx.Err(); err != nil {
		return Page{}, err
	}
	if req.Limit <= 0 {
		return Page{}, ErrInvalid
	}
	req.Limit = min(req.Limit, d.limits.MaxObjects)
	offset := 0
	if req.Cursor != "" {
		n, err := strconv.Atoi(req.Cursor)
		if err != nil || n < 0 || n > d.limits.MaxObjects {
			return Page{}, ErrInvalid
		}
		offset = n
	}
	out := Page{}
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
			return ErrInvalid
		}
		for {
			entries, readErr := f.ReadDir(256)
			for _, entry := range entries {
				if err := ctx.Err(); err != nil {
					return err
				}
				visited++
				if visited > d.limits.MaxObjects {
					return ErrListingLimit
				}
				key := path.Join(dir, entry.Name())
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
		return Page{}, err
	}
	return out, nil
}
