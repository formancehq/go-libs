package clickhousetesting

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ClickHouse/ch-go/proto"
	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/ory/dockertest/v3"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	"github.com/formancehq/go-libs/v3/testing/docker"
)

type TestingT interface {
	require.TestingT
	Cleanup(func())
}

type Server struct {
	Port string
}

func (s *Server) GetHost() string {
	return "127.0.0.1"
}

func (s *Server) GetDSN() string {
	return s.GetDatabaseDSN("")
}

func (s *Server) GetDatabaseDSN(databaseName string) string {
	return fmt.Sprintf("clickhouse://default:password@%s:%s/%s", s.GetHost(), s.Port, databaseName)
}

func (s *Server) NewDatabase(t TestingT) *Database {

	options, err := clickhouse.ParseDSN(s.GetDSN())
	require.NoError(t, err)

	db, err := clickhouse.Open(options)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	databaseName := uuid.NewString()
	err = db.Exec(context.Background(), fmt.Sprintf(`CREATE DATABASE "%s"`, databaseName))
	require.NoError(t, err)

	if os.Getenv("NO_CLEANUP") != "true" {
		t.Cleanup(func() {
			const maxTry = 10
			nbTry := 0
		l:
			for {
				err = db.Exec(context.Background(), fmt.Sprintf(`DROP DATABASE "%s"`, databaseName))
				if exception, ok := err.(*clickhouse.Exception); nbTry < maxTry && ok {
					// Due to async operation, the driver can respond with a database not empty error while we are not writing
					// So retry a few times before leverage an error
					if exception.Code == int32(proto.ErrDatabaseNotEmpty) {
						<-time.After(100 * time.Millisecond)
						nbTry++
						continue l
					}
				}
				require.NoError(t, err)
				break
			}

		})
	}

	return &Database{
		url: s.GetDatabaseDSN(databaseName),
	}
}

func CreateServer(pool *docker.Pool, opts ...ServerOption) *Server {

	serverOptions := ServerOptions{}
	for _, opt := range append(defaultOptions, opts...) {
		opt(&serverOptions)
	}

	resource := pool.Run(docker.Configuration{
		RunOptions: &dockertest.RunOptions{
			Repository: "clickhouse/clickhouse-server",
			Tag:        serverOptions.Version,
			Env:        []string{"CLICKHOUSE_PASSWORD=password"},
		},
		CheckFn: func(ctx context.Context, resource *dockertest.Resource) error {
			dsn := fmt.Sprintf("clickhouse://default:password@127.0.0.1:%s", resource.GetPort("9000/tcp"))
			options, _ := clickhouse.ParseDSN(dsn)

			db, err := clickhouse.Open(options)
			if err != nil {
				return errors.Wrap(err, "opening database")
			}
			defer func() {
				_ = db.Close()
			}()

			if err := db.Ping(context.Background()); err != nil {
				return errors.Wrap(err, "pinging database")
			}

			return nil
		},
	})

	return &Server{
		Port: resource.GetPort("9000/tcp"),
	}
}

type Database struct {
	url string
}

func (d *Database) ConnString() string {
	return d.url
}

type ServerOptions struct {
	Version string
}

type ServerOption func(options *ServerOptions)

func WithVersion(version string) ServerOption {
	return func(options *ServerOptions) {
		options.Version = version
	}
}

var defaultOptions = []ServerOption{
	WithVersion("25.2-alpine"),
}
