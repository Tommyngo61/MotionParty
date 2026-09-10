// Command meternode-agent is the host-side supervisor for a MeterNode node.
//
// It runs under systemd as an unprivileged user on a machine in a homeowner's
// residence. Nothing about it listens on a TCP port; its only local interface
// is a unix socket for a field tech (see internal/localapi).
//
// Usage:
//
//	meternode-agent [-config /etc/meternode/agent.yaml]
//	meternode-agent -version
//	meternode-agent -check-config
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/meterhome/meternode-agent/internal/agent"
	"github.com/meterhome/meternode-agent/internal/config"
	"github.com/meterhome/meternode-agent/internal/localapi"
	"github.com/meterhome/meternode-agent/internal/logging"
	"github.com/meterhome/meternode-agent/internal/supervise"
	"github.com/meterhome/meternode-agent/internal/version"
)

func main() {
	var (
		configPath  = flag.String("config", config.DefaultPath, "path to agent.yaml")
		showVersion = flag.Bool("version", false, "print the build identity and exit")
		checkConfig = flag.Bool("check-config", false, "validate the configuration and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("meternode-agent", version.String())
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "meternode-agent:", err)
		os.Exit(2)
	}
	if *checkConfig {
		// Used by the .deb postinst and by a field tech before restarting: a
		// bad config should fail here, loudly, rather than at 3am in a restart
		// loop on a machine nobody can reach.
		fmt.Printf("configuration at %s is valid\n", *configPath)
		return
	}

	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "meternode-agent:", err)
		os.Exit(1)
	}
}

func run(cfg *config.Config) error {
	log := logging.New(cfg.Log.Level, cfg.Log.Format)
	notifier := supervise.NewNotifier()

	// The state directory holds the credential and the node's private key, so
	// 0750 rather than 0755: nothing else on the box has any business
	// enumerating it.
	if err := os.MkdirAll(cfg.Paths.StateDir, 0o750); err != nil {
		return fmt.Errorf("create state directory %s: %w", cfg.Paths.StateDir, err)
	}

	boot, err := supervise.LoadBootCounter(cfg.BootCounterPath())
	if err != nil {
		// Losing the crash history is much less bad than refusing to start.
		log.Warn("could not persist the boot counter", "error", err)
	}

	a := agent.New(agent.Options{Config: cfg, Logger: log, Boot: boot})

	enrolled, nodeID, siteID := readIdentity(cfg)
	if !enrolled {
		log.Warn("node is not enrolled; collecting locally only",
			"token_file", cfg.Controller.EnrollTokenFile,
			"hint", "place a one-time enrollment token at the path above")
	}

	local := localapi.New(cfg.Paths.SocketPath, log,
		func() localapi.Status { return a.Status(enrolled, nodeID, siteID) },
		func() any { return a.LastSample() })
	if err := local.Start(); err != nil {
		// The diagnostic socket is how a field tech at the house finds out
		// what is wrong. Failing to start it is fatal: an agent that runs but
		// cannot be interrogated is worse than one that does not start, since
		// the second at least shows up in `systemctl status`.
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// SIGHUP reloads the configuration without dropping anything.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	go handleReloads(ctx, hup, a, cfg, log)

	go notifier.RunWatchdog(ctx, a.Healthy)
	_ = notifier.Ready()
	_ = notifier.Status(statusLine(enrolled, nodeID))
	go reportStatus(ctx, notifier, a, enrolled, nodeID)

	log.Info("meternode-agent running",
		"version", version.String(), "enrolled", enrolled, "node_id", nodeID,
		"socket", cfg.Paths.SocketPath, "state_dir", cfg.Paths.StateDir,
		"watchdog", notifier.WatchdogInterval())

	// A panic in the supervisor is recovered, recorded, and re-raised so
	// systemd restarts us — but the crash is queued for the next connection
	// first, so a crash-looping node is not a silent one.
	runErr := runRecovered(ctx, a, boot, log)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = notifier.Stopping()
	if err := local.Stop(shutdownCtx); err != nil {
		log.Warn("local api did not stop cleanly", "error", err)
	}
	if err := boot.MarkCleanShutdown(); err != nil {
		log.Warn("could not record a clean shutdown", "error", err)
	}
	log.Info("stopped cleanly")
	return runErr
}

// runRecovered runs the agent, converting a panic into a recorded crash.
func runRecovered(ctx context.Context, a *agent.Agent, boot *supervise.BootCounter, log interface {
	Error(msg string, args ...any)
}) (err error) {
	defer func() {
		if p := recover(); p != nil {
			log.Error("agent panicked", "error", p, "stack", string(debug.Stack()))
			// CleanShutdown stays false, so the next start counts this as a
			// crash and the crash-loop threshold can be reached.
			err = fmt.Errorf("agent panicked: %v", p)
		}
	}()
	_ = boot
	return a.Run(ctx)
}

func handleReloads(ctx context.Context, hup <-chan os.Signal, a *agent.Agent, current *config.Config, log interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}) {
	path := config.DefaultPath
	if len(os.Args) > 0 {
		// Honour -config on reload too, so a node started with a non-default
		// path does not silently reload the wrong file.
		for i, arg := range os.Args {
			if arg == "-config" || arg == "--config" {
				if i+1 < len(os.Args) {
					path = os.Args[i+1]
				}
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-hup:
			next, err := config.Load(path)
			if err != nil {
				// Keep running on the old configuration. A typo in a config
				// file must never take a node off the network.
				log.Error("SIGHUP: new configuration is invalid, keeping the current one", "error", err)
				continue
			}
			if ok, field := current.Reloadable(next); !ok {
				log.Warn("SIGHUP: this change needs a restart and was not applied",
					"field", field, "hint", "systemctl restart meternode-agent")
				continue
			}
			a.Reload(next)
			current = next
		}
	}
}

// reportStatus keeps `systemctl status` showing something useful.
func reportStatus(ctx context.Context, n *supervise.Notifier, a *agent.Agent, enrolled bool, nodeID string) {
	if !n.Enabled() {
		return
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			st := a.Status(enrolled, nodeID, "")
			line := statusLine(enrolled, nodeID)
			if len(st.FailingCollectors) > 0 {
				line += fmt.Sprintf("; %d collector(s) failing", len(st.FailingCollectors))
			}
			line += fmt.Sprintf("; %d samples, %d buffered", st.SamplesTaken, st.Buffered)
			_ = n.Status(line)
		}
	}
}

func statusLine(enrolled bool, nodeID string) string {
	if !enrolled {
		return "not enrolled — waiting for an enrollment token"
	}
	return "enrolled as " + nodeID
}

// storedIdentity is what enrollment writes alongside the credential (M3).
type storedIdentity struct {
	NodeID string `json:"node_id"`
	SiteID string `json:"site_id"`
}

// readIdentity reports whether this node has already enrolled.
//
// Enrollment itself lands in M3; this reads what it will write, so the local
// status output is honest today about a node that has not enrolled — which is
// the single most useful thing a field tech can be told.
func readIdentity(cfg *config.Config) (bool, string, string) {
	credential, err := os.ReadFile(cfg.CredentialPath())
	if err != nil || len(credential) == 0 {
		return false, "", ""
	}
	key, err := os.ReadFile(cfg.NodeKeyPath())
	if err != nil || len(key) != ed25519.PrivateKeySize {
		// A credential with no matching private key is unusable: the
		// per-request signature cannot be produced. Report unenrolled so the
		// tech is told to re-enrol rather than left staring at a node that
		// claims an identity it cannot prove.
		return false, "", ""
	}

	var id storedIdentity
	if data, err := os.ReadFile(filepath.Join(cfg.Paths.StateDir, "identity.json")); err == nil {
		_ = json.Unmarshal(data, &id)
	}
	return true, id.NodeID, id.SiteID
}
