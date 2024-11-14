package migrations

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jackc/pgxlisten"

	"github.com/formancehq/go-libs/v2/logging"
	"github.com/formancehq/go-libs/v2/platform/postgres"
	"github.com/formancehq/go-libs/v2/time"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"

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
	Version      string     `json:"version"`
	Name         string     `json:"name"`
	State        string     `json:"state,omitempty"`
	Date         time.Time  `json:"date,omitempty"`
	TerminatedAt *time.Time `json:"terminatedAt,omitempty"`
	Progress     *int       `json:"progress,omitempty"`
}

type Migrator struct {
	migrations        []Migration
	schema            string
	tableName         string
	db                *bun.DB
	lockRetryInterval time.Duration
}

func (m *Migrator) GetSchema() string {
	if m.schema == "" {
		return "public"
	}
	return m.schema
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

func (m *Migrator) initSchema(ctx context.Context) error {
	_, err := m.db.Exec(`
		create schema if not exists "` + m.GetSchema() + `";

		set search_path = '` + m.GetSchema() + `';

		create table if not exists ` + m.tableName + ` (
			version_id bigint not null,
			is_applied boolean not null default false,
			tstamp timestamp not null default now(),
			id serial primary key
		);

		alter table ` + m.tableName + `
		add column if not exists max_counter numeric,
		add column if not exists actual_counter numeric,
		add column if not exists terminated_at timestamp;
	
		create unique index if not exists 
		idx_version_id on ` + m.tableName + ` (version_id);
	`)
	if err != nil {
		return postgres.ResolveError(err)
	}

	lastVersion, err := m.GetLastVersion(ctx)
	if err != nil {
		return fmt.Errorf("failed to get last version: %w", err)
	}

	if lastVersion == -1 {
		// Insert a first noop row to keep compatibility with goose
		_, err := m.db.NewInsert().
			Model(&Version{
				VersionID: 0,
				IsApplied: true,
				Timestamp: time.Now(),
			}).
			ModelTableExpr(m.getVersionsTable()).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to insert version: %w", postgres.ResolveError(err))
		}
	}

	return err
}

func (m *Migrator) GetLastVersion(ctx context.Context) (int, error) {
	version := &Version{}
	if err := m.db.NewSelect().
		Model(version).
		ModelTableExpr(m.getVersionsTable()).
		Order("version_id DESC").
		Limit(1).
		Where("is_applied").
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

func (m *Migrator) Up(ctx context.Context) error {
	for {
		err := m.UpByOne(ctx)
		if err != nil {
			if errors.Is(err, ErrAlreadyUpToDate) {
				return nil
			}
			return err
		}
	}
}

func (m *Migrator) GetMigrations(ctx context.Context) ([]Info, error) {
	ret := make([]Info, 0, len(m.migrations))
	versions := make([]Version, 0)

	if err := m.db.NewSelect().
		TableExpr(m.getVersionsTable()).
		Order("version_id").
		Where("version_id >= 1").
		Limit(len(m.migrations)).
		Scan(ctx, &versions); err != nil {
		return nil, postgres.ResolveError(err)
	}

	for i := 0; i < int(math.Min(float64(len(versions)), float64(len(m.migrations)))); i++ {
		var (
			state    string
			progress *int
		)
		if versions[i].IsApplied {
			state = "DONE"
		} else {
			state = "PROGRESS"
			if versions[i].MaxCounter > 0 {
				completion := versions[i].ActualCounter * 100 / versions[i].MaxCounter
				progress = &completion
			}
		}
		ret = append(ret, Info{
			Version: fmt.Sprint(versions[i].VersionID),
			Name:    m.migrations[i].Name,
			State:   state,
			Date:    versions[i].Timestamp,
			TerminatedAt: func() *time.Time {
				if versions[i].TerminatedAt.IsZero() {
					return nil
				}
				return &versions[i].TerminatedAt
			}(),
			Progress: progress,
		})
	}

	for i := len(versions); i < len(m.migrations); i++ {
		ret = append(ret, Info{
			Version: fmt.Sprint(i),
			Name:    m.migrations[i].Name,
			State:   "TO DO",
		})
	}

	return ret, nil
}

func (m *Migrator) IsUpToDate(ctx context.Context) (bool, error) {
	version, err := m.GetLastVersion(ctx)
	if err != nil {
		if errors.Is(err, ErrMissingVersionTable) {
			return false, nil
		}
		return false, err
	}

	return version == len(m.migrations), nil
}

func (m *Migrator) upByOne(ctx context.Context) error {

	// We need to lock something to prevent concurrent migration
	// We could have started a transaction and lock and full table,
	// but the downside is than the underlying migrations could not use "create index concurrently".
	// So, we will use advisory locks, at session level.
	// As advisory locks at session level need to be taken and released with the same underlying connection,
	// we grab a connection from the pool if we are not already in a transaction (a sql transaction already keep the same connection).
	conn, err := m.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", postgres.ResolveError(err))
	}
	defer func() {
		if err := conn.Close(); err != nil {
			logging.FromContext(ctx).Errorf("unable to close connection: %v", err)
		}
	}()

	// use pg_advisory_lock lead to creating new sql transaction (as any query)
	// so we need to use pg_advisory_try_lock to avoid this
	for {
		logging.FromContext(ctx).Debugf("Try to acquire lock on %s", m.getVersionsTable())
		var acquired bool
		err := conn.NewSelect().
			ColumnExpr("pg_try_advisory_lock(hashtext(?))", m.getVersionsTable()).
			Scan(ctx, &acquired)
		if err != nil {
			return fmt.Errorf("failed to acquire lock: %w", postgres.ResolveError(err))
		}
		if acquired {
			logging.FromContext(ctx).Debugf("Lock acquired on %s", m.getVersionsTable())
			break
		}

		logging.FromContext(ctx).Debugf("Lock not acquired on %s, retry in %s", m.getVersionsTable(), m.lockRetryInterval)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.lockRetryInterval):
		}
	}

	defer func() {
		logging.FromContext(ctx).Debugf("Unlock %s", m.getVersionsTable())
		_, err = conn.ExecContext(ctx, "select pg_advisory_unlock(hashtext(?))", m.getVersionsTable())
		if err != nil {
			if errors.Is(err, driver.ErrBadConn) {
				// If we have a driver.ErrBadConn, it means the connection is already closed and the advisory lock is released.
				// notes(gfyrag): I'm not 100% confident about this, but I think it's the best we can do.
				return
			}

			panic(err)
		}
	}()

	err = m.initSchema(ctx)
	if err != nil {
		return fmt.Errorf("failed to create version table: %w", err)
	}

	lastVersion, err := m.GetLastVersion(ctx)
	if err != nil {
		return fmt.Errorf("failed to get last version: %w", err)
	}
	logging.FromContext(ctx).Debugf("Detected last version: %d", lastVersion)

	// At this point, there is no pending migration occurring
	if len(m.migrations) == lastVersion {
		logging.FromContext(ctx).Debug("All migrations done!")
		// no more migration to play
		return ErrAlreadyUpToDate
	}

	_, err = conn.NewInsert().
		Model(&Version{
			VersionID: lastVersion + 1,
			IsApplied: false,
			Timestamp: time.Now(),
		}).
		ModelTableExpr(m.getVersionsTable()).
		On("conflict (version_id) do nothing").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to insert version: %w", postgres.ResolveError(err))
	}

	listeningContext, cancel := context.WithCancel(ctx)
	listenerStopped := make(chan struct{})
	defer func() {
		cancel()
		<-listenerStopped
	}()

	if err := conn.Raw(func(driverConn any) error {
		channel := "migrations-" + m.GetSchema()
		logging.FromContext(ctx).Debugf("Listening for migrations notifications on " + channel)

		listener := pgxlisten.Listener{
			Connect: func(ctx context.Context) (*pgx.Conn, error) {
				return pgx.Connect(ctx, driverConn.(*stdlib.Conn).Conn().Config().ConnString())
			},
			LogError: func(ctx context.Context, err error) {
				if !errors.Is(err, context.Canceled) {
					logging.FromContext(ctx).Errorf("pgxlisten error: %v", err)
				}
			},
		}
		listener.Handle(channel, pgxlisten.HandlerFunc(func(ctx context.Context, notification *pgconn.Notification, conn *pgx.Conn) error {

			logging.FromContext(ctx).Debugf("Received notification: %s", notification.Payload)

			switch {
			case strings.HasPrefix(notification.Payload, "init: "):
				value := strings.TrimPrefix(notification.Payload, "init: ")
				maxCounter, err := strconv.ParseInt(value, 10, 64)
				if err != nil {
					logging.FromContext(ctx).Errorf("failed to parse max counter: %v", err)
					return nil
				}
				_, err = m.db.NewUpdate().
					Model(&Version{}).
					ModelTableExpr(m.getVersionsTable()).
					Where("version_id = ?", lastVersion+1).
					Set("max_counter = ?", maxCounter).
					Where("max_counter is null").
					Exec(ctx)
				if err != nil {
					logging.FromContext(ctx).Errorf("failed to update max counter: %v", err)
					return nil
				}
			case strings.HasPrefix(notification.Payload, "continue: "):
				value := strings.TrimPrefix(notification.Payload, "continue: ")
				increment, err := strconv.ParseInt(value, 10, 64)
				if err != nil {
					logging.FromContext(ctx).Errorf("failed to parse actual counter: %v", err)
					return nil
				}
				_, err = m.db.NewUpdate().
					Model(&Version{}).
					ModelTableExpr(m.getVersionsTable()).
					Where("version_id = ?", lastVersion+1).
					Set("actual_counter = coalesce(actual_counter, 0) + ?", increment).
					Exec(ctx)
				if err != nil {
					logging.FromContext(ctx).Errorf("failed to update actual counter: %v", err)
					return nil
				}
			default:
				logging.FromContext(ctx).Errorf("unknown notification: %s", notification.Payload)
			}

			return nil
		}))
		go func() {
			if err := listener.Listen(listeningContext); err != nil {
				if errors.Is(err, context.Canceled) {
					close(listenerStopped)
					return
				}
				panic(err)
			}
		}()

		return nil
	}); err != nil {
		logging.FromContext(ctx).Errorf("Failed so setup migrations listener: %v", err)
	}

	logging.FromContext(ctx).Debugf("Running migration %d: %s", lastVersion, m.migrations[lastVersion].Name)
	if err := m.migrations[lastVersion].Up(ctx, m.db); err != nil {
		return fmt.Errorf("failed to run migration '%s': %w", m.migrations[lastVersion].Name, err)
	}

	logging.FromContext(ctx).Debugf("Migration %d done", lastVersion)
	_, err = m.db.NewUpdate().
		Model(&Version{}).
		Where("version_id = ? and not is_applied", lastVersion+1).
		Set("is_applied = true").
		Set("terminated_at = ?", time.Now()).
		ModelTableExpr(m.getVersionsTable()).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to insert new version: %w", postgres.ResolveError(err))
	}

	return nil
}

func (m *Migrator) UpByOne(ctx context.Context) error {
	return m.upByOne(ctx)
}

func NewMigrator(db *bun.DB, opts ...Option) *Migrator {
	ret := &Migrator{
		db:        db,
		tableName: migrationTable,
	}
	for _, opt := range append(defaultOptions, opts...) {
		opt(ret)
	}
	if ret.lockRetryInterval == 0 {
		ret.lockRetryInterval = defaultLockRetryInterval
	}
	return ret
}

type Option func(m *Migrator)

func WithSchema(schema string) Option {
	return func(m *Migrator) {
		m.schema = schema
	}
}

func WithTableName(name string) Option {
	return func(m *Migrator) {
		m.tableName = name
	}
}

func WithLockRetryInterval(interval time.Duration) Option {
	return func(m *Migrator) {
		m.lockRetryInterval = interval
	}
}

var defaultOptions = []Option{
	WithLockRetryInterval(defaultLockRetryInterval),
}

var defaultLockRetryInterval = time.Second
