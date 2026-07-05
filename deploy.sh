#!/usr/bin/env bash
# Build restic-coverage for each host's arch and deploy to
# /usr/local/bin/restic-coverage over ssh. This is the secondary deploy path —
# the primary is the apt repo (apt.guldmund.dk); use this for pre-release
# testing on the fleet.
#
# Usage: ./deploy.sh [host ...]     (default: all hosts below)
#
# Kept portable to bash 3.2 (macOS default): no associative arrays.
set -euo pipefail

cd "$(dirname "$0")"

# "host:GOARCH" pairs. Add hosts here as the fleet grows.
FLEET="optiplex:amd64 pi:arm64"

archfor() {
    for pair in $FLEET; do
        [ "${pair%%:*}" = "$1" ] && { echo "${pair##*:}"; return 0; }
    done
    return 1
}

VER="$(git describe --tags --always 2>/dev/null || git rev-parse --short HEAD)"
LDFLAGS="-X restic-coverage/internal/cli.Version=$VER"

if [ "$#" -gt 0 ]; then
    targets="$*"
else
    targets=""
    for pair in $FLEET; do targets="$targets ${pair%%:*}"; done
fi

for host in $targets; do
    arch="$(archfor "$host")" || {
        echo "!! unknown host '$host' (known: $FLEET)" >&2
        exit 2
    }
    echo "==> building $host ($arch) @ $VER"
    bin="$(mktemp)"
    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -ldflags "$LDFLAGS" -o "$bin" ./

    echo "==> deploying to $host:/usr/local/bin/restic-coverage"
    ssh "$host" 'cat > /tmp/restic-coverage.new && sudo install -m 755 /tmp/restic-coverage.new /usr/local/bin/restic-coverage && rm /tmp/restic-coverage.new && echo "    $(hostname): $(restic-coverage version)"' < "$bin"
    rm -f "$bin"
done

echo "==> done"
