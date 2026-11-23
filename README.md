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

If the repo is **private**, export a GitHub token (repo scope) so the installer can fetch release assets via the API:

```bash
export GH_TOKEN=ghp_…   # or GITHUB_TOKEN
curl -fsSL -H "Authorization: Bearer $GH_TOKEN" \
  https://raw.githubusercontent.com/prathikmurthy/hey-cli/main/scripts/install.sh | bash
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
- Trimmed history (default **8** lines)

Set `HEY_DEBUG=1` to print agent round-trip timing on stderr.

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
