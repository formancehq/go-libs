package licence

import (
	"context"
	"time"

	"github.com/spf13/pflag"

	"github.com/formancehq/go-libs/v2/errorsutils"
	"github.com/formancehq/go-libs/v2/logging"
	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

var licenceEnabled = false

const (
	LicenceTokenFlag          = "licence-token"
	LicenceValidateTickFlag   = "licence-validate-tick"
	LicenceClusterIDFlag      = "licence-cluster-id"
	LicenceExpectedIssuerFlag = "licence-issuer"
)

func AddFlags(flags *pflag.FlagSet) {
	flags.String(LicenceTokenFlag, "", "Licence token")
	flags.Duration(LicenceValidateTickFlag, 2*time.Minute, "Licence validate tick")
	flags.String(LicenceClusterIDFlag, "", "Licence cluster ID")
	flags.String(LicenceExpectedIssuerFlag, "", "Licence expected issuer")
}

func FXModuleFromFlags(
	cmd *cobra.Command,
	serviceName string,
) fx.Option {
	if !licenceEnabled {
		return fx.Options()
	}

	licenceChanError := make(chan error, 1)

	licenceToken, _ := cmd.Flags().GetString(LicenceTokenFlag)
	licenceValidateTick, _ := cmd.Flags().GetDuration(LicenceValidateTickFlag)
	licenceClusterID, _ := cmd.Flags().GetString(LicenceClusterIDFlag)
	licenceExpectedIssuer, _ := cmd.Flags().GetString(LicenceExpectedIssuerFlag)

	return fx.Options(
		fx.Provide(func(logger logging.Logger) *Licence {
			return NewLicence(
				logger,
				licenceToken,
				licenceValidateTick,
				serviceName,
				licenceClusterID,
				licenceExpectedIssuer,
			)
		}),
		fx.Invoke(func(lc fx.Lifecycle, l *Licence, shutdowner fx.Shutdowner) {
			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					if err := l.Start(licenceChanError); err != nil {
						return errorsutils.NewErrorWithExitCode(err, 126)
					}

					go waitLicenceError(licenceChanError, shutdowner)

					return nil
				},
				OnStop: func(ctx context.Context) error {
					l.Stop()
					close(licenceChanError)
					return nil
				},
			})
		}))
}

func waitLicenceError(
	licenceErrorChan chan error,
	shutdowner fx.Shutdowner,
) {
	for err := range licenceErrorChan {
		if err != nil {
			shutdowner.Shutdown(fx.ExitCode(126))
			return
		}
	}
}
