package pgtesting

import (
	"context"
	"fmt"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	pgx "github.com/jackc/pgx/v5"
	"github.com/ory/dockertest/v3"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

type pgDatabase struct {
	url string
}

func (s *pgDatabase) ConnString() string {
	return s.url
}

type pgServer struct {
	destroy func() error
	lock    sync.Mutex
	conn    *pgx.Conn
	port    string
}

func (s *pgServer) dsn(databaseName string) string {
	return fmt.Sprintf("postgresql://root:root@localhost:%s/%s?sslmode=disable", s.port, databaseName)
}

func (s *pgServer) NewDatabase(t *testing.T) *pgDatabase {
	s.lock.Lock()
	defer s.lock.Unlock()

	databaseName := uuid.NewString()
	_, err := s.conn.Exec(context.Background(), fmt.Sprintf(`CREATE DATABASE "%s"`, databaseName))
	require.NoError(t, err)

	return &pgDatabase{
		url: s.dsn(databaseName),
	}
}

func (s *pgServer) Close() {
	if s.conn == nil {
		return
	}
	if err := s.conn.Close(context.Background()); err != nil {
		log.Fatal("error closing connection: ", err)
	}
	if err := s.destroy(); err != nil {
		log.Fatal("error destroying pg server: ", err)
	}
}

var srv *pgServer

func NewPostgresDatabase(t *testing.T) *pgDatabase {
	return srv.NewDatabase(t)
}

func DestroyPostgresServer() {
	srv.Close()
}

func CreatePostgresServer() error {

	pool, err := dockertest.NewPool("")
	if err != nil {
		return errors.Wrap(err, "unable to start docker containers pool")
	}

	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "15-alpine",
		Env: []string{
			"POSTGRES_USER=root",
			"POSTGRES_PASSWORD=root",
			"POSTGRES_DB=formance",
		},
		Entrypoint: nil,
		Cmd:        []string{"-c", "superuser-reserved-connections=0"},
	})
	if err != nil {
		return errors.Wrap(err, "unable to start postgres server container")
	}

	srv = &pgServer{
		port: resource.GetPort("5432/tcp"),
		destroy: func() error {
			return pool.Purge(resource)
		},
	}

	try := time.Duration(0)
	delay := 200 * time.Millisecond
	for try*delay < 5*time.Second {
		srv.conn, err = pgx.Connect(context.Background(), srv.dsn("formance"))
		if err != nil {
			try++
			<-time.After(delay)
			continue
		}
		return nil
	}

	return errors.New("timeout waiting for server ready")
}
