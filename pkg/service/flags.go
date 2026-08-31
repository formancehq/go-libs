package service

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

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
// registered on cmd and its descendants. Explicitly changed flags are left
// untouched so command-line values retain precedence over environment values.
func BindEnvToCommandWithError(cmd *cobra.Command) error {
	if err := BindEnvToFlagSetWithError(cmd.Flags()); err != nil {
		return err
	}
	if err := BindEnvToFlagSetWithError(cmd.PersistentFlags()); err != nil {
		return err
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

// BindEnvToFlagSetWithError binds environment variables to flags in set.
// Explicitly changed flags are left untouched so command-line values retain
// precedence over environment values.
func BindEnvToFlagSetWithError(set *pflag.FlagSet) error {
	return bindEnvToFlagSet(set, true)
}

func bindEnvToFlagSet(set *pflag.FlagSet, failFast bool) error {
	var bindingErr error
	set.VisitAll(func(flag *pflag.Flag) {
		if bindingErr != nil || flag.Changed {
			return
		}
		if err := bindEnvToFlag(set, flag); err != nil {
			if failFast {
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
		return fmt.Errorf("binding environment variable %s to flag --%s: %w", envVar, flag.Name, err)
	}

	return nil
}
