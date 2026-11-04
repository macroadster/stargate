#!/bin/sh
# Build the managed btcd child (ADR 0006) so it can be embedded in stargate.
# The Docker image, make single-binary, and GitHub releases all call this
# script so they ship the same btcd version. Pass GOOS and GOARCH to
# cross-compile. The output path is the go:embed input (bitcoin/btcd.embed).
set -eu

BTCD_VERSION=v0.27.0
OUT=${1:-btcd}

mkdir -p "$(dirname "$OUT")"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# Build inside a throwaway module that pins btcd to BTCD_VERSION, writing the
# executable straight to -o. go build supports cross-compilation; go install
# cannot, because Go refuses to install a cross-compiled binary into GOBIN.
# -mod=mod lets go fill in go.sum for the pinned graph. -C keeps the caller's
# working directory so a relative $OUT path still resolves.
# GOOS and GOARCH, when set by the caller, select the target platform.
# Go 1.27.3 matches the v0.27.0 release toolchain and includes the
# crypto/x509 root-store fix for CVE-2026-111943.
cat > "$tmp/go.mod" <<EOF
module btcd-build

go 1.27

require github.com/btcsuite/btcd ${BTCD_VERSION}
EOF
GOTOOLCHAIN=go1.27.3 CGO_ENABLED=0 go -C "$tmp" build -mod=mod -trimpath \
	-o "$tmp/btcd" github.com/btcsuite/btcd

if [ ! -x "$tmp/btcd" ]; then
	echo "btcd build did not produce an executable at $tmp/btcd" >&2
	exit 1
fi

cp "$tmp/btcd" "$OUT"
chmod 0755 "$OUT"
echo "Built btcd ${BTCD_VERSION} at ${OUT}"
