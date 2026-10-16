//go:build !unix

package object

import "os"

func openRead(root *os.Root, key string) (*os.File, error) { return root.Open(key) }
