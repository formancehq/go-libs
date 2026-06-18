package authnfx_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/formancehq/go-libs/v5/pkg/authn/jwt"
	"github.com/formancehq/go-libs/v5/pkg/fx/authnfx"
	"github.com/formancehq/go-libs/v5/pkg/fx/messagingfx"
	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

// Regression test for TS-456: JWTModule and messagingfx.HTTPModule both
// supplied an unnamed *http.Client which fx rejected as a duplicate provider.
func TestJWTAndHTTPPublisherCoexist(t *testing.T) {
	t.Parallel()

	app := fxtest.New(t,
		fx.NopLogger,
		fx.Supply(fx.Annotate(logging.Testing(), fx.As(new(logging.Logger)))),
		authnfx.JWTModule(jwt.Config{}),
		messagingfx.Module(map[string]string{}),
		messagingfx.HTTPModule(),
	)
	require.NoError(t, app.Err())
}
