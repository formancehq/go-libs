package service

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// EnvironmentBindingError identifies a rejected environment setting without
// including its value in the diagnostic unless debug mode is enabled.
// Legacy binders preserve the original parser error through errors.As/errors.Is;
// callers must not log an unwrapped legacy parser error, which can include the
// rejected value. Exact binders discard the parser cause and wrap only
// ErrInvalidEnvironmentValue; debug mode does not disclose their input.
type EnvironmentBindingError struct {
	EnvironmentVariable string
	FlagName            string
	cause               error
	debug               bool
}

func (e *EnvironmentBindingError) Error() string {
	if e.debug {
		return fmt.Sprintf("binding environment variable %s to flag --%s: %v", e.EnvironmentVariable, e.FlagName, e.cause)
	}
	return fmt.Sprintf("binding environment variable %s to flag --%s: invalid value", e.EnvironmentVariable, e.FlagName)
}

func (e *EnvironmentBindingError) Unwrap() error {
	return e.cause
}

// BindEnvToCommand binds environment variables to every flag registered on cmd
// and its descendants.
//
// Deprecated: use BindEnvToCommandWithError so malformed typed values can be
// reported instead of ignored.
func BindEnvToCommand(cmd *cobra.Command) {
	BindEnvToFlagSet(cmd.Flags())
	BindEnvToFlagSet(cmd.PersistentFlags())

	for _, subCmd := range cmd.Commands() {
		BindEnvToCommand(subCmd)
	}
}

// BindEnvToCommandWithError binds environment variables to every flag
// registered on cmd and its descendants. Call it after Cobra has parsed
// command-line arguments; explicitly changed flags are then left untouched so
// command-line values retain precedence over environment values. Callers using
// service.Execute should use Execute's integrated binding instead, which keeps
// environment values available during command initialization while deferring
// malformed-value rejection until after CLI parsing.
func BindEnvToCommandWithError(cmd *cobra.Command) error {
	if err := BindEnvToFlagSetWithError(cmd.Flags()); err != nil {
		return environmentBindingErrorForCommand(err, cmd)
	}
	if err := BindEnvToFlagSetWithError(cmd.PersistentFlags()); err != nil {
		return environmentBindingErrorForCommand(err, cmd)
	}

	for _, subCmd := range cmd.Commands() {
		if err := BindEnvToCommandWithError(subCmd); err != nil {
			return err
		}
	}

	return nil
}

// BindEnvToFlagSet binds environment variables to flags in set.
//
// Deprecated: use BindEnvToFlagSetWithError so malformed typed values can be
// reported instead of ignored.
func BindEnvToFlagSet(set *pflag.FlagSet) {
	_ = bindEnvToFlagSet(set, false)
}

// BindEnvToFlagSetWithError binds environment variables to flags in set. Call
// it after parsing command-line arguments; explicitly changed flags are then
// left untouched so command-line values retain precedence over environment
// values. Callers using service.Execute should use Execute's integrated
// binding instead.
func BindEnvToFlagSetWithError(set *pflag.FlagSet) error {
	err := bindEnvToFlagSet(set, true)
	if binding, ok := err.(*EnvironmentBindingError); ok {
		binding.debug, _ = set.GetBool(DebugFlag)
	}
	return err
}

func bindEnvToFlagSet(set *pflag.FlagSet, failFast bool) error {
	err := bindEnvToFlagSetUsing(set, failFast, bindEnvToFlag)
	if !failFast {
		return nil
	}
	return err
}

func bindEnvToFlagSetUsing(set *pflag.FlagSet, failFast bool, bind func(*pflag.FlagSet, *pflag.Flag) error) error {
	var bindingErr error
	set.VisitAll(func(flag *pflag.Flag) {
		if (bindingErr != nil && failFast) || flag.Changed {
			return
		}
		if err := bind(set, flag); err != nil {
			if bindingErr == nil {
				bindingErr = err
			}

			return
		}
	})

	return bindingErr
}

func bindEnvToFlag(set *pflag.FlagSet, flag *pflag.Flag) error {
	envVar := strings.ToUpper(flag.Name)
	envVar = strings.Replace(envVar, "-", "_", -1)
	// DEBUG is commonly set by tooling and shells; keep debug mode explicit.
	if flag.Name == DebugFlag && envVar == "DEBUG" {
		return nil
	}
	value := os.Getenv(envVar)
	if value == "" {
		return nil
	}
	value = strings.Trim(value, " ")
	switch flag.Value.Type() {
	case "stringSlice":
		if strings.Contains(value, " ") {
			value = strings.Replace(value, " ", ",", -1)
		}
	}

	if err := set.Set(flag.Name, value); err != nil {
		return &EnvironmentBindingError{EnvironmentVariable: envVar, FlagName: flag.Name, cause: err}
	}

	return nil
}

// Resolve debug mode at the reporting boundary: Execute binds before CLI parsing,
// while direct command binding runs after parsing. Flag searches inherited flags
// as well, so a root persistent --debug applies to descendant commands.
func environmentBindingErrorForCommand(err error, cmd *cobra.Command) error {
	binding, ok := err.(*EnvironmentBindingError)
	if !ok {
		return err
	}
	copy := *binding
	debug := cmd.Flag(DebugFlag)
	copy.debug = debug != nil && debug.Value.Type() == "bool" && debug.Value.String() == "true"
	return &copy
}
