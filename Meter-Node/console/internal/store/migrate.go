package store

// The migration runner. `make migrate`, the controller's own startup path, and
// CI all go through here, so there is exactly one way the schema moves.

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/MeterHome/Meter-Node/console/migrations"
)

// Migrate applies all pending migrations.
func Migrate(ctx context.Context, databaseURL string, log *slog.Logger) error {
	db, err := openStdlib(databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(gooseLogger{log})
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("store: goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, db, migrations.Dir); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}

// MigrateStatus prints which migrations have been applied.
func MigrateStatus(ctx context.Context, databaseURL string, log *slog.Logger) error {
	db, err := openStdlib(databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(gooseLogger{log})
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("store: goose dialect: %w", err)
	}
	return goose.StatusContext(ctx, db, migrations.Dir)
}

// MigrateDown rolls back one migration. Deliberately one at a time: a
// down-migration on this schema drops hypertables, and "how many did you mean"
// is not a question worth guessing at.
func MigrateDown(ctx context.Context, databaseURL string, log *slog.Logger) error {
	db, err := openStdlib(databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(gooseLogger{log})
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("store: goose dialect: %w", err)
	}
	return goose.DownContext(ctx, db, migrations.Dir)
}

// openStdlib gives goose a database/sql handle.
//
// goose wants database/sql; everything else in the controller uses pgx
// natively. pgx's stdlib shim bridges the two so there is only one driver in
// the binary and one URL format to get wrong.
func openStdlib(databaseURL string) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("store: parse database url: %w", err)
	}
	return stdlib.OpenDB(*cfg), nil
}

// gooseLogger adapts goose's logger to slog so migration output lands in the
// same stream as everything else.
type gooseLogger struct{ log *slog.Logger }

func (g gooseLogger) Fatalf(format string, v ...any) {
	g.log.Error("migration", "message", fmt.Sprintf(format, v...))
}

func (g gooseLogger) Printf(format string, v ...any) {
	g.log.Info("migration", "message", fmt.Sprintf(format, v...))
}
