// Package store is the only package that knows about PostgreSQL.
//
// Everything else talks to the interfaces its own package defines
// (enroll.Repo, api.PrincipalStore, api.Health), and this package satisfies
// them. That is what lets the interesting decisions — what happens when a
// fingerprint changes, what a viewer may do — be unit tested without a
// database.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps a connection pool.
type Store struct {
	pool *pgxpool.Pool
}

// Options configure Connect.
type Options struct {
	URL            string
	MaxConns       int32
	MinConns       int32
	ConnectTimeout time.Duration
}

// Connect opens the pool and verifies it works before returning.
//
// A pool that connects lazily would let the controller start, pass its own
// liveness check, and only fail when the first node tries to enrol — which
// looks like an agent problem from the outside. Failing here instead makes a
// bad DATABASE_URL a startup error.
func Connect(ctx context.Context, o Options) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(o.URL)
	if err != nil {
		return nil, fmt.Errorf("store: parse database url: %w", err)
	}
	if o.MaxConns > 0 {
		cfg.MaxConns = o.MaxConns
	}
	if o.MinConns > 0 {
		cfg.MinConns = o.MinConns
	}
	// A connection that has been idle for half an hour behind a cloud load
	// balancer is usually already dead; recycling beats discovering that
	// mid-transaction.
	cfg.MaxConnIdleTime = 30 * time.Minute
	cfg.MaxConnLifetime = 2 * time.Hour
	cfg.HealthCheckPeriod = time.Minute

	timeout := o.ConnectTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(connectCtx, cfg)
	if err != nil {
		return nil, fmt.Errorf("store: open pool: %w", err)
	}
	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: ping database: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Pool exposes the underlying pool for packages that need raw access (the
// ingest gateway's batched COPY, in M2).
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Ping implements api.Health.
func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return errors.New("store: no database pool")
	}
	return s.pool.Ping(ctx)
}

// Close releases the pool.
func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

// inTx runs fn inside a transaction, rolling back on error or panic.
//
// The rollback is deferred rather than written on each error path because
// enrollment has a dozen of them, and one missed rollback leaks a connection
// and holds row locks until the pool recycles it.
func (s *Store) inTx(ctx context.Context, fn func(pgx.Tx) error) (err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			panic(p)
		}
		if err != nil {
			// context.WithoutCancel: if the request context is already dead
			// (the agent hung up), the rollback still has to reach the server.
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}
