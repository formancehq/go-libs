package bunconnect

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/formancehq/go-libs/v2/logging"
	"github.com/stretchr/testify/require"
)

func TestConnectionOptionsString(t *testing.T) {
	opts := ConnectionOptions{
		DatabaseSourceName: "postgres://user:pass@localhost:5432/db",
		MaxIdleConns:       10,
		MaxOpenConns:       20,
		ConnMaxIdleTime:    5 * time.Minute,
	}

	str := opts.String()
	require.Contains(t, str, "dsn=postgres://user:pass@localhost:5432/db", "La chaîne devrait contenir le DSN")
	require.Contains(t, str, "max-idle-conns=10", "La chaîne devrait contenir le nombre max de connexions inactives")
	require.Contains(t, str, "max-open-conns=20", "La chaîne devrait contenir le nombre max de connexions ouvertes")
	require.Contains(t, str, "conn-max-idle-time=5m0s", "La chaîne devrait contenir le temps max d'inactivité")
}

type mockConnector struct {
	dsn string
	err error
}

func (m *mockConnector) Connect(_ context.Context) (driver.Conn, error) {
	return nil, m.err
}

func (m *mockConnector) Driver() driver.Driver {
	return nil
}

func mockConnectorFunc(dsn string) (driver.Connector, error) {
	return &mockConnector{dsn: dsn}, nil
}

func mockConnectorFuncWithError(dsn string) (driver.Connector, error) {
	return nil, errors.New("erreur de connexion")
}

func TestOpenSQLDBWithConnector(t *testing.T) {
	t.Skip("Ce test nécessite une vraie base de données")

	ctx := logging.ContextWithLogger(context.Background(), logging.Testing())

	opts := ConnectionOptions{
		DatabaseSourceName: "postgres://user:pass@localhost:5432/db",
		MaxIdleConns:       10,
		MaxOpenConns:       20,
		ConnMaxIdleTime:    5 * time.Minute,
		Connector:          mockConnectorFunc,
	}

	_, err := OpenSQLDB(ctx, opts)
	require.Error(t, err, "OpenSQLDB devrait échouer sans une vraie base de données")
}

func TestOpenSQLDBWithConnectorError(t *testing.T) {
	ctx := logging.ContextWithLogger(context.Background(), logging.Testing())

	opts := ConnectionOptions{
		DatabaseSourceName: "postgres://user:pass@localhost:5432/db",
		Connector:          mockConnectorFuncWithError,
	}

	_, err := OpenSQLDB(ctx, opts)
	require.Error(t, err, "OpenSQLDB devrait échouer avec une erreur de connecteur")
	require.Contains(t, err.Error(), "erreur de connexion", "L'erreur devrait contenir le message d'erreur du connecteur")
}

func TestOpenDBWithSchema(t *testing.T) {
	t.Skip("Ce test nécessite une vraie base de données")

	ctx := logging.ContextWithLogger(context.Background(), logging.Testing())

	opts := ConnectionOptions{
		DatabaseSourceName: "postgres://user:pass@localhost:5432/db",
	}

	_, err := OpenDBWithSchema(ctx, opts, "test_schema")
	require.Error(t, err, "OpenDBWithSchema devrait échouer sans une vraie base de données")
}

func TestOpenDBWithSchemaInvalidURL(t *testing.T) {
	ctx := logging.ContextWithLogger(context.Background(), logging.Testing())

	opts := ConnectionOptions{
		DatabaseSourceName: "://invalid-url",
	}

	_, err := OpenDBWithSchema(ctx, opts, "test_schema")
	require.Error(t, err, "OpenDBWithSchema devrait échouer avec une URL invalide")
}

func TestOpenDBWithSchemaValidSchema(t *testing.T) {
	ctx := logging.ContextWithLogger(context.Background(), logging.Testing())

	opts := ConnectionOptions{
		DatabaseSourceName: "postgres://user:pass@localhost:5432/db",
	}

	_, err := OpenDBWithSchema(ctx, opts, "test_schema")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "parse", "L'erreur ne devrait pas être liée au parsing de l'URL")
}
