# hey

Ask a plain-text question in your terminal; get a suggested shell command **filled into your prompt** (editable, ready to Enter), with an optional **dim/gray** note — powered by your **Cursor** subscription.

```bash
hey how do I get new git changes
# → next prompt input line:  git pull
# → dim note under it:       Fetch and merge remote updates.
```

## Install

Requires [Go 1.22+](https://go.dev/dl/) to build, and the [Cursor CLI](https://cursor.com/docs/cli/overview) (`agent`) for answers.

```bash
# Cursor CLI (if you don't have `agent` yet)
curl https://cursor.com/install -fsS | bash

# Build hey → ~/.local/bin/hey and wire up your shell(s)
git clone https://github.com/prathikmurthy/hey-cli.git
cd hey-cli
./scripts/install.sh
```

### One-liner to source (after install)

```bash
# zsh
source ~/.hey/shell/hey.zsh

# bash
source ~/.hey/shell/hey.bash
```

`hey install` also appends that source to `~/.zshrc` / `~/.bashrc`. Reload the shell or run the one-liner above, then type `hey …`.

Dry-run (no writes):

```bash
./scripts/install.sh --dry-run
hey install --dry-run
```

Or build manually:

```bash
go build -o ~/.local/bin/hey ./cmd/hey
hey install
```

### Shell setup

`hey install` will:

1. **Detect** installed shells (`zsh`, `bash` on `PATH`)
2. **Default** to your login shell (`$SHELL`)
3. **Prompt** which shell(s) to wire (comma-separated; you can pick more than one)
4. Install widgets under `~/.hey/shell/` and source them from `~/.zshrc` / `~/.bashrc`

Non-interactive:

```bash
hey install --yes                 # default shell only
hey install --shells zsh,bash     # explicit
```

| Shell | Prompt fill | Note styling | Keybinding |
|-------|-------------|--------------|------------|
| **zsh** | `hey …` uses `print -z` so the **next** prompt input line has the command | ANSI dim (`\033[2m`) on stderr | **Ctrl+G** replaces `$BUFFER` |
| **bash** | `hey …` opens an **editable** readline line (`read -e -i`) ready to Enter | ANSI dim on stderr | **Ctrl+G** fills `READLINE_LINE` |

Bare `hey …` (binary, no function) still stages a fill when integration is sourced: it writes `~/.hey/pending` and emits an OSC hint; zsh `precmd` / bash `PROMPT_COMMAND` pick it up. Without sourcing, you get the command + dim note on the terminal and a tip to source the widget.

## Auth (Cursor — not OpenAI/Anthropic)

hey calls the Cursor CLI non-interactively. Use either:

```bash
agent login
```

or set an API key from [Cursor Dashboard → API Keys](https://cursor.com/dashboard):

```bash
export CURSOR_API_KEY=...
```

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

The widget parses that, puts `COMMAND` into the input line, and prints `NOTE` with ANSI dim. Default model asks return the same shape.

## Speed

hey optimizes for snappy simple asks (e.g. git how-tos):

- Default model: **`composer-2-fast`** (override with `hey --model` / `~/.hey/config.json`)
- `agent -p --mode ask --sandbox disabled --trust`
- Empty `--workspace` (`~/.hey/workspace`) so the CLI does not index your current repo
- Tight prompt: only `COMMAND` + optional one-line `NOTE`; “do not use tools”
- Trimmed history context (default **8** lines)

Under the hood:

```text
agent -p --mode ask --output-format text --trust --sandbox disabled \
  --workspace ~/.hey/workspace --model composer-2-fast "<prompt>"
```

Set `HEY_DEBUG=1` to print agent round-trip timing on stderr.

## Context collection

hey sends recent terminal context with your question:

1. **From widgets** — `HEY_HISTORY` (last ~8 lines from zsh `fc` / bash `history`) and optional `HEY_BUFFER` (current prompt line for Ctrl+G)
2. **Fallback** — `$HISTFILE`, else `~/.zsh_history` / `~/.bash_history` (zsh extended-history prefixes are stripped)
3. Always includes **cwd** and shell name

Config lives at `~/.hey/config.json` (`model`, `shells`, `history_lines`).

## Development

```bash
go build -o hey ./cmd/hey
./hey --help
./hey install --dry-run
go test ./...
# widget sources:
ls shell/hey.zsh shell/hey.bash
```
