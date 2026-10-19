// Package secretflags collects selected command flag values for secret redaction.
// Applications own selection, annotations, naming and redaction registration.
package secretflags

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Kind identifies how a selected flag contributes secret values.
type Kind uint8

const (
	// Ignore leaves the flag untouched.
	Ignore Kind = iota
	// Value collects the flag's scalar value or individual slice members.
	Value
	// File collects file contents, never the path as a secret value.
	File
)

// Source describes the application's interpretation of one flag.
type Source struct {
	Kind Kind
	// DefaultPath is used only for a File with an empty flag value. A readable
	// default is activated on the flag, including Changed, before collection returns.
	DefaultPath string
	// OptionalDefault permits a genuinely absent default, but never a dangling
	// symlink, unreadable file or missing explicitly configured file.
	OptionalDefault bool
}

// Collect visits local, persistent and inherited flags, once per effective name,
// in that order (Cobra may already have merged inherited flags into local flags).
// selectSource owns all application annotations and policy. Call
// after parsing and environment binding, before starting work or configuring logs.
// Values retain first occurrence order; empty values and duplicates are omitted.
// No trimming or interpretation of file contents takes place.
//
// On failure no partial values are returned. Defaults activated before a later
// failure are not rolled back; the caller must stop startup. Commands and their
// flag sets must not be mutated concurrently. No secrets are logged or registered
// globally. A flag setter's error is deliberately discarded: it can echo input.
func Collect(command *cobra.Command, selectSource func(*pflag.Flag) (Source, error)) ([]string, error) {
	if command == nil {
		return nil, errors.New("command is required")
	}
	if selectSource == nil {
		return nil, errors.New("secret source selector is required")
	}
	var values []string
	seenFlags := map[string]struct{}{}
	seenValues := map[string]struct{}{}
	for _, flags := range []*pflag.FlagSet{command.Flags(), command.PersistentFlags(), command.InheritedFlags()} {
		var collectErr error
		flags.VisitAll(func(flag *pflag.Flag) {
			if collectErr != nil {
				return
			}
			if _, seen := seenFlags[flag.Name]; seen {
				return
			}
			seenFlags[flag.Name] = struct{}{}
			source, err := selectSource(flag)
			if err != nil {
				collectErr = err
				return
			}
			var selected []string
			switch source.Kind {
			case Ignore:
				return
			case Value:
				if slice, ok := flag.Value.(pflag.SliceValue); ok {
					selected = slice.GetSlice()
				} else {
					selected = []string{flag.Value.String()}
				}
			case File:
				selected, collectErr = readFile(flag, source)
				if collectErr != nil {
					return
				}
			default:
				collectErr = fmt.Errorf("invalid secret source kind for --%s", flag.Name)
				return
			}
			for _, value := range selected {
				if value == "" {
					continue
				}
				if _, seen := seenValues[value]; seen {
					continue
				}
				seenValues[value] = struct{}{}
				values = append(values, value)
			}
		})
		if collectErr != nil {
			return nil, collectErr
		}
	}
	return values, nil
}

func readFile(flag *pflag.Flag, source Source) ([]string, error) {
	path := flag.Value.String()
	implicit := path == ""
	if implicit {
		path = source.DefaultPath
	}
	contents, err := os.ReadFile(path) //nolint:gosec // application-selected secret file
	if err != nil {
		if implicit && source.OptionalDefault && os.IsNotExist(err) && missingOptionalDefault(path) {
			return nil, nil
		}
		// os.PathError embeds the input path; retain the filesystem classification
		// without echoing operator input into startup diagnostics.
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			err = pathErr.Err
		}
		return nil, fmt.Errorf("read secret file for --%s: %w", flag.Name, err)
	}
	if implicit {
		if err := flag.Value.Set(path); err != nil {
			return nil, fmt.Errorf("set secret file flag --%s: value rejected", flag.Name)
		}
		flag.Changed = true
	}
	return []string{string(contents)}, nil
}

// missingOptionalDefault excludes dangling symlinks at any path component.
// Inspect prefixes without cleaning the path: link/../file must still inspect
// link before treating the read failure as optional absence.
func missingOptionalDefault(path string) bool {
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return false
	}
	volume := len(filepath.VolumeName(path))
	for i := volume; i < len(path); i++ {
		if i == volume || !os.IsPathSeparator(path[i]) {
			continue
		}
		prefix := path[:i]
		info, err := os.Lstat(prefix)
		if err != nil {
			if !os.IsNotExist(err) {
				return false
			}
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if _, err := os.Stat(prefix); err != nil {
				return false
			}
		}
	}
	return true
}
