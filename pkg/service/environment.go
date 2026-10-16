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

// EnvironmentBindingOption configures the opt-in environment binders. It does
// not affect the legacy binders or Execute.
type EnvironmentBindingOption func(*environmentBindingOptions)

type environmentBindingOptions struct {
	continueOnError            bool
	ignoreEmpty                bool
	jsonOrDelimitedCollections bool
}

// WithContinuedEnvironmentBinding attempts the remaining flags after an invalid
// value and returns the first error. No error is suppressed; components must not
// start if binding fails. This is useful when later flags configure diagnostics.
func WithContinuedEnvironmentBinding() EnvironmentBindingOption {
	return func(options *environmentBindingOptions) { options.continueOnError = true }
}

// WithIgnoredEmptyEnvironmentValues leaves defaults unchanged for present empty
// variables, including typed flags. Whitespace-only values are not empty.
func WithIgnoredEmptyEnvironmentValues() EnvironmentBindingOption {
	return func(options *environmentBindingOptions) { options.ignoreEmpty = true }
}

// WithJSONOrDelimitedEnvironmentCollections accepts JSON arrays when the first
// non-whitespace character is '['; malformed JSON never falls back to delimiters.
// Arrays use encoding/json's []string decoding, including null items as empty
// strings. Otherwise stringSlice values use pflag CSV parsing after replacing
// ASCII spaces with commas, and stringArray values are one unmodified item.
// This option does not trim strings or other scalar values.
func WithJSONOrDelimitedEnvironmentCollections() EnvironmentBindingOption {
	return func(options *environmentBindingOptions) { options.jsonOrDelimitedCollections = true }
}

// BindEnvToFlagSetExact binds environment values after CLI parsing. Changed
// flags win; present empty strings are values. Strings are preserved byte for
// byte. By default, string slices and arrays require JSON arrays containing
// only strings; options can enable JSON-or-delimited collections and ignore empty
// variables without changing legacy APIs.
// Read collections through their typed pointer or pflag.SliceValue.GetSlice;
// pflag convenience getters may lose empty items and normalize CRLF via CSV.
// DEBUG is never bound. Diagnostics remain redacted even when debug is enabled.
// Successful bindings use FlagSet.Set bookkeeping, including Changed and Visit.
// Binding is not transactional: successful flags remain set on error. The default
// stops at the first invalid setting; WithContinuedEnvironmentBinding attempts
// all remaining flags and still returns the first error.
func BindEnvToFlagSetExact(set *pflag.FlagSet, options ...EnvironmentBindingOption) error {
	config := environmentBindingOptions{}
	for _, option := range options {
		option(&config)
	}
	return bindEnvToFlagSetUsing(set, !config.continueOnError, func(set *pflag.FlagSet, flag *pflag.Flag) error {
		return bindExactEnvironmentToFlag(set, flag, config)
	})
}

func bindExactEnvironmentToFlag(set *pflag.FlagSet, flag *pflag.Flag, options environmentBindingOptions) error {
	if flag.Name == DebugFlag {
		return nil
	}
	name := strings.ReplaceAll(strings.ToUpper(flag.Name), "-", "_")
	value, present := os.LookupEnv(name)
	if !present || (options.ignoreEmpty && value == "") {
		return nil
	}
	if err := setExactEnvironmentValue(set, flag, value, options); err != nil {
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
func BindEnvToSelectedCommandExact(cmd *cobra.Command, options ...EnvironmentBindingOption) error {
	flags := cmd.Flags()
	flags.AddFlagSet(cmd.LocalFlags())
	flags.AddFlagSet(cmd.InheritedFlags())
	// Bind against the command's own set so its Visit/NFlag bookkeeping agrees
	// with Changed, including for inherited flags merged by Cobra during parsing.
	return BindEnvToFlagSetExact(flags, options...)
}

func setExactEnvironmentValue(set *pflag.FlagSet, flag *pflag.Flag, value string, options environmentBindingOptions) error {
	switch flag.Value.Type() {
	case "stringSlice", "stringArray":
		if options.jsonOrDelimitedCollections && !strings.HasPrefix(strings.TrimSpace(value), "[") {
			if flag.Value.Type() == "stringSlice" {
				value = strings.ReplaceAll(value, " ", ",")
			}
			return set.Set(flag.Name, value)
		}
		var values []string
		if options.jsonOrDelimitedCollections {
			if err := json.Unmarshal([]byte(value), &values); err != nil {
				return ErrInvalidEnvironmentValue
			}
		} else {
			var elements []json.RawMessage
			if err := json.Unmarshal([]byte(value), &elements); err != nil || elements == nil {
				return ErrInvalidEnvironmentValue
			}
			values = make([]string, len(elements))
			for i, element := range elements {
				// Strict mode rejects null, which encoding/json otherwise decodes as "".
				if len(element) == 0 || element[0] != '"' {
					return ErrInvalidEnvironmentValue
				}
				if err := json.Unmarshal(element, &values[i]); err != nil {
					return ErrInvalidEnvironmentValue
				}
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
