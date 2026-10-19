package directory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/formancehq/go-libs/v5/pkg/storage/object"
)

func TestDirectorySafety(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "item"), []byte("abcdef"), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := Open(root, object.Limits{MaxBytes: 6})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	m, err := d.Stat(t.Context(), "item")
	if err != nil {
		t.Fatal(err)
	}
	full, err := d.Read(t.Context(), object.ReadRequest{Key: "item", Revision: m.Revision})
	if err != nil || !full.Complete || string(full.Data) != "abcdef" {
		t.Fatalf("full read: %+v %v", full, err)
	}
	prefix, err := d.Read(t.Context(), object.ReadRequest{Key: "item", PrefixBytes: 3})
	if err != nil || prefix.Complete || string(prefix.Data) != "abc" {
		t.Fatalf("prefix: %+v %v", prefix, err)
	}
	if _, err := d.Read(t.Context(), object.ReadRequest{Key: "item", MaxBytes: 2}); !errors.Is(err, object.ErrTooLarge) {
		t.Fatal(err)
	}
	if _, err := d.Read(t.Context(), object.ReadRequest{Key: "item", Revision: "changed"}); !errors.Is(err, object.ErrRevisionChanged) {
		t.Fatal(err)
	}
	for _, key := range []string{"../outside", "/absolute", `..\outside`, "."} {
		if _, err := d.Read(t.Context(), object.ReadRequest{Key: key}); err == nil {
			t.Fatalf("accepted %s", key)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := d.Read(ctx, object.ReadRequest{Key: "item"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, _, err := object.ReadCurrent(t.Context(), d, "item", 6); err != nil {
		t.Fatal(err)
	}
}
func TestDirectoryContainmentAndPaging(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skip("symlinks unavailable")
	}
	for _, key := range []string{"one", "two", "three"} {
		if err := os.WriteFile(filepath.Join(root, key), []byte(key), 0600); err != nil {
			t.Fatal(err)
		}
	}
	d, err := Open(root, object.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if _, err := d.Read(t.Context(), object.ReadRequest{Key: "escape"}); err == nil {
		t.Fatal("read outside root")
	}
	got, err := object.ListAll(t.Context(), d, "", object.Limits{})
	if err != nil || len(got) != 3 {
		t.Fatalf("%v %v", got, err)
	}
	first, err := d.ListPage(t.Context(), object.ListRequest{Limit: 1})
	if err != nil || len(first.Objects) != 1 || first.NextCursor == "" {
		t.Fatalf("%+v %v", first, err)
	}
	second, err := d.ListPage(t.Context(), object.ListRequest{Limit: 1, Cursor: first.NextCursor})
	if err != nil || len(second.Objects) != 1 || second.Objects[0].Key == first.Objects[0].Key {
		t.Fatalf("%+v %v", second, err)
	}
	if _, err := object.ListAll(t.Context(), d, "", object.Limits{MaxObjects: 2}); !errors.Is(err, object.ErrListingLimit) {
		t.Fatal(err)
	}
}

func TestDirectoryRejectsChangedGenerationAndSpecialFiles(t *testing.T) {
	root := t.TempDir()
	filename := filepath.Join(root, "item")
	if err := os.WriteFile(filename, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := Open(root, object.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	meta, err := d.Stat(t.Context(), "item")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte("longer"), 0600); err != nil {
		t.Fatal(err)
	}
	if result, err := d.Read(t.Context(), object.ReadRequest{Key: "item", Revision: meta.Revision}); !errors.Is(err, object.ErrRevisionChanged) || result.Data != nil {
		t.Fatalf("changed content escaped: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "directory"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Stat(t.Context(), "directory"); !errors.Is(err, object.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := d.Read(t.Context(), object.ReadRequest{Key: "directory"}); !errors.Is(err, object.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := d.Stat(t.Context(), "../item"); !errors.Is(err, object.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := d.ListPage(t.Context(), object.ListRequest{Limit: 1, Cursor: "-1"}); !errors.Is(err, object.ErrInvalid) {
		t.Fatal(err)
	}
}
func TestDirectoryRetainsRootAcrossRenameAndFiltersNestedKeys(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "item"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	directory, err := Open(root, object.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = directory.Close() }()
	page, err := directory.ListPage(t.Context(), object.ListRequest{Prefix: "nested/", Limit: 2})
	if err != nil || len(page.Objects) != 1 || page.Objects[0].Key != "nested/item" {
		t.Fatalf("%+v %v", page, err)
	}
	if err := os.Rename(root, filepath.Join(base, "retained")); err != nil {
		t.Skip("platform cannot rename an open root")
	}
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "item"), []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := directory.Read(t.Context(), object.ReadRequest{Key: "nested/item"})
	if err != nil || string(result.Data) != "original" {
		t.Fatalf("configured root switched: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := directory.Stat(ctx, "nested/item"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := directory.ListPage(ctx, object.ListRequest{Limit: 1}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestLazyDirectoryFollowsConfiguredPathAndClosesOperations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "later")
	bucket, err := NewBucket(path, object.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bucket.Stat(t.Context(), "item"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "item"), []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := bucket.Read(t.Context(), object.ReadRequest{Key: "item"})
	if err != nil || string(first.Data) != "one" {
		t.Fatalf("%+v %v", first, err)
	}
	if err := os.Rename(path, path+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "item"), []byte("two"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := bucket.Read(t.Context(), object.ReadRequest{Key: "item"})
	if err != nil || string(second.Data) != "two" {
		t.Fatalf("%+v %v", second, err)
	}
	if err := bucket.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDirectoryChunkedListingPreservesWireMetadataAndUTC(t *testing.T) {
	root := t.TempDir()
	modified := time.Date(2001, 2, 3, 4, 5, 6, 0, time.FixedZone("offset", 3600))
	for i := range 300 {
		file := filepath.Join(root, fmt.Sprintf("item-%03d", i))
		if err := os.WriteFile(file, []byte("abc"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(file, modified, modified); err != nil {
			t.Fatal(err)
		}
	}
	bucket, err := NewBucket(root, object.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := object.ListAll(t.Context(), bucket, "item-", object.Limits{})
	if err != nil || len(got) != 300 {
		t.Fatalf("%d %v", len(got), err)
	}
	for _, meta := range got {
		if !meta.LastModified.Equal(modified) || meta.LastModified.Location() != time.UTC || meta.Size != 3 || meta.Revision != fmt.Sprintf("%d:3", modified.UnixNano()) {
			t.Fatalf("metadata changed: %+v", meta)
		}
	}
	raw, err := json.Marshal(got[0])
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf(`{"key":%q,"lastModified":%q,"size":3,"revision":%q}`, got[0].Key, modified.UTC().Format(time.RFC3339Nano), got[0].Revision)
	if string(raw) != want {
		t.Fatalf("wire metadata changed: %s, want %s", raw, want)
	}
	if _, err := object.ListAll(t.Context(), bucket, "item-", object.Limits{MaxObjects: 299}); !errors.Is(err, object.ErrListingLimit) {
		t.Fatal(err)
	}
}

func TestLazyDirectoryCancellationPrecedesRootAccess(t *testing.T) {
	bucket, err := NewBucket(filepath.Join(t.TempDir(), "missing"), object.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := bucket.Stat(ctx, "item"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := bucket.Read(ctx, object.ReadRequest{Key: "item"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := bucket.ListPage(ctx, object.ListRequest{Limit: 1}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
