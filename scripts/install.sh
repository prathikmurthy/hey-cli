#!/usr/bin/env bash
# Install hey: download a release binary (or build from a local checkout),
# put it on PATH, and wire shell integration for the default shell.
#
# One-liner (no Go required):
#   curl -fsSL https://raw.githubusercontent.com/prathikmurthy/hey-cli/main/scripts/install.sh | bash
#
# Env:
#   HEY_PREFIX   install prefix (default: ~/.local) → $HEY_PREFIX/bin/hey
#   HEY_VERSION  release tag (default: latest)
#   HEY_REPO     owner/repo (default: prathikmurthy/hey-cli)
#   HEY_SHELLS   comma list for hey install --shells (default: auto via --yes)
set -euo pipefail

REPO="${HEY_REPO:-prathikmurthy/hey-cli}"
PREFIX="${HEY_PREFIX:-$HOME/.local}"
BIN_DIR="${PREFIX}/bin"
VERSION="${HEY_VERSION:-latest}"
DRY_RUN=0
SKIP_SHELL=0
FROM_SOURCE=0
ROOT=""

usage() {
  cat <<'EOF'
Usage: install.sh [options]

  --dry-run       Print actions without writing
  --prefix DIR    Install prefix (default: ~/.local)
  --version TAG   Release tag (default: latest)
  --skip-shell    Only install the binary; skip shell wiring
  --from-source   Build with Go from a local git checkout (dev)
  -h, --help      Show help

Curl one-liner (recommended):
  curl -fsSL https://raw.githubusercontent.com/prathikmurthy/hey-cli/main/scripts/install.sh | bash
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=1; shift ;;
    --prefix) PREFIX="$2"; BIN_DIR="${PREFIX}/bin"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --skip-shell) SKIP_SHELL=1; shift ;;
    --from-source) FROM_SOURCE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown arg: $1" >&2; usage; exit 2 ;;
  esac
done

# Detect if we were invoked from a git checkout of hey-cli.
if [[ -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" ]]; then
  _script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  if [[ -f "${_script_dir}/../go.mod" && -d "${_script_dir}/../cmd/hey" ]]; then
    ROOT="$(cd "${_script_dir}/.." && pwd)"
  fi
fi

os_name() {
  case "$(uname -s)" in
    Linux*) echo linux ;;
    Darwin*) echo darwin ;;
    *) echo "unsupported OS: $(uname -s)" >&2; exit 1 ;;
  esac
}

arch_name() {
  case "$(uname -m)" in
    x86_64|amd64) echo amd64 ;;
    aarch64|arm64) echo arm64 ;;
    *) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;;
  esac
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "hey install: need '$1' on PATH" >&2
    exit 1
  }
}

# Optional auth for private repos: GH_TOKEN or GITHUB_TOKEN.
curl_gh() {
  local args=(-fsSL)
  local token="${GH_TOKEN:-${GITHUB_TOKEN:-}}"
  if [[ -n "$token" ]]; then
    args+=(-H "Authorization: Bearer ${token}" -H "Accept: application/vnd.github+json")
  fi
  curl "${args[@]}" "$@"
}

resolve_tag() {
  local ver="$1"
  if [[ "$ver" != "latest" ]]; then
    echo "$ver"
    return
  fi
  need_cmd curl
  local url="https://api.github.com/repos/${REPO}/releases/latest"
  local tag
  tag="$(curl_gh "$url" | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"
  if [[ -z "$tag" ]]; then
    echo "hey install: could not resolve latest release for ${REPO}" >&2
    echo "  Publish a GitHub Release (tag v*), or set HEY_VERSION=vX.Y.Z / use --from-source." >&2
    if [[ -z "${GH_TOKEN:-${GITHUB_TOKEN:-}}" ]]; then
      echo "  Private repo? export GH_TOKEN=… (or GITHUB_TOKEN) and retry." >&2
    fi
    exit 1
  fi
  echo "$tag"
}

asset_url() {
  local tag="$1" os="$2" arch="$3"
  # Expected asset: hey_<tag>_<os>_<arch>.tar.gz  (tag may include leading v)
  local ver="${tag#v}"
  echo "https://github.com/${REPO}/releases/download/${tag}/hey_${ver}_${os}_${arch}.tar.gz"
}

download_binary() {
  local tag os arch url tmp dir
  tag="$(resolve_tag "$VERSION")"
  os="$(os_name)"
  arch="$(arch_name)"
  url="$(asset_url "$tag" "$os" "$arch")"
  need_cmd curl
  need_cmd tar

  tmp="$(mktemp -d "${TMPDIR:-/tmp}/hey-install.XXXXXX")"
  trap 'rm -rf "$tmp"' RETURN

  echo "Downloading hey ${tag} (${os}/${arch})…"
  if ! curl_gh "$url" -o "${tmp}/hey.tgz"; then
    echo "hey install: download failed: $url" >&2
    echo "  Assets should look like hey_${tag#v}_${os}_${arch}.tar.gz" >&2
    if [[ -z "${GH_TOKEN:-${GITHUB_TOKEN:-}}" ]]; then
      echo "  Private repo? export GH_TOKEN=… and retry." >&2
    fi
    exit 1
  fi
  tar -xzf "${tmp}/hey.tgz" -C "$tmp"
  if [[ ! -f "${tmp}/hey" ]]; then
    # Allow a nested single file named hey anywhere in the archive.
    local found
    found="$(find "$tmp" -type f -name hey | head -1)"
    if [[ -z "$found" ]]; then
      echo "hey install: archive did not contain hey binary" >&2
      exit 1
    fi
    mv "$found" "${tmp}/hey"
  fi
  chmod +x "${tmp}/hey"
  mkdir -p "$BIN_DIR"
  mv "${tmp}/hey" "${BIN_DIR}/hey"
  echo "Installed ${BIN_DIR}/hey"
}

build_from_source() {
  need_cmd go
  if [[ -z "$ROOT" ]]; then
    echo "hey install: --from-source requires a local hey-cli checkout" >&2
    exit 1
  fi
  mkdir -p "$BIN_DIR"
  echo "Building hey from source…"
  (cd "$ROOT" && go build -ldflags "-s -w -X main.version=$(git -C "$ROOT" describe --tags --always 2>/dev/null || echo dev)" -o "${BIN_DIR}/hey" ./cmd/hey)
  echo "Installed ${BIN_DIR}/hey"
}

ensure_path_in_rc() {
  local shell_name rc path_line
  shell_name="$(basename "${SHELL:-}")"
  path_line="export PATH=\"${BIN_DIR}:\$PATH\""
  case "$shell_name" in
    zsh)
      if [[ -n "${ZDOTDIR:-}" ]]; then rc="${ZDOTDIR}/.zshrc"; else rc="${HOME}/.zshrc"; fi
      ;;
    bash)
      rc="${HOME}/.bashrc"
      # macOS login shells often only read .bash_profile
      if [[ "$(uname -s)" == "Darwin" ]]; then
        local bp="${HOME}/.bash_profile"
        if [[ ! -f "$bp" ]] || ! grep -q '\.bashrc' "$bp" 2>/dev/null; then
          if [[ "$DRY_RUN" -eq 1 ]]; then
            echo "[dry-run] would ensure ${bp} sources ~/.bashrc"
          else
            {
              echo ""
              echo "# >>> hey-cli path >>>"
              echo '[[ -f ~/.bashrc ]] && source ~/.bashrc'
              echo "# <<< hey-cli path <<<"
            } >>"$bp"
          fi
        fi
      fi
      ;;
    *)
      rc=""
      ;;
  esac

  case ":$PATH:" in
    *":${BIN_DIR}:"*) ;;
    *)
      export PATH="${BIN_DIR}:$PATH"
      ;;
  esac

  if [[ -z "$rc" ]]; then
    return 0
  fi
  if grep -Fqs "${BIN_DIR}" "$rc" 2>/dev/null; then
    return 0
  fi
  if [[ "$DRY_RUN" -eq 1 ]]; then
    echo "[dry-run] would append PATH export to $rc"
    return 0
  fi
  {
    echo ""
    echo "# >>> hey-cli path >>>"
    echo "$path_line"
    echo "# <<< hey-cli path <<<"
  } >>"$rc"
  echo "Added ${BIN_DIR} to PATH in $rc"
}

wire_shell() {
  export PATH="${BIN_DIR}:$PATH"
  local hey_bin="${BIN_DIR}/hey"
  if [[ ! -x "$hey_bin" ]]; then
    echo "hey install: binary missing at $hey_bin" >&2
    exit 1
  fi
  if [[ -n "${HEY_SHELLS:-}" ]]; then
    "$hey_bin" install --shells "$HEY_SHELLS"
  else
    # Non-interactive: default to $SHELL (and detected fallbacks inside hey).
    "$hey_bin" install --yes
  fi
}

# --- main ---

if [[ "$DRY_RUN" -eq 1 ]]; then
  echo "[dry-run] prefix=${PREFIX} version=${VERSION} repo=${REPO}"
  if [[ "$FROM_SOURCE" -eq 1 ]]; then
    echo "[dry-run] would build from source → ${BIN_DIR}/hey"
  elif [[ -n "$ROOT" ]]; then
    echo "[dry-run] would prefer release binary, else build from checkout → ${BIN_DIR}/hey"
  else
    echo "[dry-run] would download release binary → ${BIN_DIR}/hey"
  fi
  echo "[dry-run] would ensure ${BIN_DIR} is on PATH via shell rc"
  if [[ "$SKIP_SHELL" -eq 0 ]]; then
    echo "[dry-run] would run: hey install --yes"
    if [[ -x "${BIN_DIR}/hey" ]]; then
      "${BIN_DIR}/hey" install --dry-run || true
    elif [[ -n "$ROOT" ]] && command -v go >/dev/null 2>&1; then
      (cd "$ROOT" && go run ./cmd/hey install --dry-run) || true
    fi
  fi
  exit 0
fi

# Prefer release binary. Fall back to source only when explicitly requested,
# or when running from a checkout and no release exists yet.
if [[ "$FROM_SOURCE" -eq 1 ]]; then
  build_from_source
elif [[ -n "$ROOT" ]] && ! curl_gh "https://api.github.com/repos/${REPO}/releases/latest" >/dev/null 2>&1; then
  echo "No GitHub release found yet; building from local checkout…"
  build_from_source
else
  download_binary
fi

if [[ "$SKIP_SHELL" -eq 0 ]]; then
  echo
  wire_shell
else
  # Binary-only: still make sure ~/.local/bin (or prefix) is on PATH in rc.
  ensure_path_in_rc
fi

cat <<EOF

✓ hey is installed.

Open a new terminal (or run: exec \$SHELL -l), then:

  hey how do I merge this

Requires the Cursor CLI for answers:
  curl https://cursor.com/install -fsS | bash
  agent login
EOF
