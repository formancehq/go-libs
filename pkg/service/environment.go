package service

import (
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ErrInvalidEnvironmentValue identifies an invalid setting without retaining
// potentially sensitive parser input. Exact binding errors wrap this sentinel.
var ErrInvalidEnvironmentValue = errors.New("invalid environment value")

// BindEnvToFlagSetExact binds environment values after CLI parsing. Changed
// flags win; present empty strings are values. Strings are preserved byte for
// byte. String slices and arrays require JSON arrays containing only strings.
// Read collections through their typed pointer or pflag.SliceValue.GetSlice;
// pflag convenience getters may lose empty items and normalize CRLF via CSV.
// DEBUG is never bound. Diagnostics remain redacted even when debug is enabled.
// Successful bindings use FlagSet.Set bookkeeping, including Changed and Visit.
// Binding is not transactional: earlier successful flags remain set on error.
func BindEnvToFlagSetExact(set *pflag.FlagSet) error {
	return bindEnvToFlagSetUsing(set, true, bindExactEnvironmentToFlag)
}

func bindExactEnvironmentToFlag(set *pflag.FlagSet, flag *pflag.Flag) error {
	if flag.Name == DebugFlag {
		return nil
	}
	name := strings.ReplaceAll(strings.ToUpper(flag.Name), "-", "_")
	value, present := os.LookupEnv(name)
	if !present {
		return nil
	}
	if err := setExactEnvironmentValue(set, flag, value); err != nil {
		// Discard the parser error entirely: arbitrary Value implementations can
		// embed credentials in both their message and exported error fields.
		return &EnvironmentBindingError{EnvironmentVariable: name, FlagName: flag.Name, cause: ErrInvalidEnvironmentValue}
	}
	return nil
}

// BindEnvToSelectedCommandExact binds only cmd's effective flags, including
// inherited persistent flags and respecting local shadowing. Pass the selected
// command after CLI parsing (for example from its Args validator). Descendant,
// sibling and ancestor-local flags are not bound. Unlike Execute's legacy
// binding, values are not available to Cobra initialization callbacks.
func BindEnvToSelectedCommandExact(cmd *cobra.Command) error {
	flags := cmd.Flags()
	flags.AddFlagSet(cmd.LocalFlags())
	flags.AddFlagSet(cmd.InheritedFlags())
	// Bind against the command's own set so its Visit/NFlag bookkeeping agrees
	// with Changed, including for inherited flags merged by Cobra during parsing.
	return BindEnvToFlagSetExact(flags)
}

func setExactEnvironmentValue(set *pflag.FlagSet, flag *pflag.Flag, value string) error {
	switch flag.Value.Type() {
	case "stringSlice", "stringArray":
		var elements []json.RawMessage
		if err := json.Unmarshal([]byte(value), &elements); err != nil || elements == nil {
			return ErrInvalidEnvironmentValue
		}
		values := make([]string, len(elements))
		for i, element := range elements {
			// JSON null unmarshals into string without error, but is not a string.
			if len(element) == 0 || element[0] != '"' {
				return ErrInvalidEnvironmentValue
			}
			if err := json.Unmarshal(element, &values[i]); err != nil {
				return ErrInvalidEnvironmentValue
			}
		}
		slice, ok := flag.Value.(pflag.SliceValue)
		if !ok {
			return ErrInvalidEnvironmentValue
		}
		original := flag.Value
		// Replace bypasses CSV and append semantics, preserving [] and [""].
		// Let FlagSet.Set still perform all of pflag's successful-set bookkeeping.
		flag.Value = environmentSliceValue{Value: original, slice: slice, values: values}
		defer func() { flag.Value = original }()
	}
	return set.Set(flag.Name, value)
}

type environmentSliceValue struct {
	pflag.Value
	slice  pflag.SliceValue
	values []string
}

func (v environmentSliceValue) Set(string) error { return v.slice.Replace(v.values) }
