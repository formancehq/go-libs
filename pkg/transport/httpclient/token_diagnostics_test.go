package httpclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"golang.org/x/oauth2"
)

type statusOnlyTokenError struct{ status int }

func (e statusOnlyTokenError) Error() string       { panic("token diagnostics must never render errors") }
func (e statusOnlyTokenError) HTTPStatusCode() int { return e.status }

type wrappedTokenError struct{ cause error }

func (e wrappedTokenError) Error() string { panic("token diagnostics must not render wrappers") }
func (e wrappedTokenError) Unwrap() error { return e.cause }

type networkTokenError struct{}

func (networkTokenError) Error() string { panic("token diagnostics must never render network errors") }
func (networkTokenError) Timeout() bool { return true }

func TestBearerRefreshFailureFieldsUsesStatusCapabilityWithoutErrorText(t *testing.T) {
	statusErr := statusOnlyTokenError{status: http.StatusForbidden}
	for _, tc := range []struct {
		name string
		err  error
		want map[string]any
	}{
		{"custom status", statusErr, map[string]any{"error_kind": "token_endpoint_response", "token_endpoint_status": 403}},
		{"wrapped custom status", wrappedTokenError{statusErr}, map[string]any{"error_kind": "token_endpoint_response", "token_endpoint_status": 403}},
		{"legacy response precedence", errors.Join(statusErr, &oauth2.RetrieveError{Response: &http.Response{StatusCode: 401}}), map[string]any{"error_kind": "token_endpoint_response", "token_endpoint_status": 401}},
		{"wrapped status", fmt.Errorf("context: %w", &oauth2.RetrieveError{Response: &http.Response{StatusCode: 401}, Body: []byte("synthetic-secret")}), map[string]any{"error_kind": "token_endpoint_response", "token_endpoint_status": 401}},
		{"joined cancellation", errors.Join(statusErr, context.Canceled), map[string]any{"error_kind": "canceled"}},
		{"joined deadline", errors.Join(statusErr, context.DeadlineExceeded), map[string]any{"error_kind": "deadline"}},
		{"transport", networkTokenError{}, map[string]any{"error_kind": "transport"}},
		{"unknown", errors.New("synthetic-secret"), map[string]any{"error_kind": "invalid_token_response"}},
		{"nil response", &oauth2.RetrieveError{Body: []byte("synthetic-secret")}, map[string]any{"error_kind": "invalid_token_response"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := BearerRefreshFailureFields(tc.err); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}
