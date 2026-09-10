// Command controller is the MeterNode Console control plane.
//
// One binary, no runtime dependencies beyond PostgreSQL/TimescaleDB and Redis.
// It migrates its own schema, serves the agent-facing endpoints and the
// operator API, and shuts down without dropping agent sockets mid-frame.
//
// Usage:
//
//	controller                 # migrate, then serve
//	controller migrate         # apply migrations and exit
//	controller migrate-status  # show which migrations have run
//	controller migrate-down    # roll back one migration
//	controller version
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	proto "github.com/MeterHome/Meter-Node/proto"

	"github.com/MeterHome/Meter-Node/console/internal/api"
	"github.com/MeterHome/Meter-Node/console/internal/config"
	"github.com/MeterHome/Meter-Node/console/internal/enroll"
	"github.com/MeterHome/Meter-Node/console/internal/logging"
	"github.com/MeterHome/Meter-Node/console/internal/store"
)

// version is set at build time: -ldflags "-X main.version=$(git describe --tags)".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "controller:", err)
		os.Exit(1)
	}
}

func run() error {
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	if cmd == "version" {
		fmt.Printf("meternode-controller %s (schema %s)\n", version, proto.SchemaSemVer)
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	log := logging.New(cfg.LogLevel, cfg.LogFormat)

	// SIGTERM is what an orchestrator sends. Handling it is what makes a
	// rolling deploy a graceful drain rather than several thousand agents
	// reconnecting at once.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch cmd {
	case "migrate":
		return store.Migrate(ctx, cfg.DatabaseURL, log)
	case "migrate-status":
		return store.MigrateStatus(ctx, cfg.DatabaseURL, log)
	case "migrate-down":
		return store.MigrateDown(ctx, cfg.DatabaseURL, log)
	case "", "serve":
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}

	return serve(ctx, cfg, log)
}

func serve(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	log.Info("starting controller",
		"version", version, "env", cfg.Env, "addr", cfg.HTTPAddr,
		"public_url", cfg.PublicURL, "schema", proto.SchemaSemVer)

	// Migrate before opening the pool the server will use. A controller that
	// starts against an un-migrated database fails on the first enrollment
	// instead of at boot, which reads as an agent problem from the outside.
	if err := store.Migrate(ctx, cfg.DatabaseURL, log); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	db, err := store.Connect(ctx, store.Options{
		URL: cfg.DatabaseURL, MaxConns: cfg.DBMaxConns,
		MinConns: cfg.DBMinConns, ConnectTimeout: cfg.DBConnectTimeout,
	})
	if err != nil {
		return err
	}
	defer db.Close()

	signer, err := store.LoadOrCreateSigningKey(ctx, db, cfg.SigningKeyID, cfg.SigningKeySeed, cfg.IsDev(), log)
	if err != nil {
		return fmt.Errorf("signing key: %w", err)
	}
	if err := store.EnsureBootstrapAdmin(ctx, db, cfg.BootstrapAdminToken, cfg.IsDev(), log); err != nil {
		return err
	}

	hints := proto.DefaultConfigHints()
	hints.StreamURL, hints.TelemetryURL = cfg.StreamURL(), cfg.TelemetryURL()

	srv := api.New(api.Options{
		Config: cfg,
		Logger: log,
		Enroll: enroll.New(store.NewEnrollRepo(db), signer, enroll.Options{
			TokenTTL:      cfg.EnrollTokenTTL,
			CredentialTTL: cfg.CredentialTTL,
			Hints:         hints,
		}),
		Principals: store.NewPrincipalStore(db),
		Health:     db,
		Version:    version,
	})

	httpSrv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: srv.Routes(),
		// Read timeouts are generous because the peers are residential links
		// with 300 ms latency and 5 Mbps upstream, not datacenter neighbours.
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       60 * time.Second,
		// No WriteTimeout: it would cap the lifetime of the long-lived
		// telemetry WebSocket that lands on this server in M2. Per-handler
		// timeouts do the bounding instead.
		IdleTimeout: 120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		log.Info("shutdown signal received, draining", "timeout", cfg.ShutdownTimeout)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	log.Info("stopped cleanly")
	return nil
}
