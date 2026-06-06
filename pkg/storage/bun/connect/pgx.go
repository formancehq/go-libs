package connect

import (
	"context"
	"database/sql/driver"
	"errors"
	"net"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

var readWriteProbeTimeout = 5 * time.Second

func buildPGXConnector(config *pgx.ConnConfig) driver.Connector {
	if config.ValidateConnect == nil {
		config.ValidateConnect = validateConnectTargetSessionAttrsReadWrite
	}
	config.Tracer = newPgxTracer()

	return stdlib.GetConnector(*config, stdlib.OptionResetSession(resetReadOnlySession))
}

func validateConnectTargetSessionAttrsReadWrite(ctx context.Context, pgConn *pgconn.PgConn) error {
	return withPgConnDeadline(pgConn, func() error {
		result, err := pgConn.Exec(ctx, "show transaction_read_only").ReadAll()
		if err != nil {
			return err
		}

		if string(result[0].Rows[0][0]) == "on" {
			return errors.New("read only connection")
		}

		return nil
	})
}

func resetReadOnlySession(ctx context.Context, conn *pgx.Conn) error {
	err := withPgConnDeadline(conn.PgConn(), func() error {
		var readOnly string
		if err := conn.QueryRow(ctx, "show transaction_read_only").Scan(&readOnly); err != nil {
			return err
		}
		if readOnly == "on" {
			return driver.ErrBadConn
		}

		var defaultReadOnly string
		if err := conn.QueryRow(ctx, "show default_transaction_read_only").Scan(&defaultReadOnly); err != nil {
			return driver.ErrBadConn
		}
		if defaultReadOnly == "on" {
			return driver.ErrBadConn
		}

		return nil
	})
	if err != nil {
		return driver.ErrBadConn
	}
	return nil
}

func withPgConnDeadline(pgConn *pgconn.PgConn, fn func() error) error {
	netConn := pgConn.Conn()
	if err := netConn.SetDeadline(time.Now().Add(readWriteProbeTimeout)); err != nil {
		return err
	}

	err := fn()
	if err != nil {
		if isTimeoutError(err) {
			return err
		}
		_ = netConn.SetDeadline(time.Time{})
		return err
	}

	return netConn.SetDeadline(time.Time{})
}

func isTimeoutError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
