package connect

import (
	"context"
	"database/sql/driver"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

func buildPGXConnector(config *pgx.ConnConfig) driver.Connector {
	if config.ValidateConnect == nil {
		config.ValidateConnect = pgconn.ValidateConnectTargetSessionAttrsReadWrite
	}
	config.Tracer = newPgxTracer()

	return stdlib.GetConnector(*config, stdlib.OptionResetSession(resetReadOnlySession))
}

func resetReadOnlySession(ctx context.Context, conn *pgx.Conn) error {
	var readOnly string
	if err := conn.QueryRow(ctx, "show transaction_read_only").Scan(&readOnly); err != nil {
		return err
	}
	if readOnly == "on" {
		return driver.ErrBadConn
	}

	var defaultReadOnly string
	if err := conn.QueryRow(ctx, "show default_transaction_read_only").Scan(&defaultReadOnly); err != nil {
		return err
	}
	if defaultReadOnly == "on" {
		return driver.ErrBadConn
	}

	return nil
}
