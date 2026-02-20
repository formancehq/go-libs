package bundebug

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/formancehq/go-libs/v4/logging"
)

type QueryHook struct {
	Debug bool
}

var _ bun.QueryHook = (*QueryHook)(nil)

func NewQueryHook() *QueryHook {
	return &QueryHook{}
}

func (h *QueryHook) BeforeQuery(
	ctx context.Context, _ *bun.QueryEvent,
) context.Context {
	return ctx
}

func (h *QueryHook) AfterQuery(ctx context.Context, event *bun.QueryEvent) {
	if !h.Debug && !isDebug(ctx) {
		return
	}

	dur := time.Since(event.StartTime)

	fields := map[string]any{
		"component": "bun",
		"operation": event.Operation(),
		"duration":  fmt.Sprintf("%s", dur.Round(time.Microsecond)),
	}

	if event.Err != nil {
		fields["err"] = event.Err.Error()
	}

	queryLines := strings.SplitN(event.Query, "\n", 2)
	query := queryLines[0]
	if len(queryLines) > 1 {
		query = query + "..."
	}

	logging.FromContext(ctx).WithFields(fields).Debug(query)
}

type contextKey string

var debugContextKey contextKey = "debug"

func WithDebug(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, debugContextKey, true)
}

func isDebug(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	debug := ctx.Value(debugContextKey)
	return debug != nil && debug.(bool)
}
