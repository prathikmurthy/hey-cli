# hey-cli zsh integration
# Context: recent history via fc, optional $BUFFER for Ctrl+G widget.
# Fill UX: `print -z` pushes the suggested command onto the next editing buffer.

if ! command -v hey >/dev/null 2>&1; then
  return 0 2>/dev/null || true
fi

_hey_history_blob() {
  local n="${HEY_HISTORY_LINES:-30}"
  fc -ln -"${n}" 2>/dev/null | sed 's/^[[:space:]]*//'
}

# Interactive wrapper: ask the model, then insert the command into the ZLE buffer stack.
hey() {
  if [[ $# -eq 0 ]]; then
    command hey --help
    return $?
  fi

  # Pass-through for subcommands / flags (install, --help, etc.)
  case "$1" in
    install|version|--help|-h|--version|-V|--print|--fill|--explain)
      command hey "$@"
      return $?
      ;;
  esac

  local out
  out="$(
    HEY_HISTORY="$(_hey_history_blob)" \
    HEY_BUFFER="${BUFFER:-}" \
    HEY_SHELL=zsh \
    command hey --fill -- "$@"
  )" || return $?

  if [[ -z "$out" ]]; then
    print -u2 "hey: empty suggestion"
    return 1
  fi

  # Push onto editing buffer so the user can edit / Enter.
  print -z -- "$out"
}

# Ctrl+G: treat current BUFFER as the question (or prompt if empty).
_hey_zle_fill() {
  local q="${BUFFER}"
  if [[ -z "$q" ]]; then
    zle -I
    print -u2 "hey: type a question on the line, then press Ctrl+G"
    return 1
  fi
  # Strip a leading "hey " if the user typed the command form.
  if [[ "$q" == hey\ * ]]; then
    q="${q#hey }"
  fi

  local out
  out="$(
    HEY_HISTORY="$(_hey_history_blob)" \
    HEY_BUFFER="$BUFFER" \
    HEY_SHELL=zsh \
    command hey --fill -- "$q"
  )" || return $?

  BUFFER="$out"
  CURSOR=${#BUFFER}
  zle redisplay
}

zle -N _hey_zle_fill
bindkey '^G' _hey_zle_fill
