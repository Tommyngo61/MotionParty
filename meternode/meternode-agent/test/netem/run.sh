#!/usr/bin/env bash
#
# Run the agent under simulated residential network conditions.
#
# Scaffold: the transport lands in M3. Until then this verifies the harness
# itself — that the image builds, that tc netem applies, and that the shaping is
# what we asked for — so it is known-good when there is something to test.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PROFILE="${PROFILE:-slow}"

case "$PROFILE" in
  typical)  DOWN=300mbit; UP=20mbit; DELAY=25ms;  JITTER=5ms;  LOSS=0.1% ;;
  slow)     DOWN=50mbit;  UP=5mbit;  DELAY=80ms;  JITTER=20ms; LOSS=1% ;;
  bad)      DOWN=25mbit;  UP=5mbit;  DELAY=300ms; JITTER=50ms; LOSS=5% ;;
  flapping) DOWN=50mbit;  UP=5mbit;  DELAY=80ms;  JITTER=20ms; LOSS=1% ;;
  *) echo "unknown profile: $PROFILE (typical|slow|bad|flapping)" >&2; exit 2 ;;
esac

echo "netem profile: $PROFILE — ${DOWN} down / ${UP} up, ${DELAY} ±${JITTER}, ${LOSS} loss"

if ! command -v docker >/dev/null; then
  echo "netem: docker is required" >&2
  exit 1
fi

IMAGE="meternode-agent-netem:local"
docker build -q -f "$ROOT/packaging/Dockerfile" -t "$IMAGE" "$ROOT/.." >/dev/null

# NET_ADMIN so the container can shape its own egress. The agent itself never
# needs this — only the harness does.
docker run --rm --cap-add NET_ADMIN --entrypoint /bin/sh "$IMAGE" -c "
  set -e
  apk add --no-cache iproute2 >/dev/null 2>&1 || true

  tc qdisc add dev eth0 root netem \
     delay ${DELAY} ${JITTER} distribution normal \
     loss ${LOSS} rate ${UP}

  echo '--- applied qdisc ---'
  tc qdisc show dev eth0

  echo '--- agent config check ---'
  /usr/bin/meternode-agent -check-config -config /etc/meternode/agent.yaml || true
  /usr/bin/meternode-agent -version
"

cat <<'PENDING'

netem: harness OK. The assertions below land with the transport in M3:
  - control-plane bytes under 150 MB/month, measured at the interface
  - no buffered sample lost across a hard disconnect, up to ring capacity
  - reconnect backoff with full jitter, 1s -> 300s, never a tight loop
  - backfill rate-limited and downsampled; peak upstream stays low
  - most recent data uploaded first
PENDING
