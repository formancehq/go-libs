package audit

import (
	"fmt"

	"github.com/spf13/pflag"
)

const (
	AuditEnabledFlag = "audit-enabled"
)

type Config struct {
	Enabled bool
}

func AddFlags(flags *pflag.FlagSet) {
	flags.Bool(AuditEnabledFlag, false, "Enable audit")
}

func ConfigFromFlags(flags *pflag.FlagSet) (Config, error) {
	enabled, err := flags.GetBool(AuditEnabledFlag)
	if err != nil {
		return Config{}, fmt.Errorf("failed to read %s flag: %w", AuditEnabledFlag, err)
	}

	return Config{
		Enabled: enabled,
	}, nil
}
