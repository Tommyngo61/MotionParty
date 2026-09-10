// Package config loads controller configuration from the environment.
//
// Environment only, no config file. The controller runs as a single binary in a
// container; a file would be one more thing to mount, template, and get out of
// sync between the compose profile and production. Every value has a default
// that works for local development, and every value that must not have a
// default in production is checked by Validate.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the controller's full configuration.
type Config struct {
	// Env is "dev", "staging", or "prod". It gates the developer conveniences
	// — notably the auto-generated signing key and the bootstrap admin token —
	// which must never appear outside dev.
	Env string

	HTTPAddr        string
	ShutdownTimeout time.Duration

	// PublicURL is the externally reachable base URL agents connect to. It is
	// what gets handed to a node at enrollment as its stream and telemetry
	// endpoints, so it has to be the real hostname, not the bind address.
	PublicURL string

	DatabaseURL      string
	DBMaxConns       int32
	DBMinConns       int32
	DBConnectTimeout time.Duration

	RedisURL string

	// SigningKeyID and SigningKeySeed are the controller's command-signing
	// identity. The seed is a base64 ed25519 seed supplied by the environment
	// or a KMS. In dev it may be empty, and a throwaway key is generated on
	// boot and persisted to the database so restarts do not invalidate agents
	// that pinned it.
	SigningKeyID   string
	SigningKeySeed string

	// EnrollTokenTTL bounds how long a minted enrollment token stays usable.
	// Short on purpose: the token is the one credential that exists before a
	// node has an identity, and it is often typed by a field tech from a
	// printout that then goes in a van.
	EnrollTokenTTL time.Duration

	// CredentialTTL is how long an issued node credential is valid. Zero means
	// no expiry, which is the phase-1 default: a node that cannot renew
	// because the homeowner's internet was out for a week must not lock itself
	// out of the fleet.
	CredentialTTL time.Duration

	// BootstrapAdminToken seeds a single admin API token on first boot so a
	// fresh dev stack is usable without a manual SQL insert. Refused outside
	// dev by Validate.
	BootstrapAdminToken string

	LogLevel  string
	LogFormat string // "json" or "text"
}

// Load reads configuration from the environment and applies defaults.
func Load() (*Config, error) {
	c := &Config{
		Env:                 env("METERNODE_ENV", "dev"),
		HTTPAddr:            env("METERNODE_HTTP_ADDR", ":8080"),
		ShutdownTimeout:     envDuration("METERNODE_SHUTDOWN_TIMEOUT", 20*time.Second),
		PublicURL:           env("METERNODE_PUBLIC_URL", "http://localhost:8080"),
		DatabaseURL:         env("METERNODE_DATABASE_URL", "postgres://meternode:meternode@localhost:5432/meternode?sslmode=disable"),
		DBMaxConns:          int32(envInt("METERNODE_DB_MAX_CONNS", 20)),
		DBMinConns:          int32(envInt("METERNODE_DB_MIN_CONNS", 2)),
		DBConnectTimeout:    envDuration("METERNODE_DB_CONNECT_TIMEOUT", 10*time.Second),
		RedisURL:            env("METERNODE_REDIS_URL", "redis://localhost:6379/0"),
		SigningKeyID:        env("METERNODE_SIGNING_KEY_ID", "ck-dev-1"),
		SigningKeySeed:      env("METERNODE_SIGNING_KEY_SEED", ""),
		EnrollTokenTTL:      envDuration("METERNODE_ENROLL_TOKEN_TTL", 72*time.Hour),
		CredentialTTL:       envDuration("METERNODE_CREDENTIAL_TTL", 0),
		BootstrapAdminToken: env("METERNODE_BOOTSTRAP_ADMIN_TOKEN", ""),
		LogLevel:            env("METERNODE_LOG_LEVEL", "info"),
		LogFormat:           env("METERNODE_LOG_FORMAT", "json"),
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// IsDev reports whether developer conveniences are permitted.
func (c *Config) IsDev() bool { return c.Env == "dev" }

// Validate rejects configurations that are unsafe rather than merely unusual.
func (c *Config) Validate() error {
	var errs []error

	switch c.Env {
	case "dev", "staging", "prod":
	default:
		errs = append(errs, fmt.Errorf("METERNODE_ENV must be dev, staging, or prod (got %q)", c.Env))
	}

	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("METERNODE_DATABASE_URL is required"))
	}
	if c.PublicURL == "" {
		errs = append(errs, errors.New("METERNODE_PUBLIC_URL is required"))
	}

	if !c.IsDev() {
		// Outside dev the controller must be handed its signing key. Generating
		// one silently would mean a restart mints a new identity that every
		// enrolled agent rejects, and the failure would look like a fleet-wide
		// command outage rather than a config mistake.
		if c.SigningKeySeed == "" {
			errs = append(errs, errors.New("METERNODE_SIGNING_KEY_SEED is required outside dev"))
		}
		if c.BootstrapAdminToken != "" {
			errs = append(errs, errors.New("METERNODE_BOOTSTRAP_ADMIN_TOKEN is a dev-only convenience and must not be set outside dev"))
		}
		if !strings.HasPrefix(c.PublicURL, "https://") {
			errs = append(errs, errors.New("METERNODE_PUBLIC_URL must be https outside dev"))
		}
	}

	if c.EnrollTokenTTL <= 0 || c.EnrollTokenTTL > 30*24*time.Hour {
		errs = append(errs, errors.New("METERNODE_ENROLL_TOKEN_TTL must be positive and at most 30 days"))
	}
	if c.DBMaxConns < 1 {
		errs = append(errs, errors.New("METERNODE_DB_MAX_CONNS must be at least 1"))
	}
	if c.DBMinConns < 0 || c.DBMinConns > c.DBMaxConns {
		errs = append(errs, errors.New("METERNODE_DB_MIN_CONNS must be between 0 and METERNODE_DB_MAX_CONNS"))
	}

	return errors.Join(errs...)
}

// StreamURL is the wss:// (or ws:// in dev) endpoint handed to agents.
func (c *Config) StreamURL() string {
	base := strings.TrimSuffix(c.PublicURL, "/")
	switch {
	case strings.HasPrefix(base, "https://"):
		return "wss://" + strings.TrimPrefix(base, "https://") + "/v1/stream"
	case strings.HasPrefix(base, "http://"):
		return "ws://" + strings.TrimPrefix(base, "http://") + "/v1/stream"
	default:
		return base + "/v1/stream"
	}
}

// TelemetryURL is the HTTP batch fallback endpoint handed to agents.
func (c *Config) TelemetryURL() string {
	return strings.TrimSuffix(c.PublicURL, "/") + "/v1/telemetry"
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
