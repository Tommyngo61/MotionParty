# Build the controller as a static binary, then ship it on a distroless base.
#
# The controller embeds its own migrations, so there is no separate migration
# image to keep in step with this one and no init container that can drift.

FROM golang:1.23-alpine AS build

WORKDIR /src

# The schema module is a sibling directory during development, so the build
# context is the repository's PARENT. See deploy/docker-compose.yml.
COPY meternode-proto/go.mod meternode-proto/go.sum ./meternode-proto/
COPY meternode-console/go.mod meternode-console/go.sum ./meternode-console/
RUN cd meternode-console && go mod download

COPY meternode-proto/ ./meternode-proto/
COPY meternode-console/ ./meternode-console/

ARG VERSION=dev
RUN cd meternode-console && \
    CGO_ENABLED=0 GOOS=linux go build \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/controller ./cmd/controller && \
    CGO_ENABLED=0 GOOS=linux go build \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/simulator ./cmd/simulator

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/controller /usr/local/bin/controller
COPY --from=build /out/simulator  /usr/local/bin/simulator

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/controller"]
