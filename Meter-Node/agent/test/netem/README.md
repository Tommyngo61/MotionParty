# `nettest` — the agent under a residential link

Most of this system's failure modes only appear under bad network conditions,
and none of them appear on a developer's laptop on loopback. This harness uses
`tc netem` inside a container to reproduce what a node actually sits on.

**Status: scaffold.** The transport lands in M3; until then there is nothing to
shape. `run.sh` builds the image and asserts the shaping applies, so the harness
itself is known-good when the transport arrives.

## The profiles

Taken from the shared contract's environmental assumptions, not invented:

| Profile | Down | Up | Latency | Loss | Models |
| --- | --- | --- | --- | --- | --- |
| `typical` | 300 Mbps | 20 Mbps | 25 ms | 0.1% | A decent suburban cable connection |
| `slow` | 50 Mbps | 5 Mbps | 80 ms | 1% | The floor of what we will accept |
| `bad` | 25 Mbps | 5 Mbps | 300 ms | 5% | Rural fixed-wireless or a saturated uplink |
| `flapping` | 50 Mbps | 5 Mbps | 80 ms | 1% | `slow`, plus the link dropping for 30–120 s at random |

Upstream is the scarce resource in every one of them, which is the whole point:
a design that only works when the uplink is fast is a design that fails in most
of the houses we will be in.

## What it will assert (M3)

1. **The agent stays within budget.** Control-plane bytes over a simulated month
   land under 150 MB, measured at the interface rather than trusted from the
   agent's own accounting.
2. **No buffered sample is lost** across a hard disconnect and reconnect, up to
   the ring's capacity — and when the ring does overflow, the drop is reported
   rather than silent.
3. **Reconnect is backoff with full jitter**, 1 s → 300 s, never a tight retry
   loop. Asserted by timing the reconnect attempts, because a fleet whose ISP
   recovered together must not return as a thundering herd.
4. **Backfill is rate-limited and downsampled.** A node returning from an outage
   must not saturate the homeowner's upstream — measured as peak upstream during
   the catch-up window, not just total bytes.
5. **Recent data first.** On reconnect the controller sees current state before
   history.

## Running it

```bash
make netem                      # all profiles
PROFILE=bad ./test/netem/run.sh # one profile
```

Needs Docker and `NET_ADMIN` (the container manipulates its own qdisc). It does
not need a GPU: the GPU collector's no-driver path is itself worth exercising
here, since it is what a node does during a driver upgrade.

## Why a container rather than a VM or a real node

`tc netem` in a network namespace reproduces bandwidth, latency, jitter, loss,
and reordering faithfully enough for everything above, and it runs in CI. What
it does *not* reproduce is the middlebox behaviour that breaks long-lived
WebSockets on some consumer ISPs — captive portals, idle-timeout NAT gateways,
deep-packet-inspection appliances. Those need a real node on a real connection,
and they are why the HTTP batch fallback exists at all.
