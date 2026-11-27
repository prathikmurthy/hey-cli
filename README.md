# hey

Ask a plain-text question in your terminal; get a suggested shell command **filled into your prompt** (editable, ready to Enter), with an optional **dim/gray** note — powered by your **Cursor** subscription.

```bash
hey how do I get new git changes
# → next prompt input line:  git pull
# → dim note under it:       Fetch and merge remote updates.
```

## Install

No Go required. One line downloads a release binary, puts it on your `PATH`, and wires your default shell (`$SHELL`):

```bash
curl -fsSL https://raw.githubusercontent.com/prathikmurthy/hey-cli/main/scripts/install.sh | bash
```

Then **open a new terminal** (or `exec $SHELL -l`) and run:

```bash
hey how do I merge this
```

You also need the [Cursor CLI](https://cursor.com/docs/cli/overview) for answers:

```bash
curl https://cursor.com/install -fsS | bash
agent login
# or: export CURSOR_API_KEY=...
```

### What the installer does

1. Downloads the latest GitHub Release binary for your OS/arch → `~/.local/bin/hey`
2. Ensures `~/.local/bin` is on `PATH` in your shell rc
3. Installs prompt-fill widgets under `~/.hey/shell/` and sources them from `~/.zshrc` / `~/.bashrc` (default shell only; non-interactive)

Options (env or flags if you download the script):

| Option | Meaning |
|--------|---------|
| `HEY_VERSION=v0.1.1` | Pin a release tag (default: latest) |
| `HEY_PREFIX=~/.local` | Install prefix |
| `HEY_SHELLS=zsh,bash` | Wire specific shells |
| `--skip-shell` | Binary only |
| `--from-source` | Build with Go from a local checkout (developers) |

Dry-run:

```bash
curl -fsSL https://raw.githubusercontent.com/prathikmurthy/hey-cli/main/scripts/install.sh | bash -s -- --dry-run
```

### Shell behavior (after install + new terminal)

| Shell | Prompt fill | Note styling | Keybinding |
|-------|-------------|--------------|------------|
| **zsh** | `hey …` uses `print -z` so the **next** prompt input line has the command | ANSI dim (`\033[2m`) on stderr | **Ctrl+G** replaces `$BUFFER` |
| **bash** | `hey …` opens an **editable** readline line (`read -e -i`) ready to Enter | ANSI dim on stderr | **Ctrl+G** fills `READLINE_LINE` |

Re-run shell wiring anytime:

```bash
hey install --yes                 # default shell only
hey install --shells zsh,bash     # explicit
```

### Developers (from source)

```bash
git clone https://github.com/prathikmurthy/hey-cli.git
cd hey-cli
./scripts/install.sh --from-source
# or: go build -o ~/.local/bin/hey ./cmd/hey && hey install --yes
```

## Auth (Cursor — not OpenAI/Anthropic)

hey calls the Cursor CLI non-interactively. Use either `agent login` or `CURSOR_API_KEY` from [Cursor Dashboard → API Keys](https://cursor.com/dashboard).

If `agent` is missing, hey exits non-zero with a one-line install hint.

## Usage

```bash
hey how do I merge this
hey how do I undo the last commit but keep changes
hey --fill how do I list listening ports    # machine-readable COMMAND/NOTE
hey --print how do I squash the last 3      # print only (command + dim note)
hey --model composer-2-fast                 # persist default model
```

### Fill protocol

Shell widgets call `hey --fill`, which prints:

```text
COMMAND:git pull
NOTE:Fetch and merge remote updates.
```

The widget parses that, puts `COMMAND` into the input line, and prints `NOTE` with ANSI dim.

## Speed

- Default model: **`composer-2-fast`**
- `agent -p --mode ask --sandbox disabled --trust --workspace ~/.hey/workspace`
- Tight prompt: only `COMMAND` + optional one-line `NOTE`
- Trimmed history (default **8** lines) and last command output (default **40** lines, max 4 KB)

Set `HEY_DEBUG=1` to print agent round-trip timing on stderr.

## Context & privacy (what is captured)

To diagnose errors (such as a failed `git pull --ff-only` or build error) when you ask `hey what do i do`, `hey` sends compact, sanitized context to the model:

- **Working directory & shell name** (`cwd`, `shell`)
- **Recent command history** (default 8 lines, secrets redacted)
- **Last non-`hey` command & exit code** (`last_command`, `last_exit`)
- **Last command output (~40 lines, capped at 4 KB)**, gathered in this order:
  1. `HEY_LAST_OUTPUT` env override (if explicitly set)
  2. Per-shell tail file (`~/.hey/run/out.<pid>`, mode `0600`) captured by `preexec`/`precmd` (zsh) or `DEBUG`/`PROMPT_COMMAND` (bash) via a fast in-memory ring-buffer tee (`hey _tee`) that never blocks the prompt
  3. Live terminal scrollback when running inside **tmux** (`tmux capture-pane`), **WezTerm** (`wezterm cli get-text`), or **Kitty** (`kitty @ get-text`) — inside tmux/WezTerm, FD teeing is skipped completely
  4. Recent `~/.hey/last_output` fallback (only if modified within 10 minutes)

### What is NOT captured / privacy guardrails

- **Interactive & TUI commands are skipped** (`vim`, `less`, `ssh`, `top`, `fzf`, `git diff`, `git log`, interactive `git commit`, bare REPLs, `ls`, background `&` jobs, and `hey` itself — so asking `hey` twice keeps the original error)
- **Sensitive commands & files are skipped** (`env`, `printenv`, `export`, `pass`, `op`, `vault`, `gpg`, `openssl`, and commands referencing `.env`, secrets, tokens, passwords, private keys, `.pem`/`.key`, `.netrc`, `.aws/credentials`, `kubeconfig`)
- **ANSI stripping & redaction**: all ANSI/OSC escape sequences are stripped; raw `env` dumps are dropped; PEM private keys, API keys/tokens (`sk-...`, `ghp_...`, `AKIA...`, JWTs, `Bearer ...`), URL credentials, and sensitive `KEY=value` lines are redacted before anything leaves your machine
- **Opt out anytime**: set `export HEY_CAPTURE_OUTPUT=0` in your shell rc to disable command output capture (history + cwd still work)

## Releases

Pushing a tag `v*` runs GitHub Actions to build linux/darwin amd64/arm64 tarballs and publish a GitHub Release. The install script downloads `hey_<version>_<os>_<arch>.tar.gz` from that release.

```bash
git tag v0.1.1 && git push origin v0.1.1
```

## Development

```bash
go build -o hey ./cmd/hey
./hey --help
./hey install --dry-run
go test ./...
bash -n scripts/install.sh
```
