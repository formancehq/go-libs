package auth

import (
	"github.com/spf13/cobra"
	flag "github.com/spf13/pflag"
	"go.uber.org/fx"
)

const (
	AuthEnabledFlag              = "auth-enabled"
	AuthIssuerFlag               = "auth-issuer"
	AuthReadKeySetMaxRetriesFlag = "auth-read-key-set-max-retries"
	AuthCheckScopesFlag          = "auth-check-scopes"
	AuthServiceFlag              = "auth-service"
)

func AddFlags(flags *flag.FlagSet) {
	flags.Bool(AuthEnabledFlag, false, "Enable auth")
	flags.String(AuthIssuerFlag, "", "Issuer")
	flags.Int(AuthReadKeySetMaxRetriesFlag, 10, "ReadKeySetMaxRetries")
	flags.Bool(AuthCheckScopesFlag, false, "CheckScopes")
	flags.String(AuthServiceFlag, "", "Service")
}

func ModuleConfigFromFlags(cmd *cobra.Command) ModuleConfig {
	authEnabled, _ := cmd.Flags().GetBool(AuthEnabledFlag)
	authIssuer, _ := cmd.Flags().GetString(AuthIssuerFlag)
	authReadKeySetMaxRetries, _ := cmd.Flags().GetInt(AuthReadKeySetMaxRetriesFlag)
	authCheckScopes, _ := cmd.Flags().GetBool(AuthCheckScopesFlag)
	authService, _ := cmd.Flags().GetString(AuthServiceFlag)

	return ModuleConfig{
		Enabled:              authEnabled,
		Issuer:               authIssuer,
		ReadKeySetMaxRetries: authReadKeySetMaxRetries,
		CheckScopes:          authCheckScopes,
		Service:              authService,
		AdditionalChecks:     make([]AdditionalCheck, 0),
	}
}

func FXModuleFromFlags(cmd *cobra.Command) fx.Option {
	return Module(ModuleConfigFromFlags(cmd))
}

func OrganizationAwareFXModuleFromFlags(cmd *cobra.Command, fn OrganizationIDProvider) fx.Option {
	cfg := ModuleConfigFromFlags(cmd)
	cfg.AdditionalChecks = append(cfg.AdditionalChecks, CheckOrganizationIDClaim(fn))
	return Module(cfg)
}

func AdditionalChecksFXModuleFromFlags(cmd *cobra.Command, checks ...AdditionalCheck) fx.Option {
	cfg := ModuleConfigFromFlags(cmd)
	cfg.AdditionalChecks = append(cfg.AdditionalChecks, checks...)
	return Module(cfg)
}
