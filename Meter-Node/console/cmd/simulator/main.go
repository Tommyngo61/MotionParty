// Command simulator spawns fake MeterNode agents against a controller.
//
// Most of this system's failure modes only appear under bad network
// conditions, and none of them appear on a developer's laptop with one agent
// on loopback. The simulator is a first-class deliverable for that reason: it
// is how enrollment races, reconnect storms, schema skew, and the 10k-socket
// ingest target get exercised before a real node in a real house does it for
// us.
//
// Milestone coverage:
//   - M1 (now): concurrent enrollment against the real /v1/enroll endpoint,
//     with injected latency and failure, credential verification, and latency
//     percentiles.
//   - M2: WebSocket telemetry, packet loss, bandwidth caps, random disconnects,
//     reconnect-with-backfill. The agent struct below is shaped for it.
//
// Usage:
//
//	simulator -controller http://localhost:8080 -admin-token dev -n 200
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"

	proto "github.com/MeterHome/Meter-Node/proto"
)

type options struct {
	controller  string
	adminToken  string
	nodes       int
	concurrency int
	latency     time.Duration
	jitter      time.Duration
	failPct     int
	seed        int64
	timeout     time.Duration
}

func main() {
	var o options
	flag.StringVar(&o.controller, "controller", envOr("METERNODE_CONTROLLER_URL", "http://localhost:8080"), "controller base URL")
	flag.StringVar(&o.adminToken, "admin-token", os.Getenv("METERNODE_BOOTSTRAP_ADMIN_TOKEN"), "operator token used to mint enrollment tokens")
	flag.IntVar(&o.nodes, "n", 10, "number of simulated nodes")
	flag.IntVar(&o.concurrency, "concurrency", 32, "how many nodes enrol at once")
	flag.DurationVar(&o.latency, "latency", 0, "simulated one-way link latency added to every request")
	flag.DurationVar(&o.jitter, "jitter", 0, "random jitter added to the simulated latency")
	flag.IntVar(&o.failPct, "fail-pct", 0, "percentage of requests to abandon before sending, simulating a dropped link")
	flag.Int64Var(&o.seed, "seed", 1, "random seed, so a failing run reproduces")
	flag.DurationVar(&o.timeout, "timeout", 30*time.Second, "per-request timeout")
	flag.Parse()

	if o.adminToken == "" {
		fmt.Fprintln(os.Stderr, "simulator: -admin-token is required (it mints the enrollment tokens)")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, o); err != nil {
		fmt.Fprintln(os.Stderr, "simulator:", err)
		os.Exit(1)
	}
}

// agent is one simulated node. It holds exactly what a real agent holds after
// enrollment, so the M2 telemetry loop can be hung off it unchanged.
type agent struct {
	index          int
	nodeID         string
	priv           ed25519.PrivateKey
	pub            ed25519.PublicKey
	credential     string
	config         proto.AgentConfigHints
	controllerKeys map[string][]byte

	enrollLatency time.Duration
	err           error
}

func run(ctx context.Context, o options) error {
	fmt.Printf("simulator: %d nodes against %s (latency %s ±%s, %d%% link failure, seed %d)\n",
		o.nodes, o.controller, o.latency, o.jitter, o.failPct, o.seed)

	client := &http.Client{Timeout: o.timeout}
	rng := rand.New(rand.NewSource(o.seed))

	// Mint the tokens up front and serially. Minting is an operator action, not
	// something a node does, and doing it inline would measure the wrong thing.
	start := time.Now()
	tokens := make([]string, o.nodes)
	for i := range tokens {
		tok, err := mintToken(ctx, client, o, fmt.Sprintf("simulated node %d", i))
		if err != nil {
			return fmt.Errorf("mint token %d: %w", i, err)
		}
		tokens[i] = tok
	}
	fmt.Printf("simulator: minted %d enrollment tokens in %s\n", o.nodes, time.Since(start).Round(time.Millisecond))

	agents := make([]*agent, o.nodes)
	sem := make(chan struct{}, o.concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex

	start = time.Now()
	for i := 0; i < o.nodes; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			mu.Lock()
			delay := simulatedDelay(rng, o)
			drop := o.failPct > 0 && rng.Intn(100) < o.failPct
			mu.Unlock()

			a := &agent{index: i}
			if drop {
				a.err = errors.New("link dropped before the request was sent")
				agents[i] = a
				return
			}
			if delay > 0 {
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					a.err = ctx.Err()
					agents[i] = a
					return
				}
			}
			a.err = a.enroll(ctx, client, o, tokens[i])
			agents[i] = a
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	return report(agents, elapsed)
}

func (a *agent) enroll(ctx context.Context, client *http.Client, o options, token string) error {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return err
	}
	a.pub, a.priv = pub, priv

	req := proto.EnrollRequest{
		Token:   token,
		NodePub: pub,
		Fingerprint: proto.HardwareFingerprint{
			// Deterministic per index so a re-run enrols the SAME simulated
			// machines. That is what makes re-enrollment and re-attestation
			// reproducible instead of a new fleet every time.
			MotherboardUUID: fmt.Sprintf("4c4c4544-0037-5a10-8054-%012x", a.index),
			GPUUUIDs:        []string{fmt.Sprintf("GPU-%08x-1111-2222-3333-444455556666", a.index)},
			PrimaryNICMAC:   fmt.Sprintf("a4:bb:6d:%02x:%02x:%02x", a.index>>16&0xff, a.index>>8&0xff, a.index&0xff),
		},
		Hardware: proto.HardwareInventory{
			Hostname: fmt.Sprintf("mn-sim-%04d", a.index), CPUModel: "AMD Ryzen 9 7950X",
			CPUCores: 16, CPUThreads: 32, MemTotalMB: 65536,
			Kernel: "6.8.0-45-generic", OS: "Ubuntu 24.04.1 LTS",
			GPUs: []proto.GPUInventory{{
				Index: 0, UUID: fmt.Sprintf("GPU-%08x-1111-2222-3333-444455556666", a.index),
				Name: "NVIDIA RTX PRO 6000 Blackwell", MemTotalMB: 98304, DriverVersion: "565.57.01",
			}},
		},
		AgentVersion: "sim-1.0.0",
		SentAtMS:     time.Now().UnixMilli(),
	}

	start := time.Now()
	var resp proto.EnrollResponse
	if err := postJSON(ctx, client, o.controller+proto.EnrollPath, "", req, &resp); err != nil {
		return err
	}
	a.enrollLatency = time.Since(start)

	// Verify the credential exactly as a real agent does before storing it. A
	// controller that hands out credentials its own published keys do not
	// verify is a bug the simulator should catch, not the fleet.
	cred, sig, err := proto.ParseCredential(resp.Credential)
	if err != nil {
		return fmt.Errorf("parse credential: %w", err)
	}
	pinned, ok := resp.ControllerKeys[cred.KeyID]
	if !ok {
		return fmt.Errorf("credential signed by key %q, which the controller did not publish", cred.KeyID)
	}
	if err := cred.Verify(ed25519.PublicKey(pinned), sig, time.Now()); err != nil {
		return fmt.Errorf("credential does not verify: %w", err)
	}
	if cred.NodeID != resp.NodeID {
		return fmt.Errorf("credential is for node %s but the response says %s", cred.NodeID, resp.NodeID)
	}

	a.nodeID, a.credential = resp.NodeID, resp.Credential
	a.config, a.controllerKeys = resp.Config, resp.ControllerKeys
	return nil
}

func mintToken(ctx context.Context, client *http.Client, o options, label string) (string, error) {
	var out struct {
		Token string `json:"token"`
	}
	body := map[string]any{"label": label}
	if err := postJSON(ctx, client, o.controller+"/v1/admin/enrollment-tokens", o.adminToken, body, &out); err != nil {
		return "", err
	}
	return out.Token, nil
}

func postJSON(ctx context.Context, client *http.Client, url, bearer string, in, out any) error {
	buf, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		var apiErr proto.APIError
		if json.NewDecoder(resp.Body).Decode(&apiErr) == nil && apiErr.Code != "" {
			return fmt.Errorf("%s: %s (%s)", resp.Status, apiErr.Message, apiErr.Code)
		}
		return fmt.Errorf("%s", resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func simulatedDelay(rng *rand.Rand, o options) time.Duration {
	if o.latency <= 0 && o.jitter <= 0 {
		return 0
	}
	d := o.latency
	if o.jitter > 0 {
		d += time.Duration(rng.Int63n(int64(o.jitter)))
	}
	return d
}

func report(agents []*agent, elapsed time.Duration) error {
	var ok int
	var latencies []time.Duration
	failures := map[string]int{}

	for _, a := range agents {
		if a == nil {
			failures["never ran"]++
			continue
		}
		if a.err != nil {
			failures[a.err.Error()]++
			continue
		}
		ok++
		latencies = append(latencies, a.enrollLatency)
	}

	fmt.Printf("\nsimulator: %d/%d enrolled in %s\n", ok, len(agents), elapsed.Round(time.Millisecond))
	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		p := func(q float64) time.Duration { return latencies[int(float64(len(latencies)-1)*q)] }
		fmt.Printf("simulator: enroll latency p50 %s  p95 %s  p99 %s  max %s\n",
			p(0.50).Round(time.Millisecond), p(0.95).Round(time.Millisecond),
			p(0.99).Round(time.Millisecond), latencies[len(latencies)-1].Round(time.Millisecond))
		fmt.Printf("simulator: throughput %.1f enrollments/sec\n", float64(ok)/elapsed.Seconds())
	}
	if len(failures) > 0 {
		fmt.Println("simulator: failures")
		for msg, n := range failures {
			fmt.Printf("  %4d  %s\n", n, msg)
		}
	}

	// Injected link failures are the point of the exercise, not a test
	// failure. A real error is one where the request reached the controller
	// and the controller got it wrong.
	for msg, n := range failures {
		if msg != "link dropped before the request was sent" {
			return fmt.Errorf("%d enrollment(s) failed for a reason other than injected link loss (e.g. %q)", n, msg)
		}
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
