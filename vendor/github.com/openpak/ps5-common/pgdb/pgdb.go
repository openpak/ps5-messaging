// Package pgdb opens the small Postgres pool the PS5 services keep player state in.
package pgdb

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultMaxConns is the pool size: the box allows 300 connections in total.
const DefaultMaxConns = 3

// ErrNoDatabase is returned when a service that must persist has no database URL.
var ErrNoDatabase = errors.New("PS5_DATABASE_URL is required (set PS5_DEV_MEMORY=1 only for local development)")

// Open connects to url with a small pool and pings it. An empty url is ErrNoDatabase.
func Open(ctx context.Context, url string, maxConns int32) (*pgxpool.Pool, error) {
	if url == "" {
		return nil, ErrNoDatabase
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("PS5_DATABASE_URL: %w", err)
	}
	if maxConns <= 0 {
		maxConns = DefaultMaxConns
	}
	cfg.MaxConns = maxConns
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: %w", err)
	}
	return pool, nil
}

// Migrate runs idempotent DDL (CREATE TABLE IF NOT EXISTS ...) in one transaction. A transaction
// advisory lock keeps two instances starting together from racing.
func Migrate(ctx context.Context, pool *pgxpool.Pool, ddl string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(5005005)`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, ddl); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
