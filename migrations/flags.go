package migrations

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	MigratorTableFlag  = "migrator-table"
	MigratorSchemaFlag = "migrator-schema"
)

func AddFlags(flags *pflag.FlagSet) {
	flags.String(MigratorTableFlag, migrationTable, "Table to store the migrations")
	flags.String(MigratorSchemaFlag, "public", "Schema to use for the migrator table")
}

func MigrationOptionsFromFlags(cmd *cobra.Command) []Option {
	table, _ := cmd.Flags().GetString(MigratorTableFlag)
	schema, _ := cmd.Flags().GetString(MigratorSchemaFlag)
	return []Option{
		WithTableName(table),
		WithSchema(schema),
	}
}
