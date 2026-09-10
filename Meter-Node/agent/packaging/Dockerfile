# A development image for the agent.
#
# NOT how a node runs in production — a real node runs the .deb under systemd
# as the unprivileged nodeagent user. This exists so the agent can be exercised
# in CI and in the controller's compose stack without a spare GPU box.
#
# The GPU collector degrades cleanly here (no driver, no nvidia-smi), which is
# itself worth exercising: it is the same path a node takes during a driver
# upgrade.

FROM golang:1.23-alpine AS build

WORKDIR /src

# The build context is the repository root: one module, so one go.mod and one
# download step covers the schema package too.
COPY go.mod go.sum ./
RUN go mod download

COPY proto/ ./proto/
COPY agent/  ./agent/

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
      -ldflags "-s -w -X github.com/MeterHome/Meter-Node/agent/internal/version.Version=${VERSION}" \
      -o /out/meternode-agent ./agent/cmd/agent && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath \
      -ldflags "-s -w -X github.com/MeterHome/Meter-Node/agent/internal/version.Version=${VERSION}" \
      -o /out/meternodectl ./agent/cmd/agentctl

FROM alpine:3.20

RUN addgroup -S nodeagent && adduser -S -G nodeagent -H -h /var/lib/meternode nodeagent && \
    mkdir -p /var/lib/meternode /run/meternode /etc/meternode && \
    chown -R nodeagent:nodeagent /var/lib/meternode /run/meternode

COPY --from=build /out/meternode-agent /usr/bin/meternode-agent
COPY --from=build /out/meternodectl    /usr/bin/meternodectl
COPY agent/packaging/systemd/agent.yaml.example /etc/meternode/agent.yaml

USER nodeagent
ENTRYPOINT ["/usr/bin/meternode-agent"]
