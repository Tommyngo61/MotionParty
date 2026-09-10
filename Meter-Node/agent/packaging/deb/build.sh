#!/usr/bin/env bash
#
# Build the meternode-agent .deb.
#
# No fpm, no dpkg-buildpackage, no build-essential: this runs in CI and on a
# developer laptop with nothing installed but Go and dpkg-deb, which ships with
# Debian and Ubuntu. The package is a static binary, a unit file, and three
# maintainer scripts — the heavyweight tooling would be all cost.
#
#   VERSION=1.2.3 ./packaging/deb/build.sh

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"  # repository root
AGENT="$ROOT/agent"
VERSION="${VERSION:-$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo 0.0.0-dev)}"
# Debian is fussy about version strings in two ways git describe is not: it may
# not contain '-' the way `v1.2.3-4-gabc123` does, and it must start with a
# digit — which a bare commit hash from an untagged repo does not. Both cases
# happen in CI on a branch, so both are handled rather than left to fail at
# package time.
DEB_VERSION="${VERSION#v}"
DEB_VERSION="${DEB_VERSION//-/\~}"
case "$DEB_VERSION" in
  [0-9]*) ;;
  *) DEB_VERSION="0.0.0+${DEB_VERSION}" ;;
esac
ARCH="amd64"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
# mktemp gives 0700; the package root has to be world-readable or dpkg installs
# a directory nothing can traverse.
chmod 0755 "$STAGE"

echo "building meternode-agent ${DEB_VERSION} (${ARCH})"

mkdir -p "$STAGE/DEBIAN" \
         "$STAGE/usr/bin" \
         "$STAGE/lib/systemd/system" \
         "$STAGE/etc/meternode" \
         "$STAGE/usr/share/doc/meternode-agent"

COMMIT="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-s -w"
LDFLAGS+=" -X github.com/MeterHome/Meter-Node/agent/internal/version.Version=${VERSION}"
LDFLAGS+=" -X github.com/MeterHome/Meter-Node/agent/internal/version.Commit=${COMMIT}"
LDFLAGS+=" -X github.com/MeterHome/Meter-Node/agent/internal/version.BuildDate=${BUILD_DATE}"

# CGO off: a static binary that does not care which libc the base image has,
# and cannot be surprised by a glibc upgrade on a node we cannot reach.
( cd "$ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "$LDFLAGS" -o "$STAGE/usr/bin/meternode-agent" ./agent/cmd/agent )
( cd "$ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "$LDFLAGS" -o "$STAGE/usr/bin/meternodectl" ./agent/cmd/agentctl )

install -m 0644 "$AGENT/packaging/systemd/meternode-agent.service" "$STAGE/lib/systemd/system/"
install -m 0640 "$AGENT/packaging/systemd/agent.yaml.example"      "$STAGE/etc/meternode/agent.yaml.example"
install -m 0644 "$AGENT/SECURITY.md"                               "$STAGE/usr/share/doc/meternode-agent/"
install -m 0644 "$AGENT/README.md"                                 "$STAGE/usr/share/doc/meternode-agent/"

SIZE_KB="$(du -ks "$STAGE" | cut -f1)"

cat > "$STAGE/DEBIAN/control" <<CONTROL
Package: meternode-agent
Version: ${DEB_VERSION}
Section: admin
Priority: optional
Architecture: ${ARCH}
Maintainer: MeterHome <ops@meterhome.example>
Installed-Size: ${SIZE_KB}
Depends: adduser, systemd
Recommends: docker.io | docker-ce
Description: MeterNode host agent
 Supervisor for a company-owned GPU compute node hosted in a private
 residence. Collects host and GPU telemetry within a strict bandwidth
 budget, survives network loss, and supervises workload containers under
 enforced network isolation.
 .
 Runs as the unprivileged nodeagent user under systemd. Its only local
 interface is a unix socket; it never opens a listening TCP port.
CONTROL

# The config file is marked conffile so a package upgrade does not clobber a
# node the field tech has tuned.
cat > "$STAGE/DEBIAN/conffiles" <<'CONFFILES'
/etc/meternode/agent.yaml.example
CONFFILES

cat > "$STAGE/DEBIAN/postinst" <<'POSTINST'
#!/bin/sh
set -e

case "$1" in
  configure)
    # A system user with no login shell and no home directory to write to.
    if ! getent passwd nodeagent >/dev/null; then
      adduser --system --group --no-create-home \
              --home /var/lib/meternode --shell /usr/sbin/nologin nodeagent
    fi

    # Membership in the docker group is what lets the agent supervise workload
    # containers without being root. It is also, honestly, root-equivalent on
    # this host — SECURITY.md says so plainly rather than pretending otherwise.
    if getent group docker >/dev/null; then
      usermod -aG docker nodeagent || true
    fi

    install -d -o nodeagent -g nodeagent -m 0750 /var/lib/meternode
    install -d -o root      -g nodeagent -m 0750 /etc/meternode

    # Seed the config on a FIRST install only. An upgrade must never overwrite
    # a node someone has tuned in the field.
    if [ ! -e /etc/meternode/agent.yaml ]; then
      cp /etc/meternode/agent.yaml.example /etc/meternode/agent.yaml
      chown root:nodeagent /etc/meternode/agent.yaml
      chmod 0640 /etc/meternode/agent.yaml
    fi

    # The credential and the node's private key live here. Nothing else on the
    # box has any business reading them.
    for f in /var/lib/meternode/credential /var/lib/meternode/node.key; do
      if [ -e "$f" ]; then
        chown nodeagent:nodeagent "$f"
        chmod 0600 "$f"
      fi
    done

    # Refuse to enable a unit that would immediately fail on a bad config.
    if [ -x /usr/bin/meternode-agent ] && [ -e /etc/meternode/agent.yaml ]; then
      if ! /usr/bin/meternode-agent -config /etc/meternode/agent.yaml -check-config >/dev/null 2>&1; then
        echo "meternode-agent: /etc/meternode/agent.yaml is not valid; not starting." >&2
        echo "  Run: meternode-agent -check-config" >&2
        exit 0
      fi
    fi

    systemctl daemon-reload || true
    systemctl enable meternode-agent.service || true
    if [ -d /run/systemd/system ]; then
      systemctl restart meternode-agent.service || true
    fi
    ;;
esac

exit 0
POSTINST

cat > "$STAGE/DEBIAN/prerm" <<'PRERM'
#!/bin/sh
set -e
case "$1" in
  remove|deconfigure)
    if [ -d /run/systemd/system ]; then
      systemctl stop meternode-agent.service || true
    fi
    systemctl disable meternode-agent.service || true
    ;;
esac
exit 0
PRERM

cat > "$STAGE/DEBIAN/postrm" <<'POSTRM'
#!/bin/sh
set -e
case "$1" in
  purge)
    # The node's credential and private key go on purge, not on remove: an
    # operator removing the package to reinstall it must not have to re-enrol
    # the machine, which needs a new token and, often, a site visit.
    rm -rf /var/lib/meternode
    rm -f  /etc/meternode/agent.yaml
    ;;
esac
systemctl daemon-reload >/dev/null 2>&1 || true
exit 0
POSTRM

chmod 0755 "$STAGE/DEBIAN/postinst" "$STAGE/DEBIAN/prerm" "$STAGE/DEBIAN/postrm"

mkdir -p "$AGENT/dist"
OUT="$AGENT/dist/meternode-agent_${DEB_VERSION}_${ARCH}.deb"
dpkg-deb --build --root-owner-group "$STAGE" "$OUT" >/dev/null

echo "built $OUT"
