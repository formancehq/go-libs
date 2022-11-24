package sharedhealth_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	sharedhealth "github.com/formancehq/go-libs/sharedhealth/pkg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

func TestHealthController(t *testing.T) {
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
				func() sharedhealth.NamedCheck {
					return sharedhealth.NewNamedCheck("test1", sharedhealth.CheckFn(func(ctx context.Context) error {
						return nil
					}))
				},
				func() sharedhealth.NamedCheck {
					return sharedhealth.NewNamedCheck("test2", sharedhealth.CheckFn(func(ctx context.Context) error {
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
				func() sharedhealth.NamedCheck {
					return sharedhealth.NewNamedCheck("test1", sharedhealth.CheckFn(func(ctx context.Context) error {
						return nil
					}))
				},
				func() sharedhealth.NamedCheck {
					return sharedhealth.NewNamedCheck("test2", sharedhealth.CheckFn(func(ctx context.Context) error {
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
		options = append(options, sharedhealth.Module(), fx.NopLogger)
		for _, p := range tc.healthChecksProvider {
			options = append(options, sharedhealth.ProvideHealthCheck(p))
		}
		options = append(options, fx.Invoke(func(ctrl *sharedhealth.HealthController) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/_health", nil)
			ctrl.Check(rec, req)
			assert.Equal(t, tc.expectedStatus, rec.Result().StatusCode)

			ret := make(map[string]string)
			assert.NoError(t, json.NewDecoder(rec.Result().Body).Decode(&ret))
			assert.Equal(t, tc.expectedResult, ret)
		}))
		app := fx.New(options...)
		require.NoError(t, app.Err())
	}
}
