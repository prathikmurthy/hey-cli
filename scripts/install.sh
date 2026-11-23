#!/usr/bin/env bash
# Install hey: build (if needed), place on PATH, then run `hey install` for shell wiring.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PREFIX="${HEY_PREFIX:-$HOME/.local}"
BIN_DIR="${PREFIX}/bin"
DRY_RUN=0
SKIP_SHELL=0

usage() {
  cat <<'EOF'
Usage: scripts/install.sh [--dry-run] [--prefix DIR] [--skip-shell]

  --dry-run      Print actions without writing
  --prefix DIR   Install prefix (default: ~/.local) → DIR/bin/hey
  --skip-shell   Only install the binary; skip hey install shell wiring
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=1; shift ;;
    --prefix) PREFIX="$2"; BIN_DIR="${PREFIX}/bin"; shift 2 ;;
    --skip-shell) SKIP_SHELL=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown arg: $1" >&2; usage; exit 2 ;;
  esac
done

need_go() {
  command -v go >/dev/null 2>&1 || {
    echo "Go is required to build hey. Install Go 1.22+ or download a release binary." >&2
    exit 1
  }
}

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo "[dry-run] would build hey → ${BIN_DIR}/hey"
  echo "[dry-run] would ensure ${BIN_DIR} is on PATH"
  if [[ "$SKIP_SHELL" -eq 0 ]]; then
    echo "[dry-run] would run: hey install --dry-run"
    if [[ -x "${BIN_DIR}/hey" ]]; then
      "${BIN_DIR}/hey" install --dry-run || true
    elif command -v go >/dev/null 2>&1; then
      (cd "$ROOT" && go run ./cmd/hey install --dry-run) || true
    else
      echo "[dry-run] (hey binary not built yet; shell detect skipped)"
    fi
  fi
  exit 0
fi

need_go
mkdir -p "$BIN_DIR"
echo "Building hey…"
(cd "$ROOT" && go build -o "${BIN_DIR}/hey" ./cmd/hey)
echo "Installed ${BIN_DIR}/hey"

case ":$PATH:" in
  *":${BIN_DIR}:"*) ;;
  *)
    echo "Add to PATH (e.g. in ~/.zshrc / ~/.bashrc):"
    echo "  export PATH=\"${BIN_DIR}:\$PATH\""
    export PATH="${BIN_DIR}:$PATH"
    ;;
esac

if [[ "$SKIP_SHELL" -eq 0 ]]; then
  echo
  "${BIN_DIR}/hey" install
fi
