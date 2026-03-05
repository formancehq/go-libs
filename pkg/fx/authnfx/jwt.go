package authnfx

import (
	"net/http"

	"github.com/spf13/cobra"
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v5/pkg/authn/jwt"
	"github.com/formancehq/go-libs/v5/pkg/authn/oidc"
)

func JWTModule(cfg jwt.ModuleConfig) fx.Option {
	options := jwtModuleOptions("")
	options = append(options, fx.Provide(func() jwt.ModuleConfig {
		return cfg
	}))
	return fx.Module("auth", options...)
}

func AnnotatedJWTModule(cfg jwt.ModuleConfig, annotationTag string) fx.Option {
	nameAnnotation := `name:"` + annotationTag + `"`
	options := jwtModuleOptions(nameAnnotation)
	options = append(options, fx.Provide(fx.Annotate(func() jwt.ModuleConfig {
		return cfg
	}, fx.ResultTags(nameAnnotation))))
	return fx.Module("auth", options...)
}

func jwtModuleOptions(nameAnnotation string) []fx.Option {
	options := make([]fx.Option, 0)
	if nameAnnotation == "" {
		options = append(options,
			fx.Supply(http.DefaultClient),
			fx.Provide(jwt.NewKeySet),
			fx.Provide(jwt.NewAuthenticatorFromConfig),
		)
		return options
	}

	options = append(options, fx.Provide(
		fx.Annotate(func() *http.Client {
			return http.DefaultClient
		}, fx.ResultTags(nameAnnotation)),
	))
	options = append(options, fx.Provide(
		fx.Annotate(jwt.NewKeySet, fx.ParamTags(nameAnnotation, nameAnnotation), fx.ResultTags(nameAnnotation, ``)),
	))
	options = append(options, fx.Provide(
		fx.Annotate(jwt.NewAuthenticatorFromConfig, fx.ParamTags(nameAnnotation, nameAnnotation), fx.ResultTags(nameAnnotation)),
	))
	return options
}

func JWTModuleFromFlags(cmd *cobra.Command) fx.Option {
	return JWTModule(jwt.ConfigFromFlags(cmd.Flags()))
}

func OrganizationAwareJWTModuleFromFlags(cmd *cobra.Command, fn jwt.OrganizationIDProvider) fx.Option {
	cfg := jwt.ConfigFromFlags(cmd.Flags())
	cfg.AdditionalChecks = append(cfg.AdditionalChecks, jwt.CheckOrganizationIDClaim(fn))
	return JWTModule(cfg)
}

func AdditionalChecksJWTModuleFromFlags(cmd *cobra.Command, checks ...jwt.AdditionalCheck) fx.Option {
	cfg := jwt.ConfigFromFlags(cmd.Flags())
	cfg.AdditionalChecks = append(cfg.AdditionalChecks, checks...)
	return JWTModule(cfg)
}

// Compatibility: re-export the Authenticator type
type Authenticator = jwt.Authenticator
type KeySet = oidc.KeySet
