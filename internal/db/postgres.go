package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rapidbuildapp/rapidbuild/config"
)

type PostgresClient struct {
	Pool *pgxpool.Pool
}

func NewPostgresClient(cfg *config.Config) (*PostgresClient, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Parse the database URL to configure connection pool
	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse database URL: %w", err)
	}

	// Disable prepared statements to avoid issues with connection pooling
	// This is recommended for serverless databases like Neon
	poolConfig.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	// Configure pool settings for better concurrency
	poolConfig.MaxConns = 20
	poolConfig.MinConns = 2
	poolConfig.MaxConnLifetime = 1 * time.Hour
	poolConfig.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("unable to create connection pool: %w", err)
	}

	// Test the connection
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("unable to ping database: %w", err)
	}

	return &PostgresClient{Pool: pool}, nil
}

func (c *PostgresClient) Close() {
	c.Pool.Close()
}

// QueryRow executes a query that is expected to return at most one row
func (c *PostgresClient) QueryRow(ctx context.Context, query string, args ...interface{}) Row {
	return c.Pool.QueryRow(ctx, query, args...)
}

// Query executes a query that returns rows
func (c *PostgresClient) Query(ctx context.Context, query string, args ...interface{}) (Rows, error) {
	return c.Pool.Query(ctx, query, args...)
}

// Exec executes a query that doesn't return rows
func (c *PostgresClient) Exec(ctx context.Context, query string, args ...interface{}) (int64, error) {
	result, err := c.Pool.Exec(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// Begin starts a new transaction
func (c *PostgresClient) Begin(ctx context.Context) (Tx, error) {
	tx, err := c.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &pgxTxWrapper{tx: tx}, nil
}

// Interfaces for compatibility
type Row interface {
	Scan(dest ...interface{}) error
}

type Rows interface {
	Next() bool
	Scan(dest ...interface{}) error
	Close()
	Err() error
}

type Tx interface {
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
	QueryRow(ctx context.Context, query string, args ...interface{}) Row
	Query(ctx context.Context, query string, args ...interface{}) (Rows, error)
	Exec(ctx context.Context, query string, args ...interface{}) (int64, error)
}

// pgxTxWrapper wraps pgx.Tx to match our Tx interface
type pgxTxWrapper struct {
	tx pgx.Tx
}

func (w *pgxTxWrapper) Commit(ctx context.Context) error {
	return w.tx.Commit(ctx)
}

func (w *pgxTxWrapper) Rollback(ctx context.Context) error {
	return w.tx.Rollback(ctx)
}

func (w *pgxTxWrapper) QueryRow(ctx context.Context, query string, args ...interface{}) Row {
	return w.tx.QueryRow(ctx, query, args...)
}

func (w *pgxTxWrapper) Query(ctx context.Context, query string, args ...interface{}) (Rows, error) {
	return w.tx.Query(ctx, query, args...)
}

func (w *pgxTxWrapper) Exec(ctx context.Context, query string, args ...interface{}) (int64, error) {
	result, err := w.tx.Exec(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}
