package migrations

import (
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

func MigrationOptionsFromFlags(flags *pflag.FlagSet) []Option {
	table, _ := flags.GetString(MigratorTableFlag)
	schema, _ := flags.GetString(MigratorSchemaFlag)
	return []Option{
		WithTableName(table),
		WithSchema(schema),
	}
}
