package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	logging "github.com/formancehq/go-libs/v5/pkg/observe/log"
)

type chiLogEntry struct {
	r *http.Request
}

func (c *chiLogEntry) Write(status, bytes int, _ http.Header, elapsed time.Duration, extra interface{}) {
	fields := map[string]any{
		"status":  status,
		"bytes":   bytes,
		"elapsed": elapsed,
	}
	if extra != nil {
		fields["extra"] = extra
	}
	logging.FromContext(c.r.Context()).
		WithFields(fields).
		Infof("%s %s", c.r.Method, c.r.URL.Path)
}

func (c *chiLogEntry) Panic(v interface{}, stack []byte) {
	logging.FromContext(c.r.Context()).
		WithFields(map[string]any{
			"panic": fmt.Sprintf("%v", v),
			"stack": string(stack),
		}).
		Errorf("%s %s: panic recovered", c.r.Method, c.r.URL.Path)
}

var _ middleware.LogEntry = (*chiLogEntry)(nil)

type chiLogFormatter struct{}

func (c chiLogFormatter) NewLogEntry(r *http.Request) middleware.LogEntry {
	return &chiLogEntry{
		r: r,
	}
}

var _ middleware.LogFormatter = (*chiLogFormatter)(nil)

func NewLogFormatter() *chiLogFormatter {
	return &chiLogFormatter{}
}
