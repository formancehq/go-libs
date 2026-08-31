package service

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func Execute(cmd *cobra.Command) {
	bindEnvForExecute(cmd)
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// bindEnvForExecute preserves the existing early environment binding so Cobra
// initializers can observe valid values. Parse failures are deferred until
// argument validation, after Cobra has marked explicit CLI flags Changed. A
// malformed environment value therefore fails only when no higher-precedence
// CLI value replaced it, and always before pre-run or run hooks start service
// components.
func bindEnvForExecute(cmd *cobra.Command) {
	bindingErrors := make(map[*pflag.Flag]error)
	bindEnvToCommandWithDeferredErrors(cmd, bindingErrors)
	rejectSelectedEnvErrorsBeforeRun(cmd, bindingErrors)
}

func bindEnvToCommandWithDeferredErrors(cmd *cobra.Command, bindingErrors map[*pflag.Flag]error) {
	bindEnvToFlagSetWithDeferredErrors(cmd.Flags(), bindingErrors)
	bindEnvToFlagSetWithDeferredErrors(cmd.PersistentFlags(), bindingErrors)

	for _, subCmd := range cmd.Commands() {
		bindEnvToCommandWithDeferredErrors(subCmd, bindingErrors)
	}
}

func bindEnvToFlagSetWithDeferredErrors(set *pflag.FlagSet, bindingErrors map[*pflag.Flag]error) {
	set.VisitAll(func(flag *pflag.Flag) {
		if flag.Changed {
			return
		}
		if _, alreadyBound := bindingErrors[flag]; alreadyBound {
			return
		}
		if err := bindEnvToFlag(set, flag); err != nil {
			bindingErrors[flag] = err
		}
	})
}

func rejectSelectedEnvErrorsBeforeRun(cmd *cobra.Command, bindingErrors map[*pflag.Flag]error) {
	originalArgs := cmd.Args
	cmd.Args = func(cmd *cobra.Command, args []string) error {
		var selectedError error
		cmd.Flags().VisitAll(func(flag *pflag.Flag) {
			if selectedError != nil || flag.Changed {
				return
			}
			selectedError = bindingErrors[flag]
		})
		if selectedError != nil {
			return selectedError
		}
		if originalArgs != nil {
			return originalArgs(cmd, args)
		}

		return nil
	}

	for _, subCmd := range cmd.Commands() {
		rejectSelectedEnvErrorsBeforeRun(subCmd, bindingErrors)
	}
}
