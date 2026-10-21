//go:build unix

package directory

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/formancehq/go-libs/v5/pkg/storage/object"
)

func TestDirectoryRefusesFIFOWithoutWriter(t *testing.T) {
	root := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(root, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	directory, err := Open(root, object.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = directory.Close() }()
	done := make(chan error, 1)
	go func() { _, err := directory.Read(t.Context(), object.ReadRequest{Key: "fifo"}); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, object.ErrInvalid) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO blocked before type admission")
	}
	file, err := openRead(directory.root, "fifo")
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
}

func TestDirectoryListingSkipsUnreadableKeysBeforeRecursion(t *testing.T) {
	root := t.TempDir()
	for _, key := range []string{"normal.txt", "ok/normal.txt", `bad\file.txt`, `ok/bad\file.txt`} {
		file := filepath.Join(root, key)
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("abc"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	unsupported := filepath.Join(root, `bad\directory`)
	if err := os.Mkdir(unsupported, 0o700); err != nil {
		t.Fatal(err)
	}
	// Recursing into an unsupported directory would exhaust the entry budget.
	for i := range 12 {
		if err := os.WriteFile(filepath.Join(unsupported, string(rune('a'+i))), []byte("abc"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, mode := range []string{"path", "pinned"} {
		t.Run(mode, func(t *testing.T) {
			var directory *Bucket
			var err error
			if mode == "path" {
				directory, err = NewBucket(root, object.Limits{MaxObjects: 8})
			} else {
				directory, err = Open(root, object.Limits{MaxObjects: 8})
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = directory.Close() }()
			objects, err := object.ListAll(t.Context(), directory, "", object.Limits{})
			if err != nil {
				t.Fatal(err)
			}
			var keys []string
			for _, meta := range objects {
				keys = append(keys, meta.Key)
				if _, err := directory.Stat(t.Context(), meta.Key); err != nil {
					t.Fatal(err)
				}
				result, err := directory.Read(t.Context(), object.ReadRequest{Key: meta.Key, Revision: meta.Revision})
				if err != nil || string(result.Data) != "abc" {
					t.Fatalf("listed key cannot be read: %q, %v", meta.Key, err)
				}
				_, data, err := object.ReadCurrent(t.Context(), directory, meta.Key, 16)
				if err != nil || string(data) != "abc" {
					t.Fatalf("listed key cannot be read currently: %q, %v", meta.Key, err)
				}
			}
			slices.Sort(keys)
			if !slices.Equal(keys, []string{"normal.txt", "ok/normal.txt"}) {
				t.Fatalf("listed unsupported keys: %q", keys)
			}
			// Unsupported entries still consume inventory budget; skipping them must
			// not make enumeration of an arbitrarily large directory unbounded.
			skippedRoot := t.TempDir()
			for _, name := range []string{`bad\one`, `bad\two`} {
				if err := os.WriteFile(filepath.Join(skippedRoot, name), []byte("abc"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			limited, err := NewBucket(skippedRoot, object.Limits{MaxObjects: 1})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := limited.ListPage(t.Context(), object.ListRequest{Limit: 1}); !errors.Is(err, object.ErrListingLimit) {
				t.Fatalf("entry budget not enforced: %v", err)
			}
		})
	}
}
