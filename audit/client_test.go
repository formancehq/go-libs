package audit_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/formancehq/go-libs/v3/audit"
	"go.uber.org/zap"
)

func TestClientDisabled(t *testing.T) {
	cfg := audit.DefaultConfig("test")
	cfg.Enabled = false

	client, err := audit.NewClient(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	audit.HTTPMiddleware(client)(handler).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}
