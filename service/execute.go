package service

import (
	"os"

	"github.com/spf13/cobra"
)

var osExit = os.Exit

func Execute(cmd *cobra.Command) {
	BindEnvToCommand(cmd)
	if err := cmd.Execute(); err != nil {
		osExit(1)
	}
}
