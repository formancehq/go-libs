package secretflags

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestOptionalDefaultDistinguishesDanglingAncestorsFromAbsence(t *testing.T) {
	for _, kind := range []string{"dangling ancestor", "dangling before parent traversal", "missing descendants", "healthy symlink missing child", "healthy symlink readable child"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "keys", "token")
			wantError, wantValue := false, false
			switch kind {
			case "dangling ancestor", "dangling before parent traversal":
				if err := os.Symlink(filepath.Join(root, "absent"), filepath.Join(root, "keys")); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				wantError = true
				if kind == "dangling before parent traversal" {
					path = filepath.Join(root, "keys") + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "token"
				}
			case "missing descendants":
				path = filepath.Join(root, "missing", "keys", "token")
			case "healthy symlink missing child", "healthy symlink readable child":
				target := filepath.Join(root, "actual")
				if err := os.Mkdir(target, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(root, "keys")); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				if kind == "healthy symlink readable child" {
					if err := os.WriteFile(filepath.Join(target, "token"), []byte(" synthetic-value\n"), 0o600); err != nil {
						t.Fatal(err)
					}
					wantValue = true
				}
			}
			command := &cobra.Command{Use: "test"}
			command.Flags().String("a-value", "earlier-synthetic-value", "")
			command.Flags().String("key-file", "", "")
			got, err := Collect(command, func(flag *pflag.Flag) (Source, error) {
				if flag.Name == "a-value" {
					return Source{Kind: Value}, nil
				}
				return Source{Kind: File, DefaultPath: path, OptionalDefault: true}, nil
			})
			flag := command.Flags().Lookup("key-file")
			if wantError {
				if !errors.Is(err, os.ErrNotExist) || got != nil || flag.Changed || flag.Value.String() != "" {
					t.Fatal("dangling ancestor was not refused without values or activation")
				}
				diagnostic := fmt.Sprintf("%+v %#v", err, err)
				if strings.Contains(diagnostic, root) || strings.Contains(diagnostic, "earlier-synthetic-value") {
					t.Fatal("file diagnostic exposed a path or collected value")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if wantValue {
				if len(got) != 2 || got[1] != " synthetic-value\n" || !flag.Changed || flag.Value.String() != path {
					t.Fatal("healthy symlink did not preserve exact contents and activation")
				}
			} else if len(got) != 1 || flag.Changed || flag.Value.String() != "" {
				t.Fatal("genuinely absent optional default changed collection or activation")
			}
		})
	}
}
