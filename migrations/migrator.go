package migrations

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/formancehq/go-libs/v2/logging"
	"github.com/formancehq/go-libs/v2/platform/postgres"
	"github.com/formancehq/go-libs/v2/time"

	"github.com/uptrace/bun"
)

const (
	// Keep goose name to keep backward compatibility
	migrationTable = "goose_db_version"
)

var (
	ErrMissingVersionTable = errors.New("missing version table")
	ErrAlreadyUpToDate     = errors.New("already up to date")
)

type Info struct {
	Version string    `json:"version" bun:"version_id"`
	Name    string    `json:"name" bun:"-"`
	State   string    `json:"state,omitempty" bun:"-"`
	Date    time.Time `json:"date,omitempty" bun:"tstamp"`
}

type Migrator struct {
	migrations   []Migration
	schema       string
	createSchema bool
	tableName    string
}

func (m *Migrator) RegisterMigrations(migrations ...Migration) *Migrator {
	m.migrations = append(m.migrations, migrations...)
	return m
}

func (m *Migrator) getVersionsTable() string {
	if m.schema != "" {
		return fmt.Sprintf(`"%s"."%s"`, m.schema, m.tableName)
	}
	return fmt.Sprintf(`"%s"`, m.tableName)
}

func (m *Migrator) createVersionTableIfNeeded(ctx context.Context, db bun.IDB) error {
	_, err := db.NewCreateTable().
		Model(&VersionTable{}).
		ModelTableExpr(m.getVersionsTable()).
		IfNotExists().
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to create version table: %w", postgres.ResolveError(err))
	}

	lastVersion, err := m.GetLastVersion(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to get last version: %w", err)
	}

	if lastVersion == -1 {
		if err := m.insertVersion(ctx, db, 0); err != nil {
			return fmt.Errorf("failed to insert version: %w", err)
		}
	}

	return err
}

func (m *Migrator) GetLastVersion(ctx context.Context, db bun.IDB) (int, error) {
	version := &VersionTable{}
	if err := db.NewSelect().
		Model(version).
		ModelTableExpr(m.getVersionsTable()).
		Order("version_id DESC").
		Limit(1).
		ColumnExpr("*").
		Scan(ctx); err != nil {
		err = postgres.ResolveError(err)
		switch {
		case errors.Is(err, postgres.ErrMissingTable):
			return -1, ErrMissingVersionTable
		case errors.Is(err, postgres.ErrNotFound):
			return -1, nil
		default:
			return -1, err
		}
	}

	return version.VersionID, nil
}

func (m *Migrator) insertVersion(ctx context.Context, db bun.IDB, version int) error {
	_, err := db.NewInsert().
		Model(&VersionTable{
			VersionID: version,
			IsApplied: true,
			Timestamp: time.Now(),
		}).
		ModelTableExpr(m.getVersionsTable()).
		Exec(ctx)
	return err
}

func (m *Migrator) Up(ctx context.Context, db bun.IDB) error {
	for {
		err := m.UpByOne(ctx, db)
		if err != nil {
			if errors.Is(err, ErrAlreadyUpToDate) {
				return nil
			}
			return err
		}
	}
}

func (m *Migrator) GetMigrations(ctx context.Context, db bun.IDB) ([]Info, error) {
	ret := make([]Info, len(m.migrations))

	if err := db.NewSelect().
		TableExpr(m.getVersionsTable()).
		Order("version_id").
		Where("version_id >= 1").
		Column("version_id", "tstamp").
		Limit(len(m.migrations)).
		Scan(ctx, &ret); err != nil {
		return nil, err
	}

	for i := 0; i < int(math.Min(float64(len(ret)), float64(len(m.migrations)))); i++ {
		ret[i].Name = m.migrations[i].Name
		ret[i].State = "DONE"
	}

	for i := len(ret); i < len(m.migrations); i++ {
		ret = append(ret, Info{
			Version: fmt.Sprint(i),
			Name:    m.migrations[i].Name,
			State:   "TO DO",
		})
	}

	return ret, nil
}

func (m *Migrator) IsUpToDate(ctx context.Context, db bun.IDB) (bool, error) {
	version, err := m.GetLastVersion(ctx, db)
	if err != nil {
		if errors.Is(err, ErrMissingVersionTable) {
			return false, nil
		}
		return false, err
	}

	return version == len(m.migrations), nil
}

func (m *Migrator) createSchemaIfNeeded(ctx context.Context, db bun.IDB) error {
	if m.schema != "" && m.createSchema {
		_, err := db.ExecContext(ctx, fmt.Sprintf(`create schema if not exists "%s"`, m.schema))
		if err != nil {
			return fmt.Errorf("failed to create schema: %w", err)
		}
	}

	return nil
}

func (m *Migrator) upByOne(ctx context.Context, db bun.IDB) error {

	err := m.createSchemaIfNeeded(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	err = m.createVersionTableIfNeeded(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to create version table: %w", err)
	}

	// We need to lock something to prevent concurrent migration
	// We could have started a transaction and lock and full table,
	// but the downside is than the underlying migrations could not use "create index concurrently".
	// So, we will use advisory locks, at session level.
	// As advisory locks at session level need to be taken and released with the same underlying connection,
	// we grab a connection from the pool if we are not already in a transaction (a sql transaction already keep the same connection).
	conn := db
	switch idb := db.(type) {
	case *bun.DB:
		newConn, err := idb.Conn(ctx)
		if err != nil {
			return fmt.Errorf("failed to get connection: %w", err)
		}
		defer func() {
			if err := newConn.Close(); err != nil {
				logging.FromContext(ctx).Errorf("unable to close connection: %v", err)
			}
		}()
		conn = newConn
	}

	_, err = conn.ExecContext(ctx, "select pg_advisory_lock(hashtext(?))", m.getVersionsTable())
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}

	defer func() {
		_, err = conn.ExecContext(ctx, "select pg_advisory_unlock(hashtext(?))", m.getVersionsTable())
		if err != nil {
			panic(err)
		}
	}()

	lastVersion, err := m.GetLastVersion(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to get last version: %w", err)
	}

	// At this point, there is no pending migration occurring
	if len(m.migrations) == lastVersion {
		// no more migration to play
		return ErrAlreadyUpToDate
	}

	// notes(gfyrag): run migration using db provided and not the tx we just created.
	// we need this tx to be able to lock the migrations table,
	// but we want migrations has the control and start a transaction if they need to.
	logging.FromContext(ctx).Debugf("Running migration %d: %s", lastVersion, m.migrations[lastVersion].Name)
	if err := m.migrations[lastVersion].Up(ctx, db); err != nil {
		return fmt.Errorf("failed to run migration '%s': %w", m.migrations[lastVersion].Name, err)
	}

	newVersion := lastVersion + 1
	if err := m.insertVersion(ctx, db, newVersion); err != nil {
		return fmt.Errorf("failed to insert new version: %w", err)
	}

	return nil
}

func (m *Migrator) UpByOne(ctx context.Context, db bun.IDB) error {
	return postgres.ResolveError(m.upByOne(ctx, db))
}

func NewMigrator(opts ...Option) *Migrator {
	ret := &Migrator{
		tableName: migrationTable,
	}
	for _, opt := range opts {
		opt(ret)
	}
	return ret
}

type Option func(m *Migrator)

func WithSchema(schema string, create bool) Option {
	return func(m *Migrator) {
		m.schema = schema
		m.createSchema = create
	}
}

func WithTableName(name string) Option {
	return func(m *Migrator) {
		m.tableName = name
	}
}
