package bunconnect

import (
	"context"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/trace"
)

type pgxTracer struct{}

func (p pgxTracer) TraceConnectStart(ctx context.Context, _ pgx.TraceConnectStartData) context.Context {
	ctx, _ = tracer.Start(ctx, "connect")
	return ctx
}

func (p pgxTracer) TraceConnectEnd(ctx context.Context, data pgx.TraceConnectEndData) {
	span := trace.SpanFromContext(ctx)
	defer span.End()
	if data.Err != nil {
		span.RecordError(data.Err)
	}
}

func (p pgxTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	ctx, _ = tracer.Start(ctx, "query")
	return ctx
}

func (p pgxTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span := trace.SpanFromContext(ctx)
	defer span.End()
	if data.Err != nil {
		span.RecordError(data.Err)
	}
}

func newPgxTracer() pgx.QueryTracer {
	return &pgxTracer{}
}

var (
	_ pgx.QueryTracer   = &pgxTracer{}
	_ pgx.ConnectTracer = &pgxTracer{}
)
