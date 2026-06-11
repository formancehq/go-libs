package audit

import (
	"fmt"

	"github.com/spf13/pflag"
)

const (
	AuditEnabledFlag             = "audit-enabled"
	AuditHandledHeaderSecretFlag = "audit-handled-header-secret"
)

type Config struct {
	Enabled bool
	// HandledHeaderSecret is the shared secret required for the audit
	// middleware to honor the HandledHeader dedup header. Without it, any
	// client can spoof the header and bypass the audit trail.
	HandledHeaderSecret string
}

func AddFlags(flags *pflag.FlagSet) {
	flags.Bool(AuditEnabledFlag, false, "Enable audit")
	flags.String(AuditHandledHeaderSecretFlag, "", "Shared secret required to honor the audit-handled dedup header; without it the header is spoofable by external clients")
}

func ConfigFromFlags(flags *pflag.FlagSet) (Config, error) {
	enabled, err := flags.GetBool(AuditEnabledFlag)
	if err != nil {
		return Config{}, fmt.Errorf("failed to read %s flag: %w", AuditEnabledFlag, err)
	}

	handledHeaderSecret, err := flags.GetString(AuditHandledHeaderSecretFlag)
	if err != nil {
		return Config{}, fmt.Errorf("failed to read %s flag: %w", AuditHandledHeaderSecretFlag, err)
	}

	return Config{
		Enabled:             enabled,
		HandledHeaderSecret: handledHeaderSecret,
	}, nil
}
