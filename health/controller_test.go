package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v3/health"
)

func TestHealthController(t *testing.T) {
	t.Parallel()
	type testCase struct {
		name                 string
		healthChecksProvider []any
		expectedStatus       int
		expectedResult       map[string]string
	}

	var tests = []testCase{
		{
			name: "all-ok",
			healthChecksProvider: []any{
				func() health.NamedCheck {
					return health.NewNamedCheck("test1", health.CheckFn(func(ctx context.Context) error {
						return nil
					}))
				},
				func() health.NamedCheck {
					return health.NewNamedCheck("test2", health.CheckFn(func(ctx context.Context) error {
						return nil
					}))
				},
			},
			expectedStatus: http.StatusOK,
			expectedResult: map[string]string{
				"test1": "OK",
				"test2": "OK",
			},
		},
		{
			name: "one-failing",
			healthChecksProvider: []any{
				func() health.NamedCheck {
					return health.NewNamedCheck("test1", health.CheckFn(func(ctx context.Context) error {
						return nil
					}))
				},
				func() health.NamedCheck {
					return health.NewNamedCheck("test2", health.CheckFn(func(ctx context.Context) error {
						return errors.New("failure")
					}))
				},
			},
			expectedStatus: http.StatusInternalServerError,
			expectedResult: map[string]string{
				"test1": "OK",
				"test2": "failure",
			},
		},
	}

	for _, tc := range tests {
		options := make([]fx.Option, 0)
		options = append(options, health.Module(), fx.NopLogger)
		for _, p := range tc.healthChecksProvider {
			options = append(options, health.ProvideHealthCheck(p))
		}
		options = append(options, fx.Invoke(func(ctrl *health.HealthController) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/_health", nil)
			ctrl.Check(rec, req)
			require.Equal(t, tc.expectedStatus, rec.Result().StatusCode)

			ret := make(map[string]string)
			require.NoError(t, json.NewDecoder(rec.Result().Body).Decode(&ret))
			require.Equal(t, tc.expectedResult, ret)
		}))
		app := fx.New(options...)
		require.NoError(t, app.Err())
	}
}
