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
		for _, input := range []string{"", "[", `  ["synthetic-secret"`, "synthetic-secret", "null", `[null]`, `[1]`, `[true]`, `[{}]`, `[[]]`, `["secret",null]`, `["secret"] trailing`} {
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

// These cases distinguish stored-value correctness from pflag's CSV-based
// convenience getters. Consumers requiring exact values must use GetSlice or
// the typed pointer, not a String representation followed by parsing.
func TestExactEnvironmentLosslessCollectionRetrieval(t *testing.T) {
	for _, kind := range []string{"slice", "array"} {
		for _, tc := range []struct {
			name        string
			input       string
			exact       []string
			convenience []string
		}{
			{"sole-empty", `[""]`, []string{""}, []string{}},
			{"CRLF", `["line\r\nnext"]`, []string{"line\r\nnext"}, []string{"line\nnext"}},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				t.Setenv("QUALIFICATION_ITEMS", tc.input)
				flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
				var pointer *[]string
				if kind == "slice" {
					pointer = flags.StringSlice("qualification-items", nil, "")
				} else {
					pointer = flags.StringArray("qualification-items", nil, "")
				}
				original := flags.Lookup("qualification-items").Value
				require.NoError(t, BindEnvToFlagSetExact(flags))
				require.Same(t, original, flags.Lookup("qualification-items").Value)
				require.Equal(t, tc.exact, *pointer)
				slice, ok := flags.Lookup("qualification-items").Value.(pflag.SliceValue)
				require.True(t, ok)
				require.Equal(t, tc.exact, slice.GetSlice())
				var convenience []string
				var err error
				if kind == "slice" {
					convenience, err = flags.GetStringSlice("qualification-items")
				} else {
					convenience, err = flags.GetStringArray("qualification-items")
				}
				require.NoError(t, err)
				require.Equal(t, tc.convenience, convenience)
				require.NotEqual(t, tc.exact, convenience)
				// Reading through convenience getters does not mutate the exact storage.
				require.Equal(t, tc.exact, slice.GetSlice())
				require.Equal(t, tc.exact, *pointer)
			})
		}
	}
}

func TestExactEnvironmentRequiredCollection(t *testing.T) {
	for _, compatible := range []bool{false, true} {
		options := []EnvironmentBindingOption{}
		if compatible {
			options = append(options, WithIgnoredEmptyEnvironmentValues(), WithJSONOrDelimitedEnvironmentCollections(), WithContinuedEnvironmentBinding())
		}
		t.Run(fmt.Sprintf("compatible=%t", compatible), func(t *testing.T) {
			for _, kind := range []string{"slice", "array"} {
				for _, inherited := range []bool{false, true} {
					for _, tc := range []struct {
						name    string
						input   string
						present bool
						want    []string
						invalid bool
					}{
						{name: "empty-array", input: `[]`, present: true, want: []string{}},
						{name: "sole-empty", input: `[""]`, present: true, want: []string{""}},
						{name: "items", input: `["one,two","line\r\nnext"]`, present: true, want: []string{"one,two", "line\r\nnext"}},
						{name: "malformed-prefix", input: `[`, present: true, invalid: true},
						{name: "absent"},
					} {
						t.Run(fmt.Sprintf("%s/inherited=%t/%s", kind, inherited, tc.name), func(t *testing.T) {
							t.Setenv("QUALIFICATION_ITEMS", tc.input)
							if !tc.present {
								require.NoError(t, os.Unsetenv("QUALIFICATION_ITEMS"))
							}
							root := &cobra.Command{Use: "root", SilenceUsage: true, SilenceErrors: true}
							run := &cobra.Command{Use: "run"}
							flags := run.Flags()
							if inherited {
								flags = root.PersistentFlags()
							}
							var pointer *[]string
							if kind == "slice" {
								pointer = flags.StringSlice("qualification-items", nil, "")
							} else {
								pointer = flags.StringArray("qualification-items", nil, "")
							}
							if inherited {
								require.NoError(t, root.MarkPersistentFlagRequired("qualification-items"))
							} else {
								require.NoError(t, run.MarkFlagRequired("qualification-items"))
							}
							validated := false
							ran := false
							originalArgs := func(cmd *cobra.Command, args []string) error {
								validated = true
								require.Equal(t, tc.present, cmd.Flags().Changed("qualification-items"))
								return cobra.NoArgs(cmd, args)
							}
							run.Args = func(cmd *cobra.Command, args []string) error {
								if err := BindEnvToSelectedCommandExact(cmd, options...); err != nil {
									return err
								}
								return originalArgs(cmd, args)
							}
							run.RunE = func(cmd *cobra.Command, _ []string) error {
								ran = true
								require.True(t, validated)
								require.NoError(t, cmd.ValidateRequiredFlags())
								require.Equal(t, tc.want, *pointer)
								require.Equal(t, tc.want, cmd.Flag("qualification-items").Value.(pflag.SliceValue).GetSlice())
								require.True(t, cmd.Flags().Changed("qualification-items"))
								require.Equal(t, 1, cmd.Flags().NFlag())
								var visited []string
								cmd.Flags().Visit(func(f *pflag.Flag) { visited = append(visited, f.Name) })
								require.Equal(t, []string{"qualification-items"}, visited)
								return nil
							}
							root.AddCommand(run)
							root.SetArgs([]string{"run"})
							err := root.Execute()
							switch {
							case tc.invalid:
								require.ErrorIs(t, err, ErrInvalidEnvironmentValue)
								require.False(t, validated)
								require.False(t, ran)
								require.False(t, run.Flags().Changed("qualification-items"))
								require.Zero(t, run.Flags().NFlag())
							case !tc.present:
								require.ErrorContains(t, err, `required flag(s) "qualification-items" not set`)
								require.True(t, validated)
								require.False(t, ran)
							default:
								require.NoError(t, err)
								require.True(t, ran)
							}
						})
					}
				}
			}
		})
	}
}

func TestEnvironmentCompatibilityInputMatrix(t *testing.T) {
	// Frozen input/output cases: expectations are literal, not calculated with a
	// second implementation of the binder. Both options are needed for this policy.
	for _, binder := range []string{"flag-set", "selected-command"} {
		for _, kind := range []string{"slice", "array"} {
			for _, tc := range []struct {
				name, input  string
				slice, array []string
				invalid      bool
			}{
				{name: "empty-ignored", input: "", slice: []string{"default"}, array: []string{"default"}},
				{name: "csv", input: "first,second", slice: []string{"first", "second"}, array: []string{"first,second"}},
				{name: "space", input: "first second", slice: []string{"first", "second"}, array: []string{"first second"}},
				{name: "surrounding-spaces", input: " first ", slice: []string{"", "first", ""}, array: []string{" first "}},
				{name: "comma-space", input: "first, second", slice: []string{"first", "", "second"}, array: []string{"first, second"}},
				{name: "tab", input: "first\tsecond", slice: []string{"first\tsecond"}, array: []string{"first\tsecond"}},
				{name: "json-empty", input: `[]`, slice: []string{}, array: []string{}},
				{name: "json-sole-empty", input: `[""]`, slice: []string{""}, array: []string{""}},
				{name: "json-items", input: `["first,second"," a ","line\r\nnext"]`, slice: []string{"first,second", " a ", "line\r\nnext"}, array: []string{"first,second", " a ", "line\r\nnext"}},
				{name: "json-null", input: `[null]`, slice: []string{""}, array: []string{""}},
				{name: "json-mixed-null", input: " \t[\"a\",null]\n", slice: []string{"a", ""}, array: []string{"a", ""}},
				{name: "literal-null", input: `null`, slice: []string{"null"}, array: []string{"null"}},
				{name: "literal-object", input: `{}`, slice: []string{"{}"}, array: []string{"{}"}},
				{name: "csv-quoted-empty", input: `""`, slice: []string{""}, array: []string{`""`}},
				{name: "malformed-prefix", input: `[`, invalid: true},
				{name: "incomplete-json", input: ` ["synthetic-secret"`, invalid: true},
				{name: "json-number", input: `[1]`, invalid: true},
				{name: "malformed-csv", input: `"synthetic-secret`, invalid: kind == "slice", array: []string{`"synthetic-secret`}},
			} {
				t.Run(binder+"/"+kind+"/"+tc.name, func(t *testing.T) {
					t.Setenv("QUALIFICATION_ITEMS", tc.input)
					root := &cobra.Command{Use: "root", SilenceErrors: true, SilenceUsage: true}
					run := &cobra.Command{Use: "run", Run: func(*cobra.Command, []string) {}}
					flags := run.Flags()
					flags.Bool(DebugFlag, true, "")
					var pointer *[]string
					if kind == "slice" {
						pointer = flags.StringSlice("qualification-items", []string{"default"}, "")
					} else {
						pointer = flags.StringArray("qualification-items", []string{"default"}, "")
					}
					options := []EnvironmentBindingOption{WithIgnoredEmptyEnvironmentValues(), WithJSONOrDelimitedEnvironmentCollections()}
					var err error
					if binder == "flag-set" {
						err = BindEnvToFlagSetExact(flags, options...)
					} else {
						run.Args = func(cmd *cobra.Command, args []string) error {
							if err := BindEnvToSelectedCommandExact(cmd, options...); err != nil {
								return err
							}
							return cobra.NoArgs(cmd, args)
						}
						root.AddCommand(run)
						root.SetArgs([]string{"run"})
						err = root.Execute()
					}
					if tc.invalid {
						require.ErrorIs(t, err, ErrInvalidEnvironmentValue)
						require.False(t, flags.Changed("qualification-items"))
						require.Zero(t, flags.NFlag())
						require.NotContains(t, err.Error(), "synthetic-secret")
						require.NotContains(t, errors.Unwrap(err).Error(), "synthetic-secret")
						return
					}
					require.NoError(t, err)
					want := tc.slice
					if kind == "array" {
						want = tc.array
					}
					require.Equal(t, want, *pointer)
					require.Equal(t, want, flags.Lookup("qualification-items").Value.(pflag.SliceValue).GetSlice())
					require.Equal(t, tc.input != "", flags.Changed("qualification-items"))
					if tc.input == "" {
						require.Zero(t, flags.NFlag())
					} else {
						require.Equal(t, 1, flags.NFlag())
					}
				})
			}
		}
	}
}

func TestEnvironmentCompatibilityScalarPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		changed           bool
	}{
		{"empty", "", "default", false},
		{"whitespace", " \t\r\n ", " \t\r\n ", true},
		{"surrounding-space", "  synthetic-token  ", "  synthetic-token  ", true},
		{"literal-array", `["literal"]`, `["literal"]`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("QUALIFICATION_TOKEN", tc.input)
			t.Setenv("QUALIFICATION_LIMIT", "")
			t.Setenv("DEBUG", "true")
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			token := flags.String("qualification-token", "default", "")
			limit := flags.Int("qualification-limit", 42, "")
			debug := flags.Bool(DebugFlag, false, "")
			require.NoError(t, BindEnvToFlagSetExact(flags, WithIgnoredEmptyEnvironmentValues(), WithJSONOrDelimitedEnvironmentCollections()))
			require.Equal(t, tc.want, *token)
			require.Equal(t, tc.changed, flags.Changed("qualification-token"))
			require.Equal(t, 42, *limit)
			require.False(t, flags.Changed("qualification-limit"))
			require.False(t, *debug)
		})
	}
}

func TestEnvironmentBindingOptionsAreIndependent(t *testing.T) {
	t.Setenv("QUALIFICATION_ITEMS", "")
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	items := flags.StringSlice("qualification-items", []string{"default"}, "")
	require.NoError(t, BindEnvToFlagSetExact(flags, WithIgnoredEmptyEnvironmentValues()))
	require.Equal(t, []string{"default"}, *items)
	t.Setenv("QUALIFICATION_ITEMS", "a b")
	require.ErrorIs(t, BindEnvToFlagSetExact(flags, WithIgnoredEmptyEnvironmentValues()), ErrInvalidEnvironmentValue)
	require.NoError(t, BindEnvToFlagSetExact(flags, WithJSONOrDelimitedEnvironmentCollections()))
	require.Equal(t, []string{"a", "b"}, *items)

	t.Setenv("QUALIFICATION_TOKEN", "")
	other := pflag.NewFlagSet("test", pflag.ContinueOnError)
	token := other.String("qualification-token", "default", "")
	require.NoError(t, BindEnvToFlagSetExact(other, WithJSONOrDelimitedEnvironmentCollections()))
	require.Equal(t, "", *token)
	require.True(t, other.Changed("qualification-token"))
}

func TestEnvironmentBindingContinuesWithSafeFirstError(t *testing.T) {
	for _, continued := range []bool{false, true} {
		t.Run(fmt.Sprintf("continued=%t", continued), func(t *testing.T) {
			t.Setenv("QUALIFICATION_A_INVALID", "synthetic-secret")
			t.Setenv("QUALIFICATION_B_VALUE", "diagnostic-config")
			t.Setenv("QUALIFICATION_C_INVALID", "another-secret")
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			flags.Int("qualification-a-invalid", 1, "")
			value := flags.String("qualification-b-value", "default", "")
			flags.Bool("qualification-c-invalid", false, "")
			flags.Bool(DebugFlag, true, "")
			options := []EnvironmentBindingOption{WithIgnoredEmptyEnvironmentValues(), WithJSONOrDelimitedEnvironmentCollections()}
			if continued {
				options = append(options, WithContinuedEnvironmentBinding())
			}
			err := BindEnvToFlagSetExact(flags, options...)
			binding, ok := errors.AsType[*EnvironmentBindingError](err)
			require.True(t, ok)
			require.Equal(t, "qualification-a-invalid", binding.FlagName)
			require.ErrorIs(t, err, ErrInvalidEnvironmentValue)
			require.NotContains(t, err.Error(), "synthetic-secret")
			require.NotContains(t, errors.Unwrap(err).Error(), "synthetic-secret")
			require.NotContains(t, err.Error(), "another-secret")
			require.Equal(t, continued, flags.Changed("qualification-b-value"))
			if continued {
				require.Equal(t, "diagnostic-config", *value)
			} else {
				require.Equal(t, "default", *value)
			}
		})
	}
}

func TestEnvironmentCompatibilityCLIAndSelectedScope(t *testing.T) {
	t.Setenv("QUALIFICATION_ITEMS", `["synthetic-secret"`)
	t.Setenv("QUALIFICATION_UNUSED", "invalid")
	t.Setenv("QUALIFICATION_SHARED", "  exact  ")
	root := &cobra.Command{Use: "root", SilenceErrors: true, SilenceUsage: true}
	shared := root.PersistentFlags().String("qualification-shared", "default", "")
	run := &cobra.Command{Use: "run"}
	pointer := run.Flags().StringSlice("qualification-items", nil, "")
	unused := &cobra.Command{Use: "unused"}
	unused.Flags().Int("qualification-unused", 1, "")
	validated := false
	run.Args = func(cmd *cobra.Command, args []string) error {
		if err := BindEnvToSelectedCommandExact(cmd, WithIgnoredEmptyEnvironmentValues(), WithJSONOrDelimitedEnvironmentCollections(), WithContinuedEnvironmentBinding()); err != nil {
			return err
		}
		require.Equal(t, "  exact  ", *shared)
		validated = true
		return cobra.NoArgs(cmd, args)
	}
	ran := false
	run.Run = func(*cobra.Command, []string) { ran = true }
	root.AddCommand(run, unused)
	root.SetArgs([]string{"run", "--qualification-items=cli"})
	require.NoError(t, root.Execute())
	require.True(t, validated)
	require.True(t, ran)
	require.Equal(t, []string{"cli"}, *pointer)
	require.False(t, unused.Flags().Changed("qualification-unused"))
}
