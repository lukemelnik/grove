#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd "$(dirname "$0")" && pwd)
bash -n "${script_dir}/install.sh"

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT
mkdir -p "${tmpdir}/bin" "${tmpdir}/install"

cat >"${tmpdir}/bin/uname" <<'EOF'
#!/usr/bin/env bash
case "$1" in
  -s) printf '%s\n' 'MINGW64_NT' ;;
  -m) printf '%s\n' 'x86_64' ;;
  *) exit 2 ;;
esac
EOF

cat >"${tmpdir}/bin/curl" <<'EOF'
#!/usr/bin/env bash
for arg in "$@"; do
  if [[ "$arg" == *'/releases/latest' ]]; then
    printf '%s\n' '{"tag_name":"v1.2.3"}'
    exit 0
  fi
done
while (($# > 0)); do
  if [[ $1 == '-o' ]]; then
    : >"$2"
    exit 0
  fi
  shift
done
exit 2
EOF

cat >"${tmpdir}/bin/unzip" <<'EOF'
#!/usr/bin/env bash
: >grove.exe
EOF
chmod +x "${tmpdir}/bin/uname" "${tmpdir}/bin/curl" "${tmpdir}/bin/unzip"

PATH="${tmpdir}/bin:${PATH}" GROVE_INSTALL_DIR="${tmpdir}/install" \
  bash "${script_dir}/install.sh" >/dev/null

test -f "${tmpdir}/install/grove.exe"
test ! -e "${tmpdir}/install/grove"
