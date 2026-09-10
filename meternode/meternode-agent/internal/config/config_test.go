package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMissingConfigFileIsNotAnError: a node that boots before anyone has
// written a config must still start, sample, and answer `meternodectl status` —
// that is how a field tech finds out what is wrong.
func TestMissingConfigFileIsNotAnError(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("a missing config file must not stop the agent: %v", err)
	}
	if cfg.Collect.SampleInterval != 10*time.Second || cfg.Collect.Heartbeat != 15*time.Second {
		t.Errorf("defaults must be the contract's budget, got %+v", cfg.Collect)
	}
	if cfg.Paths.SocketPath == "" || cfg.Paths.StateDir == "" {
		t.Error("defaults must include usable paths")
	}
}

func TestLoadAppliesFileThenEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(path, []byte(`
controller:
  url: https://control.example.com
collect:
  sample_interval: 20s
  flush_interval: 120s
paths:
  state_dir: /var/lib/test
  socket_path: /run/test.sock
log:
  level: debug
`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Collect.SampleInterval != 20*time.Second || cfg.Log.Level != "debug" {
		t.Fatalf("file values not applied: %+v", cfg)
	}
	// Defaults survive for fields the file did not set.
	if cfg.Collect.Heartbeat != 15*time.Second {
		t.Errorf("unset fields must keep their default, got %s", cfg.Collect.Heartbeat)
	}

	// Environment wins over the file: the imaging pipeline sets these rather
	// than templating YAML.
	t.Setenv("METERNODE_SAMPLE_INTERVAL", "30s")
	t.Setenv("METERNODE_LOG_LEVEL", "warn")
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Collect.SampleInterval != 30*time.Second || cfg.Log.Level != "warn" {
		t.Fatalf("environment must override the file: %+v", cfg)
	}
}

func TestValidateProtectsTheBandwidthBudget(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Config)
		want string
	}{
		{"heartbeat too fast", func(c *Config) { c.Collect.Heartbeat = time.Second }, "heartbeat"},
		{"budget above the hard ceiling", func(c *Config) { c.Bandwidth.MonthlyBudgetMB = 5000 }, "ceiling"},
		{"zero budget", func(c *Config) { c.Bandwidth.MonthlyBudgetMB = 0 }, "monthly_budget_mb"},
		{"degrade at zero", func(c *Config) { c.Bandwidth.DegradeAt = 0 }, "degrade_at"},
		{"degrade above one", func(c *Config) { c.Bandwidth.DegradeAt = 1.5 }, "degrade_at"},
		{"billing day 31", func(c *Config) { c.Bandwidth.BillingCycleDay = 31 }, "billing_cycle_day"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := Default()
			c.mut(&cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want an error mentioning %q", err, c.want)
			}
		})
	}
}

// TestCollectorTimeoutMustBeShorterThanTheInterval: a timeout at or above the
// sample interval means a wedged collector stalls the next sample, which is the
// exact failure the timeout exists to prevent.
func TestCollectorTimeoutMustBeShorterThanTheInterval(t *testing.T) {
	cfg := Default()
	cfg.Collect.CollectorTimeout = cfg.Collect.SampleInterval
	if err := cfg.Validate(); err == nil {
		t.Fatal("a collector timeout equal to the sample interval must be refused")
	}
	cfg.Collect.CollectorTimeout = cfg.Collect.SampleInterval - time.Second
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a shorter timeout should be fine: %v", err)
	}
}

// TestTLSVerificationCannotBeDisabledAgainstARealController. A node sits on a
// stranger's LAN. Skipping certificate verification there is not a developer
// convenience.
func TestTLSVerificationCannotBeDisabledAgainstARealController(t *testing.T) {
	cfg := Default()
	cfg.Controller.URL = "https://control.example.com"
	cfg.Controller.InsecureSkipVerify = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("insecure_skip_verify must be refused against a non-local controller")
	}

	cfg.Controller.URL = "http://localhost:8080"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("localhost should be allowed for development: %v", err)
	}
}

func TestControllerMustBeHTTPS(t *testing.T) {
	cfg := Default()
	cfg.Controller.URL = "http://control.example.com"
	if err := cfg.Validate(); err == nil {
		t.Fatal("a plaintext controller URL must be refused")
	}
}

// TestReloadableSeparatesLiveFieldsFromRestartOnes. Reconnecting to apply a
// changed controller URL would drop a socket that may have taken minutes of
// backoff to establish on a flaky residential link.
func TestReloadableSeparatesLiveFieldsFromRestartOnes(t *testing.T) {
	base := Default()

	live := base
	live.Collect.SampleInterval = 30 * time.Second
	live.Collect.CollectorTimeout = 5 * time.Second
	live.Log.Level = "debug"
	live.Collect.NetInterface = "eth1"
	if ok, field := base.Reloadable(&live); !ok {
		t.Errorf("intervals and logging should reload live, blocked on %q", field)
	}

	for name, mut := range map[string]func(*Config){
		"controller.url":            func(c *Config) { c.Controller.URL = "https://other.example.com" },
		"paths.state_dir":           func(c *Config) { c.Paths.StateDir = "/srv/other" },
		"paths.socket_path":         func(c *Config) { c.Paths.SocketPath = "/run/other.sock" },
		"runtime.enforce_isolation": func(c *Config) { c.Runtime.EnforceIsolation = false },
	} {
		t.Run(name, func(t *testing.T) {
			next := base
			mut(&next)
			ok, field := base.Reloadable(&next)
			if ok {
				t.Fatalf("%s must require a restart", name)
			}
			if field != name {
				t.Errorf("blocked field = %q, want %q", field, name)
			}
		})
	}
}

// TestIsolationDefaultsOn: the homeowner's LAN is the thing being protected,
// and there must be no way to end up with enforcement off by accident.
func TestIsolationDefaultsOn(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Runtime.EnforceIsolation {
		t.Fatal("runtime.enforce_isolation must default to true")
	}
}

func TestStatePathsAreUnderTheStateDir(t *testing.T) {
	cfg := Default()
	cfg.Paths.StateDir = "/var/lib/meternode"
	for name, got := range map[string]string{
		"credential":   cfg.CredentialPath(),
		"node key":     cfg.NodeKeyPath(),
		"buffer":       cfg.BufferPath(),
		"boot counter": cfg.BootCounterPath(),
	} {
		if !strings.HasPrefix(got, "/var/lib/meternode/") {
			t.Errorf("%s path %q is not under the state dir", name, got)
		}
	}
}

func TestSocketPathMustBeAbsolute(t *testing.T) {
	cfg := Default()
	cfg.Paths.SocketPath = "agent.sock"
	if err := cfg.Validate(); err == nil {
		t.Fatal("a relative socket path must be refused")
	}
}
