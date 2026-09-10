// Package config loads the agent's configuration.
//
// One YAML file at /etc/meternode/agent.yaml, environment variable overrides,
// and a working default for everything. The agent must start and do something
// useful with no configuration at all, because the first thing it does on a new
// machine is run before anyone has configured it.
//
// Reload on SIGHUP is supported and must not drop the controller connection —
// see Reloadable below for which fields may actually change at runtime.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	proto "github.com/meterhome/meternode-proto"
	"gopkg.in/yaml.v3"
)

// DefaultPath is where the packaged agent looks for its configuration.
const DefaultPath = "/etc/meternode/agent.yaml"

// Config is the agent's full configuration.
type Config struct {
	// Controller is where to talk to, and how to prove who we are.
	Controller Controller `yaml:"controller"`

	// Collect governs sampling. The defaults are the shared contract's, and
	// the agent will refuse values that would blow the bandwidth budget.
	Collect Collect `yaml:"collect"`

	// Bandwidth is the control-plane byte budget the agent degrades against.
	Bandwidth Bandwidth `yaml:"bandwidth"`

	// Paths are where state lives on disk.
	Paths Paths `yaml:"paths"`

	// Runtime is container supervision (M5). Present now so the config file
	// shape does not change under operators between releases.
	Runtime Runtime `yaml:"runtime"`

	Log Log `yaml:"log"`
}

// Controller is the control-plane connection.
type Controller struct {
	// URL is the controller base URL, e.g. https://control.example.com. The
	// stream and telemetry endpoints are derived from it unless the controller
	// overrides them at enrollment.
	URL string `yaml:"url"`

	// StreamURL and TelemetryURL are normally supplied by the controller at
	// enrollment and cached; setting them here overrides that, which is what a
	// staging environment or a captive-portal workaround needs.
	StreamURL    string `yaml:"stream_url"`
	TelemetryURL string `yaml:"telemetry_url"`

	// EnrollTokenFile is read once, then deleted. An env var override exists
	// for the imaging pipeline.
	EnrollTokenFile string `yaml:"enroll_token_file"`

	// InsecureSkipVerify disables TLS verification. It exists for a developer
	// with a self-signed controller and is refused unless the URL is
	// localhost, because a node in a stranger's house on an untrusted LAN is
	// the last place to be trusting an unverified certificate.
	InsecureSkipVerify bool `yaml:"insecure_skip_verify"`

	// ReconnectMin/Max bound the exponential backoff with full jitter. Never a
	// tight retry loop: a fleet that all lost their ISP at once must not
	// return as a thundering herd.
	ReconnectMin time.Duration `yaml:"reconnect_min"`
	ReconnectMax time.Duration `yaml:"reconnect_max"`
}

// Collect governs sampling and flushing.
type Collect struct {
	SampleInterval time.Duration `yaml:"sample_interval"`
	FlushInterval  time.Duration `yaml:"flush_interval"`
	Heartbeat      time.Duration `yaml:"heartbeat_interval"`

	// CollectorTimeout bounds any single collector.
	//
	// This is the field that keeps the agent alive on a node with a dying
	// disk. A hung SMART read or a wedged NVML call must not stall sampling:
	// the collector is abandoned, its failure is reported as an event, and the
	// rest of the sample still ships.
	CollectorTimeout time.Duration `yaml:"collector_timeout"`

	// DiskMounts limits disk collection. Empty means "every real filesystem",
	// which excludes tmpfs, overlay, squashfs and the rest of the noise a
	// container host accumulates.
	DiskMounts []string `yaml:"disk_mounts"`

	// NetInterface pins bandwidth accounting to one interface. Empty means the
	// interface carrying the default route, which is what a homeowner's ISP
	// meters.
	NetInterface string `yaml:"net_interface"`
}

// Bandwidth is the control-plane budget.
type Bandwidth struct {
	// MonthlyBudgetMB is what the agent aims to stay under. The contract's
	// target is 150 MB; the hard ceiling is 500 MB and is not configurable.
	MonthlyBudgetMB uint32 `yaml:"monthly_budget_mb"`

	// DegradeAt is the fraction of the budget at which the agent lengthens
	// intervals and emits net.cap_approaching. Well below 1.0 on purpose:
	// degrading at 99% makes the last day of every month a telemetry blackout.
	DegradeAt float64 `yaml:"degrade_at"`

	// BillingCycleDay is the day of month the homeowner's ISP resets their
	// cap. Residential caps reset on a billing date, not on the 1st.
	BillingCycleDay int `yaml:"billing_cycle_day"`
}

// Paths are on-disk locations.
type Paths struct {
	StateDir string `yaml:"state_dir"` // credentials, buffer, boot counter
	// SocketPath is the local diagnostic socket. A unix socket, always.
	// There is no configuration that opens a TCP port on a node.
	SocketPath string `yaml:"socket_path"`
}

// Runtime is container supervision. Phase 1 keeps the reconciliation loop
// running against an always-empty desired set.
type Runtime struct {
	DockerSocket    string `yaml:"docker_socket"`
	WorkloadNetwork string `yaml:"workload_network"`
	// EnforceIsolation makes the agent refuse to run workloads when it cannot
	// verify its isolation invariants. It defaults to true and there is no
	// supported way to run workloads with it off — the homeowner's LAN is the
	// thing being protected.
	EnforceIsolation bool `yaml:"enforce_isolation"`
}

// Log configures output.
type Log struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"` // json | text
}

// Default returns a configuration that works on a freshly imaged node with no
// file present at all.
func Default() Config {
	return Config{
		Controller: Controller{
			EnrollTokenFile: "/etc/meternode/enroll.token",
			ReconnectMin:    1 * time.Second,
			ReconnectMax:    300 * time.Second,
		},
		Collect: Collect{
			SampleInterval:   proto.DefaultSampleInterval,
			FlushInterval:    proto.DefaultFlushInterval,
			Heartbeat:        proto.DefaultHeartbeatInterval,
			CollectorTimeout: 3 * time.Second,
		},
		Bandwidth: Bandwidth{
			MonthlyBudgetMB: proto.TargetMonthlyBytes / (1024 * 1024),
			DegradeAt:       proto.DegradeAtFraction,
			BillingCycleDay: 1,
		},
		Paths: Paths{
			StateDir:   "/var/lib/meternode",
			SocketPath: "/run/meternode/agent.sock",
		},
		Runtime: Runtime{
			DockerSocket:     "/var/run/docker.sock",
			WorkloadNetwork:  "meternode-workloads",
			EnforceIsolation: true,
		},
		Log: Log{Level: "info", Format: "json"},
	}
}

// Load reads the configuration file, applies environment overrides, and
// validates the result.
//
// A missing file is not an error. A node that boots before anyone has written a
// config must still start, sample, and answer `meternodectl status` — that is
// how a field tech finds out what is wrong.
func Load(path string) (*Config, error) {
	cfg := Default()

	if path == "" {
		path = DefaultPath
	}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("config: parse %s: %w", path, err)
		}
	case errors.Is(err, os.ErrNotExist):
		// Defaults it is.
	default:
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	cfg.applyEnv()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// applyEnv overlays environment variables.
//
// The imaging pipeline sets these rather than templating a YAML file, and a
// field tech debugging a node sets one on the command line.
func (c *Config) applyEnv() {
	envStr("METERNODE_CONTROLLER_URL", &c.Controller.URL)
	envStr("METERNODE_STREAM_URL", &c.Controller.StreamURL)
	envStr("METERNODE_TELEMETRY_URL", &c.Controller.TelemetryURL)
	envStr("METERNODE_ENROLL_TOKEN_FILE", &c.Controller.EnrollTokenFile)
	envStr("METERNODE_STATE_DIR", &c.Paths.StateDir)
	envStr("METERNODE_SOCKET_PATH", &c.Paths.SocketPath)
	envStr("METERNODE_LOG_LEVEL", &c.Log.Level)
	envStr("METERNODE_LOG_FORMAT", &c.Log.Format)
	envStr("METERNODE_NET_INTERFACE", &c.Collect.NetInterface)
	envDuration("METERNODE_SAMPLE_INTERVAL", &c.Collect.SampleInterval)
	envDuration("METERNODE_FLUSH_INTERVAL", &c.Collect.FlushInterval)
	envDuration("METERNODE_HEARTBEAT_INTERVAL", &c.Collect.Heartbeat)
	envDuration("METERNODE_COLLECTOR_TIMEOUT", &c.Collect.CollectorTimeout)
	envUint32("METERNODE_MONTHLY_BUDGET_MB", &c.Bandwidth.MonthlyBudgetMB)
	envBool("METERNODE_INSECURE_SKIP_VERIFY", &c.Controller.InsecureSkipVerify)
}

// Validate rejects configurations that would break the bandwidth budget or the
// safety properties, rather than merely being unusual.
func (c *Config) Validate() error {
	var errs []error

	if c.Collect.SampleInterval < time.Second {
		errs = append(errs, errors.New("collect.sample_interval must be at least 1s"))
	}
	if c.Collect.FlushInterval < c.Collect.SampleInterval {
		errs = append(errs, errors.New("collect.flush_interval must be at least collect.sample_interval"))
	}
	if c.Collect.Heartbeat < 5*time.Second {
		// Faster than this and the heartbeat alone eats the monthly budget:
		// at 100 bytes and 5 s, that is ~52 MB/month before any telemetry.
		errs = append(errs, errors.New("collect.heartbeat_interval must be at least 5s (the bandwidth budget will not survive less)"))
	}
	if c.Collect.CollectorTimeout <= 0 || c.Collect.CollectorTimeout >= c.Collect.SampleInterval {
		// A collector timeout at or above the sample interval means a wedged
		// collector stalls the next sample, which is the exact failure the
		// timeout exists to prevent.
		errs = append(errs, errors.New("collect.collector_timeout must be positive and shorter than collect.sample_interval"))
	}

	if c.Bandwidth.MonthlyBudgetMB == 0 {
		errs = append(errs, errors.New("bandwidth.monthly_budget_mb must be positive"))
	}
	if hard := uint32(proto.CeilingMonthlyBytes / (1024 * 1024)); c.Bandwidth.MonthlyBudgetMB > hard {
		errs = append(errs, fmt.Errorf("bandwidth.monthly_budget_mb may not exceed the %d MB hard ceiling", hard))
	}
	if c.Bandwidth.DegradeAt <= 0 || c.Bandwidth.DegradeAt > 1 {
		errs = append(errs, errors.New("bandwidth.degrade_at must be between 0 and 1"))
	}
	if c.Bandwidth.BillingCycleDay < 1 || c.Bandwidth.BillingCycleDay > 28 {
		// 28, not 31: a cycle day of 30 does not exist in February, and a
		// budget that silently skips a month is worse than one that is a
		// couple of days off.
		errs = append(errs, errors.New("bandwidth.billing_cycle_day must be between 1 and 28"))
	}

	if c.Paths.StateDir == "" {
		errs = append(errs, errors.New("paths.state_dir is required"))
	}
	if c.Paths.SocketPath == "" {
		errs = append(errs, errors.New("paths.socket_path is required"))
	} else if !filepath.IsAbs(c.Paths.SocketPath) {
		errs = append(errs, errors.New("paths.socket_path must be absolute"))
	}

	if c.Controller.ReconnectMin <= 0 || c.Controller.ReconnectMax < c.Controller.ReconnectMin {
		errs = append(errs, errors.New("controller.reconnect_min must be positive and no greater than controller.reconnect_max"))
	}

	if c.Controller.InsecureSkipVerify && !isLocal(c.Controller.URL) {
		// A node sits on a stranger's LAN. Skipping certificate verification
		// there is not a developer convenience, it is an invitation.
		errs = append(errs, errors.New("controller.insecure_skip_verify is only permitted against a localhost controller"))
	}
	if c.Controller.URL != "" && !isLocal(c.Controller.URL) && !strings.HasPrefix(c.Controller.URL, "https://") {
		errs = append(errs, errors.New("controller.url must be https unless it is localhost"))
	}

	return errors.Join(errs...)
}

// Reloadable reports whether moving from c to next can be applied on SIGHUP
// without restarting.
//
// Intervals, log level, and the disk/interface selection are live. Anything
// that changes identity or the transport is not: reconnecting to apply a new
// controller URL would drop a socket that may have taken minutes of backoff to
// establish on a flaky residential link.
func (c *Config) Reloadable(next *Config) (bool, string) {
	switch {
	case c.Controller.URL != next.Controller.URL:
		return false, "controller.url"
	case c.Paths.StateDir != next.Paths.StateDir:
		return false, "paths.state_dir"
	case c.Paths.SocketPath != next.Paths.SocketPath:
		return false, "paths.socket_path"
	case c.Runtime.EnforceIsolation != next.Runtime.EnforceIsolation:
		return false, "runtime.enforce_isolation"
	default:
		return true, ""
	}
}

// CredentialPath is where the node's issued credential lives. 0600, owned by
// the agent user.
func (c *Config) CredentialPath() string {
	return filepath.Join(c.Paths.StateDir, "credential")
}

// NodeKeyPath is where the node's private key lives. It is generated locally
// and never transmitted.
func (c *Config) NodeKeyPath() string {
	return filepath.Join(c.Paths.StateDir, "node.key")
}

// BufferPath is the SQLite ring buffer that holds telemetry while offline.
func (c *Config) BufferPath() string {
	return filepath.Join(c.Paths.StateDir, "buffer.db")
}

// BootCounterPath tracks restarts, so a crash loop surfaces as a CRITICAL
// event rather than as silence.
func (c *Config) BootCounterPath() string {
	return filepath.Join(c.Paths.StateDir, "boot-counter")
}

func isLocal(url string) bool {
	return strings.Contains(url, "localhost") || strings.Contains(url, "127.0.0.1") || strings.Contains(url, "[::1]")
}

func envStr(key string, dst *string) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		*dst = v
	}
}

func envDuration(key string, dst *time.Duration) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			*dst = d
		}
	}
}

func envUint32(key string, dst *uint32) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			*dst = uint32(n)
		}
	}
}

func envBool(key string, dst *bool) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			*dst = b
		}
	}
}
