//go:build unix

package object

import (
	"os"

	"golang.org/x/sys/unix"
)

// Nonblocking open prevents a racing replacement with a FIFO from blocking
// before the caller can inspect the opened descriptor. Regular-file I/O is
// unchanged; all traversal still uses the rooted OS API.
func openRead(root *os.Root, key string) (*os.File, error) {
	return root.OpenFile(key, os.O_RDONLY|unix.O_NONBLOCK, 0)
}
