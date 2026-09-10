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

# The schema module is a sibling directory during development, so the build
# context is the repository's PARENT.
COPY meternode-proto/go.mod meternode-proto/go.sum ./meternode-proto/
COPY meternode-agent/go.mod meternode-agent/go.sum ./meternode-agent/
RUN cd meternode-agent && go mod download

COPY meternode-proto/ ./meternode-proto/
COPY meternode-agent/ ./meternode-agent/

ARG VERSION=dev
RUN cd meternode-agent && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath \
      -ldflags "-s -w -X github.com/meterhome/meternode-agent/internal/version.Version=${VERSION}" \
      -o /out/meternode-agent ./cmd/agent && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath \
      -ldflags "-s -w -X github.com/meterhome/meternode-agent/internal/version.Version=${VERSION}" \
      -o /out/meternodectl ./cmd/agentctl

FROM alpine:3.20

RUN addgroup -S nodeagent && adduser -S -G nodeagent -H -h /var/lib/meternode nodeagent && \
    mkdir -p /var/lib/meternode /run/meternode /etc/meternode && \
    chown -R nodeagent:nodeagent /var/lib/meternode /run/meternode

COPY --from=build /out/meternode-agent /usr/bin/meternode-agent
COPY --from=build /out/meternodectl    /usr/bin/meternodectl
COPY meternode-agent/packaging/systemd/agent.yaml.example /etc/meternode/agent.yaml

USER nodeagent
ENTRYPOINT ["/usr/bin/meternode-agent"]
