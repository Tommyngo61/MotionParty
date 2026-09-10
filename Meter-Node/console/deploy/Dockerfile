# Build the controller as a static binary, then ship it on a distroless base.
#
# The controller embeds its own migrations, so there is no separate migration
# image to keep in step with this one and no init container that can drift.

FROM golang:1.23-alpine AS build

WORKDIR /src

# The build context is the repository root: one module, so one go.mod and one
# download step covers the schema package too.
COPY go.mod go.sum ./
RUN go mod download

COPY proto/   ./proto/
COPY console/ ./console/

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/controller ./console/cmd/controller && \
    CGO_ENABLED=0 GOOS=linux go build \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/simulator ./console/cmd/simulator

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/controller /usr/local/bin/controller
COPY --from=build /out/simulator  /usr/local/bin/simulator

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/controller"]
