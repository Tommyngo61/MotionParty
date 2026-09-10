// Command meternodectl is the local diagnostic CLI.
//
// It is for a field tech standing at the house with a laptop on the node's
// console, asking "why isn't this thing working". It talks to the running agent
// over a unix socket — there is no network interface to this, on purpose, and
// no configuration that would add one.
//
// Usage:
//
//	meternodectl status          # human-readable summary
//	meternodectl status --json   # the same thing, machine-readable
//	meternodectl sample          # the most recent raw telemetry sample
//	meternodectl version
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/meterhome/meternode-agent/internal/config"
	"github.com/meterhome/meternode-agent/internal/localapi"
	"github.com/meterhome/meternode-agent/internal/version"
)

func main() {
	var (
		socket  = flag.String("socket", "", "path to the agent socket (default: from agent.yaml)")
		asJSON  = flag.Bool("json", false, "emit JSON")
		timeout = flag.Duration("timeout", 10*time.Second, "request timeout")
	)
	flag.Usage = usage
	flag.Parse()

	cmd := "status"
	if flag.NArg() > 0 {
		cmd = flag.Arg(0)
	}
	if cmd == "version" {
		fmt.Println("meternodectl", version.String())
		return
	}

	path := *socket
	if path == "" {
		// Fall back to the config file so a node with a non-default socket
		// path still works without the tech knowing where it is.
		if cfg, err := config.Load(config.DefaultPath); err == nil {
			path = cfg.Paths.SocketPath
		} else {
			path = config.Default().Paths.SocketPath
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	client := localapi.NewClient(path)

	var err error
	switch cmd {
	case "status":
		err = runStatus(ctx, client, *asJSON)
	case "sample":
		err = runSample(ctx, client)
	default:
		fmt.Fprintf(os.Stderr, "meternodectl: unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `meternodectl — local diagnostics for a MeterNode agent

Usage:
  meternodectl [flags] [command]

Commands:
  status    show what the agent is doing and what is wrong (default)
  sample    print the most recent raw telemetry sample
  version   print the build identity

Flags:
  -socket path   agent socket (default: paths.socket_path from agent.yaml)
  -json          machine-readable output
  -timeout d     request timeout (default 10s)

If this reports permission denied, run it with sudo or add yourself to the
agent's group.
`)
}

func runStatus(ctx context.Context, c *localapi.Client, asJSON bool) error {
	st, err := c.Status(ctx)
	if err != nil {
		return err
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(st)
	}
	render(st)
	return nil
}

func runSample(ctx context.Context, c *localapi.Client) error {
	raw, err := c.Sample(ctx)
	if err != nil {
		return err
	}
	if len(raw) == 0 || string(raw) == "null\n" {
		fmt.Println("No sample yet — the agent has just started.")
		return nil
	}
	fmt.Print(string(raw))
	return nil
}

// render prints the status for a human.
//
// Warnings come FIRST, before the identity block. A tech at the house has a
// question — "what is wrong with this box" — and making them read past twenty
// lines of healthy fields to find the answer is the difference between a
// five-minute visit and a twenty-minute one.
func render(s *localapi.Status) {
	if len(s.Warnings) > 0 {
		fmt.Println("PROBLEMS")
		for _, w := range s.Warnings {
			fmt.Printf("  ! %s\n", wrap(w, 4))
		}
		fmt.Println()
	} else {
		fmt.Println("No problems reported.")
		fmt.Println()
	}

	section("AGENT")
	field("version", s.AgentVersion)
	field("uptime", humanDuration(s.UptimeS))
	field("pid", fmt.Sprint(s.PID))
	field("restarts", fmt.Sprintf("%d boots, %d crashes", s.BootCount, s.CrashCount))

	section("IDENTITY")
	if s.Enrolled {
		field("enrolled", "yes")
		field("node id", s.NodeID)
		if s.SiteID != "" {
			field("site id", s.SiteID)
		}
	} else {
		field("enrolled", "NO")
		if s.EnrollHint != "" {
			field("token file", s.EnrollHint)
		}
	}
	if s.ControllerURL != "" {
		field("controller", s.ControllerURL)
	}

	section("CONNECTION")
	if s.Connected {
		field("connected", "yes ("+s.Transport+")")
		field("last upload", orDash(s.LastUploadAt))
	} else {
		field("connected", "no")
		if s.ReconnectIn != "" {
			field("retrying in", s.ReconnectIn)
		}
	}
	if s.ClockSkewMS != 0 {
		field("clock skew", fmt.Sprintf("%d ms", s.ClockSkewMS))
	}

	section("COLLECTION")
	field("collectors", strings.Join(s.Collectors, ", "))
	if len(s.FailingCollectors) > 0 {
		field("failing", strings.Join(s.FailingCollectors, ", "))
	}
	field("gpu source", orDash(s.GPUSource))
	field("interval", fmt.Sprintf("%ds", s.SampleIntervalS))
	field("samples", fmt.Sprintf("%d taken, last took %d ms", s.SamplesTaken, s.LastSampleMS))
	field("last sample", orDash(s.LastSampleAt))

	section("BUFFER")
	field("queued", fmt.Sprintf("%d of %d samples", s.Buffered, s.BufferCapacity))
	if s.BufferDropped > 0 {
		field("dropped", fmt.Sprintf("%d samples", s.BufferDropped))
	}

	section("BANDWIDTH")
	field("control plane", fmt.Sprintf("%.1f MB month-to-date", float64(s.ControlPlaneBytesMTD)/(1024*1024)))
	field("budget", fmt.Sprintf("%d MB/month (%s)", s.BudgetMB, s.BudgetStatus))
	fmt.Println()
}

func section(name string) { fmt.Printf("%s\n", name) }

func field(k, v string) { fmt.Printf("  %-14s %s\n", k, v) }

func orDash(v string) string {
	if v == "" {
		return "—"
	}
	return v
}

func humanDuration(seconds int64) string {
	d := time.Duration(seconds) * time.Second
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", seconds)
	case d < time.Hour:
		return fmt.Sprintf("%dm", seconds/60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh %dm", seconds/3600, (seconds%3600)/60)
	default:
		return fmt.Sprintf("%dd %dh", seconds/86400, (seconds%86400)/3600)
	}
}

// wrap re-indents a long warning so it stays readable on an 80-column console,
// which is what a tech has on a node with no desktop.
func wrap(s string, indent int) string {
	const width = 74
	if len(s) <= width {
		return s
	}
	var out strings.Builder
	line := 0
	for i, word := range strings.Fields(s) {
		if line > 0 && line+len(word)+1 > width {
			out.WriteString("\n" + strings.Repeat(" ", indent))
			line = 0
		} else if i > 0 {
			out.WriteString(" ")
			line++
		}
		out.WriteString(word)
		line += len(word)
	}
	return out.String()
}
