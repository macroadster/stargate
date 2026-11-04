#!/bin/sh
# Exercise install.sh without hitting GitHub, and check the btcd version pin
# used by the embed build.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
BTCD_VERSION=$(sed -n 's/^BTCD_VERSION=//p' "$ROOT/scripts/build-btcd.sh")
if [ -z "$BTCD_VERSION" ]; then
	echo "BTCD_VERSION missing from scripts/build-btcd.sh" >&2
	exit 1
fi
if ! grep -q 'scripts/build-btcd.sh' "$ROOT/Dockerfile"; then
	echo "root Dockerfile does not build btcd via scripts/build-btcd.sh" >&2
	exit 1
fi
if ! grep -q 'embedbtcd' "$ROOT/Dockerfile" || ! grep -q 'embedbtcd' "$ROOT/backend/Dockerfile"; then
	echo "Dockerfiles must build stargate with -tags embedbtcd" >&2
	exit 1
fi
if ! grep -q "github.com/btcsuite/btcd@${BTCD_VERSION}" "$ROOT/backend/Dockerfile"; then
	echo "backend/Dockerfile btcd pin is not ${BTCD_VERSION}" >&2
	exit 1
fi

fail() {
	echo "install_test: $1" >&2
	exit 1
}

home=$(mktemp -d)
bin=$(mktemp -d)
install_dir="${home}/.local/bin"
cat > "${bin}/curl" << 'EOF'
#!/bin/sh
out=""
url=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out=$2; shift 2 ;;
    -*) shift ;;
    *) url=$1; shift ;;
  esac
done
case "$url" in
  *stargate-*)
    printf '%s\n' '#!/bin/sh' 'echo stargate-test' 'exit 0' > "$out"
    ;;
  *)
    echo "unexpected url: $url" >&2
    exit 1
    ;;
esac
EOF
chmod +x "${bin}/curl"

HOME="$home" INSTALL_DIR="$install_dir" PATH="${bin}:${PATH}" "$ROOT/install.sh" > "${home}/out"
[ -x "${install_dir}/stargate" ] || fail "stargate was not installed"
got=$("${install_dir}/stargate" --version)
[ "$got" = "stargate-test" ] || fail "stargate --version printed ${got}"
[ ! -e "${install_dir}/btcd" ] || fail "install.sh should not write a separate btcd"
grep -q "Installed stargate" "${home}/out" || fail "missing Installed stargate line: $(cat "${home}/out")"
rm -rf "$home" "$bin"

echo "install_test: ok"
