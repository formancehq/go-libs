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
	"github.com/stretchr/testify/require"
)

func TestCollectValuesAndEffectiveFlags(t *testing.T) {
	parent := &cobra.Command{Use: "parent"}
	parent.PersistentFlags().String("inherited", "parent-value", "")
	parent.PersistentFlags().String("shadowed", "hidden", "")
	command := &cobra.Command{Use: "child"}
	parent.AddCommand(command)
	command.Flags().String("shadowed", "local-value", "")
	command.Flags().StringSlice("slice", []string{"one", "", "one", "two"}, "")
	command.Flags().StringArray("array", []string{"one,two", "three"}, "")
	command.Flags().String("empty", "", "")
	command.Flags().String("ignored", "public", "")
	command.PersistentFlags().String("persistent", "persistent-value", "")
	counts := map[string]int{}
	got, err := Collect(command, func(flag *pflag.Flag) (Source, error) {
		counts[flag.Name]++
		if flag.Name == "ignored" {
			return Source{}, nil
		}
		return Source{Kind: Value}, nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"one,two", "three", "parent-value", "persistent-value", "local-value", "one", "two"}, got)
	for _, count := range counts {
		require.Equal(t, 1, count)
	}
}

func TestCollectFiles(t *testing.T) {
	for _, tc := range []struct {
		name                                                   string
		explicit, optional, exists, dangling, directory, empty bool
		wantError, activated                                   bool
	}{
		{name: "explicit", explicit: true, exists: true},
		{name: "default", exists: true, activated: true},
		{name: "absent optional", optional: true},
		{name: "missing explicit", explicit: true, optional: true, wantError: true},
		{name: "missing required", wantError: true},
		{name: "dangling optional", optional: true, dangling: true, wantError: true},
		{name: "directory", directory: true, optional: true, wantError: true},
		{name: "empty file", exists: true, empty: true, activated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			command := &cobra.Command{Use: "test"}
			command.Flags().String("key-file", "", "")
			path := filepath.Join(t.TempDir(), "input")
			if tc.exists {
				contents := []byte(" synthetic\nfile \n")
				if tc.empty {
					contents = nil
				}
				require.NoError(t, os.WriteFile(path, contents, 0o600))
			}
			if tc.dangling {
				require.NoError(t, os.Symlink(path+"-absent", path))
			}
			if tc.directory {
				require.NoError(t, os.Mkdir(path, 0o700))
			}
			if tc.explicit {
				require.NoError(t, command.Flags().Set("key-file", path))
			}
			got, err := Collect(command, func(*pflag.Flag) (Source, error) {
				return Source{Kind: File, DefaultPath: path, OptionalDefault: tc.optional}, nil
			})
			if tc.wantError {
				require.Error(t, err)
				require.Nil(t, got)
				require.NotContains(t, fmt.Sprintf("%+v %#v", err, err), path)
				if !tc.directory {
					require.ErrorIs(t, err, os.ErrNotExist)
				}
			} else {
				require.NoError(t, err)
				if tc.exists && !tc.empty {
					require.Equal(t, []string{" synthetic\nfile \n"}, got)
				} else {
					require.Empty(t, got)
				}
			}
			require.Equal(t, tc.explicit || tc.activated, command.Flags().Lookup("key-file").Changed)
		})
	}
}

type rejectingValue struct{}

func (rejectingValue) String() string { return "" }
func (rejectingValue) Type() string   { return "string" }
func (rejectingValue) Set(value string) error {
	return fmt.Errorf("rejected synthetic-secret %s", value)
}

func TestCollectFailureReturnsNoPartialValues(t *testing.T) {
	command := &cobra.Command{Use: "test"}
	command.Flags().String("a-token", "synthetic-secret", "")
	command.Flags().Var(rejectingValue{}, "b-file", "")
	path := filepath.Join(t.TempDir(), "synthetic-path")
	require.NoError(t, os.WriteFile(path, []byte("synthetic-file"), 0o600))
	got, err := Collect(command, func(flag *pflag.Flag) (Source, error) {
		if flag.Name == "a-token" {
			return Source{Kind: Value}, nil
		}
		return Source{Kind: File, DefaultPath: path}, nil
	})
	require.Error(t, err)
	require.Nil(t, got)
	require.Nil(t, errors.Unwrap(err))
	require.False(t, command.Flags().Lookup("b-file").Changed)
	require.NotContains(t, fmt.Sprintf("%+v %#v", err, err), "synthetic")
}

func TestCollectInvalidInputsAndSelectorFailure(t *testing.T) {
	_, err := Collect(nil, nil)
	require.Error(t, err)
	_, err = Collect(&cobra.Command{}, nil)
	require.Error(t, err)
	for _, kind := range []Kind{Ignore, Kind(99)} {
		command := &cobra.Command{Use: "test"}
		command.Flags().String("a", "synthetic", "")
		command.Flags().String("b", "synthetic", "")
		calls := 0
		expected := errors.New("classification failed")
		values, err := Collect(command, func(*pflag.Flag) (Source, error) {
			calls++
			if kind == Ignore {
				return Source{}, expected
			}
			return Source{Kind: kind}, nil
		})
		require.Error(t, err)
		require.Nil(t, values)
		require.Equal(t, 1, calls)
		if kind == Ignore {
			require.ErrorIs(t, err, expected)
		} else {
			require.True(t, strings.Contains(err.Error(), "invalid secret source kind"))
		}
	}
}
