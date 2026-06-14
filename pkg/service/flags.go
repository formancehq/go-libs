package service

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func BindEnvToCommand(cmd *cobra.Command) {
	BindEnvToFlagSet(cmd.Flags())
	BindEnvToFlagSet(cmd.PersistentFlags())

	for _, subCmd := range cmd.Commands() {
		BindEnvToCommand(subCmd)
	}
}

func BindEnvToFlagSet(set *pflag.FlagSet) {
	set.VisitAll(func(flag *pflag.Flag) {
		envVar := strings.ToUpper(flag.Name)
		envVar = strings.Replace(envVar, "-", "_", -1)
		// DEBUG is commonly set by tooling and shells; keep debug mode explicit.
		if flag.Name == DebugFlag && envVar == "DEBUG" {
			return
		}
		value := os.Getenv(envVar)
		if value == "" {
			return
		}
		value = strings.Trim(value, " ")
		switch flag.Value.Type() {
		case "stringSlice":
			if strings.Contains(value, " ") {
				value = strings.Replace(value, " ", ",", -1)
			}
		}

		if err := set.Set(flag.Name, value); err != nil {
			return
		}
	})
}
