package authnfx

import (
	"context"

	"github.com/spf13/cobra"
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v5/pkg/authn/licence"
	"github.com/formancehq/go-libs/v5/pkg/errors"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

func LicenceModuleFromFlags(
	cmd *cobra.Command,
	serviceName string,
) fx.Option {
	if !licence.IsEnabled() {
		return fx.Options(
			fx.Supply(&licence.Licence{}),
		)
	}

	licenceChanError := make(chan error, 1)

	licenceToken, _ := cmd.Flags().GetString(licence.LicenceTokenFlag)
	licenceValidateTick, _ := cmd.Flags().GetDuration(licence.LicenceValidateTickFlag)
	licenceClusterID, _ := cmd.Flags().GetString(licence.LicenceClusterIDFlag)
	licenceExpectedIssuer, _ := cmd.Flags().GetString(licence.LicenceExpectedIssuerFlag)

	return fx.Options(
		fx.Provide(func(logger logging.Logger) *licence.Licence {
			return licence.NewLicence(
				logger,
				licenceToken,
				licenceValidateTick,
				serviceName,
				licenceClusterID,
				licenceExpectedIssuer,
			)
		}),
		fx.Invoke(func(lc fx.Lifecycle, l *licence.Licence, shutdowner fx.Shutdowner) {
			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					if err := l.Start(licenceChanError); err != nil {
						return errors.NewErrorWithExitCode(err, 126)
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
			_ = shutdowner.Shutdown(fx.ExitCode(126))
			return
		}
	}
}
