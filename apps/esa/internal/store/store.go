// Package store is the PostgreSQL persistence layer, built on GORM. Only the
// fleet and auth packages use it; HTTP handlers never touch it directly.
//
// The schema is defined by the structs in models.go and applied at startup
// with GORM AutoMigrate (see Migrate).
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ErrNotFound is returned when a looked-up row does not exist.
var ErrNotFound = errors.New("store: not found")

// Queries runs queries against the database or inside a transaction.
type Queries struct {
	db *gorm.DB
}

// Store owns the connection pool.
type Store struct {
	*Queries
	pool  *pgxpool.Pool
	sqlDB *sql.DB
}

// Open connects to PostgreSQL. databaseURL is a libpq URL or keyword/value
// string, for example postgres://esa@localhost:5432/esa?sslmode=disable.
func Open(ctx context.Context, databaseURL string, log *slog.Logger) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	cfg.ConnConfig.RuntimeParams["application_name"] = "esa"
	// Always work in UTC so timestamps round-trip without surprises.
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		c := cfg.ConnConfig
		return nil, fmt.Errorf("connect to database %s@%s:%d/%s: %w", c.User, c.Host, c.Port, c.Database, err)
	}

	sqlDB := stdlib.OpenDBFromPool(pool)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		// Unique-key and foreign-key violations come back as gorm.ErrDuplicatedKey
		// and gorm.ErrForeignKeyViolated instead of driver-specific errors.
		TranslateError: true,
		NowFunc:        func() time.Time { return time.Now().UTC().Truncate(time.Microsecond) },
		Logger: logger.NewSlogLogger(log, logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			// Log SQL with placeholders only. Arguments can be session hashes or secrets.
			ParameterizedQueries: true,
		}),
	})
	if err != nil {
		_ = sqlDB.Close()
		pool.Close()
		return nil, fmt.Errorf("open gorm: %w", err)
	}

	return &Store{Queries: &Queries{db: db}, pool: pool, sqlDB: sqlDB}, nil
}

// Close closes every connection.
func (s *Store) Close() {
	_ = s.sqlDB.Close()
	s.pool.Close()
}

// Ping checks the database is reachable.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// InTx runs fn in a transaction. The transaction commits if fn returns nil
// and rolls back otherwise, including on panic.
func (s *Store) InTx(ctx context.Context, fn func(q *Queries) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&Queries{db: tx})
	})
}

// with returns a session bound to ctx.
func (q *Queries) with(ctx context.Context) *gorm.DB { return q.db.WithContext(ctx) }

// notFound maps gorm.ErrRecordNotFound to ErrNotFound.
func notFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}
