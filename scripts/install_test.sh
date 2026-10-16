#!/bin/sh
# Exercise install.sh without hitting GitHub. A fake curl serves stargate
# and, when asked, btcd.
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
if ! grep -q "github.com/btcsuite/btcd@${BTCD_VERSION}" "$ROOT/backend/Dockerfile"; then
	echo "backend/Dockerfile btcd pin is not ${BTCD_VERSION}" >&2
	exit 1
fi

fail() {
	echo "install_test: $1" >&2
	exit 1
}

run_install() {
	mode=$1
	home=$(mktemp -d)
	bin=$(mktemp -d)
	install_dir="${home}/.local/bin"

	# curl -fsSL -o is the stargate download (stdout stays empty).
	# curl -sSL -o -w is the btcd download (stdout is the HTTP status).
	cat > "${bin}/curl" << EOF
#!/bin/sh
out=""
write_code=0
url=""
while [ \$# -gt 0 ]; do
  case "\$1" in
    -o) out=\$2; shift 2 ;;
    -w) write_code=1; shift 2 ;;
    -w*) write_code=1; shift ;;
    -*) shift ;;
    *) url=\$1; shift ;;
  esac
done
code=200
case "\$url" in
  *btcd-*)
    if [ "$mode" = "missing" ]; then
      code=404
    elif [ "$mode" = "error" ]; then
      code=500
    else
      printf '%s\n' '#!/bin/sh' 'echo btcd-test' > "\$out"
    fi
    ;;
  *stargate-*)
    printf '%s\n' '#!/bin/sh' 'echo stargate-test' 'exit 0' > "\$out"
    ;;
  *)
    echo "unexpected url: \$url" >&2
    exit 1
    ;;
esac
if [ "\$write_code" -eq 1 ]; then
  printf '%s' "\$code"
fi
if [ "\$code" != "200" ] && [ "\$write_code" -eq 0 ]; then
  exit 22
fi
exit 0
EOF
	chmod +x "${bin}/curl"

	set +e
	HOME="$home" INSTALL_DIR="$install_dir" PATH="${bin}:${PATH}" \
		"$ROOT/install.sh" > "${home}/out" 2> "${home}/err"
	status=$?
	set -e
	printf '%s\n' "$status" > "${home}/status"
	printf '%s\n' "$install_dir" > "${home}/install_dir"
	printf '%s' "$home"
}

assert_stargate() {
	home=$1
	install_dir=$(cat "${home}/install_dir")
	[ -x "${install_dir}/stargate" ] || fail "stargate was not installed"
	got=$("${install_dir}/stargate" --version)
	[ "$got" = "stargate-test" ] || fail "stargate --version printed ${got}"
}

home=$(run_install ok)
assert_stargate "$home"
install_dir=$(cat "${home}/install_dir")
[ -x "${install_dir}/btcd" ] || fail "btcd was not installed"
got=$("${install_dir}/btcd")
[ "$got" = "btcd-test" ] || fail "btcd printed ${got}"
grep -q "Installed btcd" "${home}/out" || fail "missing Installed btcd line: $(cat "${home}/out")"
status=$(cat "${home}/status")
[ "$status" -eq 0 ] || fail "ok install exited ${status}: $(cat "${home}/err")"
rm -rf "$home"

home=$(run_install missing)
assert_stargate "$home"
install_dir=$(cat "${home}/install_dir")
[ ! -e "${install_dir}/btcd" ] || fail "404 should not install btcd"
grep -q "was not in this release" "${home}/err" || fail "missing 404 warning: $(cat "${home}/err")"
status=$(cat "${home}/status")
[ "$status" -eq 0 ] || fail "missing-btcd install exited ${status}: $(cat "${home}/err")"
rm -rf "$home"

home=$(run_install error)
status=$(cat "${home}/status")
[ "$status" -ne 0 ] || fail "HTTP 500 should fail the install"
rm -rf "$home"

echo "install_test: ok"
