package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/formancehq/go-libs/v5/pkg/fx/servicefx"
	"github.com/formancehq/go-libs/v5/pkg/service/health"
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
		options = append(options, servicefx.HealthModule(), fx.NopLogger)
		for _, p := range tc.healthChecksProvider {
			options = append(options, servicefx.ProvideHealthCheck(p))
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

func TestHealthControllerReturnsWhenContextCanceledDuringHungCheck(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	ctrl := health.NewHealthController(
		health.NewNamedCheck("hung", health.CheckFn(func(ctx context.Context) error {
			close(started)
			<-release
			return nil
		})),
	)
	t.Cleanup(func() {
		close(release)
	})

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/_health", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		ctrl.Check(rec, req)
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("health check did not start")
	}

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("health controller did not return after request context cancellation")
	}

	require.Equal(t, http.StatusInternalServerError, rec.Result().StatusCode)

	ret := make(map[string]string)
	require.NoError(t, json.NewDecoder(rec.Result().Body).Decode(&ret))
	require.Equal(t, map[string]string{
		"hung": context.Canceled.Error(),
	}, ret)
}
