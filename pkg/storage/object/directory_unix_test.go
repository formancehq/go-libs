//go:build unix

package object

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestDirectoryRefusesFIFOWithoutWriter(t *testing.T) {
	root := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(root, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	directory, err := OpenDirectory(root, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = directory.Close() }()
	done := make(chan error, 1)
	go func() { _, err := directory.Read(t.Context(), ReadRequest{Key: "fifo"}); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrInvalid) {
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
