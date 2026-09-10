package config

import (
	"strings"
	"testing"
	"time"
)

func base() *Config {
	return &Config{
		Env: "prod", HTTPAddr: ":8080", PublicURL: "https://control.example.com",
		DatabaseURL: "postgres://x", DBMaxConns: 20, DBMinConns: 2,
		SigningKeyID: "ck-1", SigningKeySeed: "c2VlZA==",
		EnrollTokenTTL: 72 * time.Hour,
	}
}

// TestProdRefusesDevConveniences is the test that matters here. Every one of
// these would be a quiet production incident rather than a loud one.
func TestProdRefusesDevConveniences(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Config)
		want string
	}{
		{"generated signing key", func(c *Config) { c.SigningKeySeed = "" }, "SIGNING_KEY_SEED"},
		{"bootstrap admin token", func(c *Config) { c.BootstrapAdminToken = "letmein" }, "BOOTSTRAP_ADMIN_TOKEN"},
		{"plaintext public url", func(c *Config) { c.PublicURL = "http://control.example.com" }, "https"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := base()
			c.mut(cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("prod must reject %s: got %v", c.name, err)
			}
			// The same configuration is fine in dev.
			cfg.Env = "dev"
			cfg.PublicURL = "http://localhost:8080"
			if err := cfg.Validate(); err != nil {
				t.Fatalf("dev should allow it: %v", err)
			}
		})
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Config)
	}{
		{"unknown env", func(c *Config) { c.Env = "production" }},
		{"no database", func(c *Config) { c.DatabaseURL = "" }},
		{"no public url", func(c *Config) { c.PublicURL = "" }},
		{"zero token ttl", func(c *Config) { c.EnrollTokenTTL = 0 }},
		{"absurd token ttl", func(c *Config) { c.EnrollTokenTTL = 365 * 24 * time.Hour }},
		{"no connections", func(c *Config) { c.DBMaxConns = 0 }},
		{"min above max", func(c *Config) { c.DBMinConns = 50 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := base()
			c.mut(cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}

func TestAgentFacingURLs(t *testing.T) {
	cases := []struct{ public, stream, telemetry string }{
		{"https://control.example.com", "wss://control.example.com/v1/stream", "https://control.example.com/v1/telemetry"},
		{"https://control.example.com/", "wss://control.example.com/v1/stream", "https://control.example.com/v1/telemetry"},
		{"http://localhost:8080", "ws://localhost:8080/v1/stream", "http://localhost:8080/v1/telemetry"},
	}
	for _, c := range cases {
		cfg := &Config{PublicURL: c.public}
		if got := cfg.StreamURL(); got != c.stream {
			t.Errorf("StreamURL(%q) = %q, want %q", c.public, got, c.stream)
		}
		if got := cfg.TelemetryURL(); got != c.telemetry {
			t.Errorf("TelemetryURL(%q) = %q, want %q", c.public, got, c.telemetry)
		}
	}
}
