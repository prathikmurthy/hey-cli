# hey-cli bash integration
# Context: recent history via history builtin; readline fill via bind -x (Ctrl+G).

if ! command -v hey >/dev/null 2>&1; then
  return 0 2>/dev/null || true
fi

_hey_history_blob() {
  local n="${HEY_HISTORY_LINES:-30}"
  history "${n}" 2>/dev/null | sed 's/^[[:space:]]*[0-9]*[[:space:]]*//'
}

# Interactive wrapper: suggest a command and stage it for readline editing.
hey() {
  if [[ $# -eq 0 ]]; then
    command hey --help
    return $?
  fi

  case "$1" in
    install|version|--help|-h|--version|-V|--print|--fill|--explain)
      command hey "$@"
      return $?
      ;;
  esac

  local out
  out="$(
    HEY_HISTORY="$(_hey_history_blob)" \
    HEY_BUFFER="${READLINE_LINE:-}" \
    HEY_SHELL=bash \
    command hey --fill -- "$@"
  )" || return $?

  if [[ -z "$out" ]]; then
    echo "hey: empty suggestion" >&2
    return 1
  fi

  # Stage for Up-arrow / next line; also print so the user sees it.
  history -s -- "$out"
  # If we are inside a bind -x context this would work; for argv form, stash for PROMPT_COMMAND.
  export HEY_INSERT="$out"
  echo "$out"
  echo "(suggestion saved — press Up, or Ctrl+G next time to fill the line)" >&2
}

_hey_readline_fill() {
  local q="${READLINE_LINE}"
  if [[ -z "$q" && -n "${HEY_INSERT:-}" ]]; then
    READLINE_LINE="$HEY_INSERT"
    READLINE_POINT=${#READLINE_LINE}
    unset HEY_INSERT
    return 0
  fi
  if [[ -z "$q" ]]; then
    echo "hey: type a question on the line, then press Ctrl+G" >&2
    return 1
  fi
  if [[ "$q" == hey\ * ]]; then
    q="${q#hey }"
  fi

  local out
  out="$(
    HEY_HISTORY="$(_hey_history_blob)" \
    HEY_BUFFER="$READLINE_LINE" \
    HEY_SHELL=bash \
    command hey --fill -- "$q"
  )" || return $?

  READLINE_LINE="$out"
  READLINE_POINT=${#READLINE_LINE}
  unset HEY_INSERT
}

# Insert pending suggestion at the start of the next prompt when set via argv form.
_hey_prompt_insert() {
  if [[ -n "${HEY_INSERT:-}" && -z "${READLINE_LINE:-}" ]]; then
    # Cannot set READLINE_LINE from PROMPT_COMMAND reliably on all bash versions;
    # history -s already staged the command for Up.
    :
  fi
}

case ";${PROMPT_COMMAND:-};" in
  *"_hey_prompt_insert"*) ;;
  *) PROMPT_COMMAND="_hey_prompt_insert${PROMPT_COMMAND:+; $PROMPT_COMMAND}" ;;
esac

bind -x '"\C-g": _hey_readline_fill'
