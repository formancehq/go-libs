package service

import (
	"os"

	"github.com/spf13/cobra"
)

func Execute(cmd *cobra.Command) {
	BindEnvToCommand(cmd)
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
