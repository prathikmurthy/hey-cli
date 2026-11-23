# hey

Ask a plain-text question in your terminal; get a suggested shell command filled into your prompt (zsh ZLE / bash readline), powered by your **Cursor** subscription.

```bash
hey how do I merge this
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

After install, reload the shell (`source ~/.zshrc` or open a new terminal).

| Shell | Prompt fill | Keybinding |
|-------|-------------|------------|
| **zsh** | `hey …` uses `print -z` so the next prompt has the command ready to edit/Enter | **Ctrl+G** fills `$BUFFER` |
| **bash** | `hey …` stages via `history -s` (Up to recall) and prints the command | **Ctrl+G** fills `READLINE_LINE` |

Outside the widget (e.g. scripts), `hey` prints the suggestion to stdout.

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
hey --fill how do I list listening ports    # command only
hey --print how do I squash the last 3     # command + short explanation
hey --model gpt-5                          # persist model for agent --model
```

Under the hood (fill path):

```text
agent -p --mode ask --output-format text --trust [--model NAME] "<prompt>"
```

## Context collection

hey sends recent terminal context with your question:

1. **From widgets** — `HEY_HISTORY` (last ~30 lines from zsh `fc` / bash `history`) and optional `HEY_BUFFER` (current prompt line for Ctrl+G)
2. **Fallback** — `$HISTFILE`, else `~/.zsh_history` / `~/.bash_history` (zsh extended-history prefixes are stripped)
3. Always includes **cwd** and shell name

Config lives at `~/.hey/config.json` (`model`, `shells`, `history_lines`).

## Development

```bash
go build -o hey ./cmd/hey
./hey --help
./hey install --dry-run
# widget sources:
ls shell/hey.zsh shell/hey.bash
```
