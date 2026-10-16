package service

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestExactEnvironmentStrings(t *testing.T) {
	for _, value := range []string{"", "  synthetic-token  ", " \t\n", "a,b c", "[literal]"} {
		t.Run(fmt.Sprintf("%q", value), func(t *testing.T) {
			t.Setenv("QUALIFICATION_TOKEN", value)
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			token := flags.String("qualification-token", "default", "")
			require.NoError(t, BindEnvToFlagSetExact(flags))
			require.Equal(t, value, *token)
			require.True(t, flags.Changed("qualification-token"))
			require.Equal(t, 1, flags.NFlag())
			var visited []string
			flags.Visit(func(f *pflag.Flag) { visited = append(visited, f.Name) })
			require.Equal(t, []string{"qualification-token"}, visited)
			require.NoError(t, BindEnvToFlagSetExact(flags))
			require.Equal(t, value, *token)
		})
	}
}

func TestExactEnvironmentCollections(t *testing.T) {
	for _, kind := range []string{"slice", "array"} {
		for _, tc := range []struct {
			input string
			want  []string
		}{
			{`[]`, []string{}}, {`[""]`, []string{""}},
			{`["one,two","three four",""]`, []string{"one,two", "three four", ""}},
			{" \n[\" a \",\"line\\nnext\",\"quoted\\\"\"]\t", []string{" a ", "line\nnext", `quoted"`}},
		} {
			t.Run(kind+tc.input, func(t *testing.T) {
				t.Setenv("QUALIFICATION_ITEMS", tc.input)
				flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
				var value *[]string
				if kind == "slice" {
					value = flags.StringSlice("qualification-items", []string{"default"}, "")
				} else {
					value = flags.StringArray("qualification-items", []string{"default"}, "")
				}
				original := flags.Lookup("qualification-items").Value
				require.NoError(t, BindEnvToFlagSetExact(flags))
				require.Equal(t, tc.want, *value)
				require.Same(t, original, flags.Lookup("qualification-items").Value)
				require.True(t, flags.Changed("qualification-items"))
				require.Equal(t, 1, flags.NFlag())
				var visited int
				flags.Visit(func(*pflag.Flag) { visited++ })
				require.Equal(t, 1, visited)
				require.NoError(t, BindEnvToFlagSetExact(flags))
				require.Equal(t, tc.want, *value)
			})
		}
	}
}

func TestExactEnvironmentFailuresAreSecretSafe(t *testing.T) {
	for _, kind := range []string{"int", "bool", "slice", "array", "custom"} {
		for _, input := range []string{"", "synthetic-secret", "null", `[null]`, `[1]`, `[true]`, `[{}]`, `[[]]`, `["secret",null]`, `["secret"] trailing`} {
			t.Run(kind+input, func(t *testing.T) {
				t.Setenv("QUALIFICATION_VALUE", input)
				flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
				flags.Bool(DebugFlag, true, "")
				switch kind {
				case "int":
					flags.Int("qualification-value", 9, "")
				case "bool":
					flags.Bool("qualification-value", false, "")
				case "slice":
					flags.StringSlice("qualification-value", []string{"default"}, "")
				case "array":
					flags.StringArray("qualification-value", []string{"default"}, "")
				case "custom":
					flags.Var(new(disclosingValue), "qualification-value", "")
				}
				err := BindEnvToFlagSetExact(flags)
				require.Error(t, err)
				binding, ok := errors.AsType[*EnvironmentBindingError](err)
				require.True(t, ok)
				require.Equal(t, "QUALIFICATION_VALUE", binding.EnvironmentVariable)
				require.Equal(t, "qualification-value", binding.FlagName)
				require.ErrorIs(t, err, ErrInvalidEnvironmentValue)
				require.False(t, flags.Changed("qualification-value"))
				require.Zero(t, flags.NFlag())
				for e := err; e != nil; e = errors.Unwrap(e) {
					require.NotContains(t, fmt.Sprintf("%+v %#v", e, e), "synthetic-secret")
					require.NotContains(t, fmt.Sprintf("%+v %#v", e, e), "parser payload")
				}
			})
		}
	}
}

type disclosingValue struct{}

func (*disclosingValue) String() string         { return "default" }
func (*disclosingValue) Type() string           { return "custom" }
func (*disclosingValue) Set(value string) error { return fmt.Errorf("parser payload %s", value) }

func TestExactEnvironmentCLIAndUnset(t *testing.T) {
	t.Setenv("QUALIFICATION_ITEMS", "malformed-secret")
	t.Setenv("QUALIFICATION_LIMIT", "malformed-secret")
	t.Setenv("DEBUG", "true")
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	values := flags.StringSlice("qualification-items", []string{"default"}, "")
	limit := flags.Int("qualification-limit", 1, "")
	debug := flags.Bool(DebugFlag, false, "")
	untouched := flags.String("qualification-unset-unique", "default", "")
	t.Setenv("QUALIFICATION_UNSET_UNIQUE", "ignored")
	require.NoError(t, flags.Parse([]string{"--qualification-items=a,b", "--qualification-limit=4"}))
	// Exercise absence explicitly instead of relying on the caller's environment.
	require.NoError(t, os.Unsetenv("QUALIFICATION_UNSET_UNIQUE"))
	require.NoError(t, BindEnvToFlagSetExact(flags))
	require.Equal(t, []string{"a", "b"}, *values)
	require.Equal(t, 4, *limit)
	require.False(t, *debug)
	require.Equal(t, "default", *untouched)
	require.False(t, flags.Changed("qualification-unset-unique"))
}

func TestExactSelectedCommand(t *testing.T) {
	for _, selected := range []string{"run", "version"} {
		t.Run(selected, func(t *testing.T) {
			t.Setenv("QUALIFICATION_TOKEN", "  exact  ")
			t.Setenv("QUALIFICATION_SHARED", "shared")
			t.Setenv("QUALIFICATION_SHADOW", "selected")
			t.Setenv("QUALIFICATION_ROOT_LOCAL", "malformed")
			t.Setenv("QUALIFICATION_BAD", "malformed")
			root := &cobra.Command{Use: "root", SilenceErrors: true, SilenceUsage: true}
			root.Flags().Int("qualification-root-local", 1, "")
			shared := root.PersistentFlags().String("qualification-shared", "default", "")
			shadow := root.PersistentFlags().Int("qualification-shadow", 1, "")
			called := false
			run := &cobra.Command{Use: "run", Run: func(*cobra.Command, []string) { called = true }}
			run.Flags().String("qualification-token", "default", "")
			run.Flags().String("qualification-shadow", "default", "")
			run.Args = func(cmd *cobra.Command, args []string) error {
				require.NoError(t, BindEnvToSelectedCommandExact(cmd))
				require.Equal(t, "  exact  ", cmd.Flag("qualification-token").Value.String())
				require.Equal(t, "selected", cmd.Flag("qualification-shadow").Value.String())
				require.True(t, cmd.Flags().Changed("qualification-shared"))
				return cobra.NoArgs(cmd, args)
			}
			require.NoError(t, run.MarkFlagRequired("qualification-token"))
			version := &cobra.Command{Use: "version", Run: func(*cobra.Command, []string) { called = true }}
			// CLI wins for inherited malformed integer on the version command.
			version.Args = func(cmd *cobra.Command, _ []string) error { return BindEnvToSelectedCommandExact(cmd) }
			bad := &cobra.Command{Use: "unused"}
			bad.Flags().Int("qualification-bad", 1, "")
			root.AddCommand(run, version, bad)
			args := []string{selected}
			if selected == "version" {
				args = append(args, "--qualification-shadow=4")
			}
			root.SetArgs(args)
			require.NoError(t, root.Execute())
			require.True(t, called)
			require.Equal(t, "shared", *shared)
			require.Equal(t, selected == "version", root.PersistentFlags().Changed("qualification-shadow"))
			if selected == "run" {
				require.Equal(t, 1, *shadow)
			}
			require.False(t, root.Flags().Changed("qualification-root-local"))
			require.False(t, bad.Flags().Changed("qualification-bad"))
		})
	}
}

func TestExactEnvironmentScalarParsing(t *testing.T) {
	t.Setenv("QUALIFICATION_ENABLED", "true")
	t.Setenv("QUALIFICATION_LIMIT", "42")
	t.Setenv("QUALIFICATION_TIMEOUT", "2s")
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	enabled := flags.Bool("qualification-enabled", false, "")
	limit := flags.Int("qualification-limit", 1, "")
	timeout := flags.Duration("qualification-timeout", 0, "")
	require.NoError(t, BindEnvToFlagSetExact(flags))
	require.True(t, *enabled)
	require.Equal(t, 42, *limit)
	require.Equal(t, "2s", timeout.String())
	require.Equal(t, 3, flags.NFlag())
}

func TestLegacyEnvironmentParsingRemainsCompatible(t *testing.T) {
	t.Setenv("QUALIFICATION_TOKEN", "  trimmed  ")
	t.Setenv("QUALIFICATION_ITEMS", "one two")
	t.Setenv("QUALIFICATION_EMPTY", "")
	flags := pflag.NewFlagSet("legacy", pflag.ContinueOnError)
	token := flags.String("qualification-token", "default", "")
	items := flags.StringSlice("qualification-items", nil, "")
	empty := flags.String("qualification-empty", "default", "")
	require.NoError(t, BindEnvToFlagSetWithError(flags))
	require.Equal(t, "trimmed", *token)
	require.Equal(t, []string{"one", "two"}, *items)
	require.Equal(t, "default", *empty)
	require.False(t, flags.Changed("qualification-empty"))
}

func TestExactEnvironmentCLIPrecedenceForCollections(t *testing.T) {
	for _, kind := range []string{"slice", "array"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("QUALIFICATION_ITEMS", `["environment"]`)
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			var items *[]string
			if kind == "slice" {
				items = flags.StringSlice("qualification-items", nil, "")
			} else {
				items = flags.StringArray("qualification-items", nil, "")
			}
			require.NoError(t, flags.Parse([]string{"--qualification-items=cli", "--qualification-items=second"}))
			require.NoError(t, BindEnvToFlagSetExact(flags))
			require.Equal(t, []string{"cli", "second"}, *items)
		})
	}
}

func TestExactSelectedCommandRefusesBeforeHooks(t *testing.T) {
	t.Setenv("QUALIFICATION_LIMIT", "synthetic-secret")
	root := &cobra.Command{Use: "root", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().Bool(DebugFlag, true, "")
	root.PersistentPreRun = func(*cobra.Command, []string) { t.Fatal("invalid config reached startup") }
	run := &cobra.Command{Use: "run", Args: func(cmd *cobra.Command, _ []string) error { return BindEnvToSelectedCommandExact(cmd) }, Run: func(*cobra.Command, []string) { t.Fatal("invalid config reached run") }}
	run.Flags().Int("qualification-limit", 1, "")
	root.AddCommand(run)
	root.SetArgs([]string{"run"})
	err := root.Execute()
	require.ErrorIs(t, err, ErrInvalidEnvironmentValue)
	require.NotContains(t, err.Error(), "synthetic-secret")
}

func TestExactEnvironmentRestoresRejectedSliceValue(t *testing.T) {
	t.Setenv("QUALIFICATION_ITEMS", `["synthetic-secret"]`)
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	original := new(rejectedSliceValue)
	flags.Var(original, "qualification-items", "")
	err := BindEnvToFlagSetExact(flags)
	require.ErrorIs(t, err, ErrInvalidEnvironmentValue)
	require.Same(t, original, flags.Lookup("qualification-items").Value)
	require.False(t, flags.Changed("qualification-items"))
	require.Zero(t, flags.NFlag())
	require.NotContains(t, err.Error(), "synthetic-secret")
	require.NotContains(t, errors.Unwrap(err).Error(), "synthetic-secret")
}

type rejectedSliceValue struct{}

func (*rejectedSliceValue) String() string         { return "[]" }
func (*rejectedSliceValue) Type() string           { return "stringSlice" }
func (*rejectedSliceValue) Set(value string) error { return fmt.Errorf("unexpected Set: %s", value) }
func (*rejectedSliceValue) Append(value string) error {
	return fmt.Errorf("unexpected Append: %s", value)
}
func (*rejectedSliceValue) Replace(values []string) error {
	return fmt.Errorf("parser payload: %v", values)
}
func (*rejectedSliceValue) GetSlice() []string { return nil }
