#!/bin/sh
# Build the managed btcd child (ADR 0006) so it can be embedded in stargate.
# The Docker image, make single-binary, and GitHub releases all call this
# script so they ship the same btcd version. Pass GOOS and GOARCH to
# cross-compile. The output path is the go:embed input (bitcoin/btcd.embed).
set -eu

BTCD_VERSION=v0.25.0
OUT=${1:-btcd}

mkdir -p "$(dirname "$OUT")"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# pkg@version ignores the current module and writes the program name into GOBIN.
# GOOS and GOARCH, when set by the caller, select the target platform.
CGO_ENABLED=0 GOBIN="$tmp" go install -trimpath "github.com/btcsuite/btcd@${BTCD_VERSION}"

if [ ! -x "$tmp/btcd" ]; then
	echo "btcd build did not produce an executable at $tmp/btcd" >&2
	exit 1
fi

cp "$tmp/btcd" "$OUT"
chmod 0755 "$OUT"
echo "Built btcd ${BTCD_VERSION} at ${OUT}"
