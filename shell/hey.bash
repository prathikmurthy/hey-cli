# hey-cli bash integration
# - hey() captures COMMAND/NOTE, presents an editable readline line, prints NOTE dimmed
# - PROMPT_COMMAND stages ~/.hey/pending into history + HEY_INSERT for Ctrl+G
# - Ctrl+G fills READLINE_LINE from the question (or pending insert)

export HEY_INTEGRATION=1

if ! command -v hey >/dev/null 2>&1; then
  return 0 2>/dev/null || true
fi

_hey_history_blob() {
  local n="${HEY_HISTORY_LINES:-8}"
  history "${n}" 2>/dev/null | sed 's/^[[:space:]]*[0-9]*[[:space:]]*//'
}

_hey_dim() {
  if [[ -n "${NO_COLOR:-}" ]]; then
    printf '%s\n' "$*"
  else
    printf '\033[2m%s\033[0m\n' "$*"
  fi
}

_hey_parse() {
  local out="$1"
  reply_cmd=""
  reply_note=""
  local line
  while IFS= read -r line || [[ -n "$line" ]]; do
    case "$line" in
      COMMAND:*) reply_cmd="${line#COMMAND:}" ;;
      CMD:*)     reply_cmd="${line#CMD:}" ;;
      NOTE:*)    reply_note="${line#NOTE:}" ;;
    esac
  done <<< "$out"
  reply_cmd="${reply_cmd#"${reply_cmd%%[![:space:]]*}"}"
  reply_cmd="${reply_cmd%"${reply_cmd##*[![:space:]]}"}"
  reply_note="${reply_note#"${reply_note%%[![:space:]]*}"}"
  reply_note="${reply_note%"${reply_note##*[![:space:]]}"}"
  if [[ -z "$reply_cmd" ]]; then
    reply_cmd="$(printf '%s\n' "$out" | sed -n '/./{p;q;}')"
  fi
}

# Interactive wrapper: suggest a command onto an editable readline (+ dim note).
hey() {
  if [[ $# -eq 0 ]]; then
    command hey --help
    return $?
  fi

  case "$1" in
    install|version|--help|-h|--version|-V|--print|--explain|--model)
      command hey "$@"
      return $?
      ;;
    --fill|--capture)
      command hey "$@"
      return $?
      ;;
  esac

  local out reply_cmd reply_note
  out="$(
    HEY_CAPTURE=1 HEY_INTEGRATION=1 \
    HEY_HISTORY="$(_hey_history_blob)" \
    HEY_BUFFER="${READLINE_LINE:-}" \
    HEY_SHELL=bash \
    command hey --fill -- "$@"
  )" || return $?

  _hey_parse "$out"
  if [[ -z "$reply_cmd" ]]; then
    echo "hey: empty suggestion" >&2
    return 1
  fi

  if [[ -n "$reply_note" ]]; then
    _hey_dim "$reply_note" >&2
  fi

  rm -f "${HOME}/.hey/pending" 2>/dev/null
  unset HEY_INSERT

  # Editable input line, ready to Enter (runs the line via eval after edit).
  local line
  # shellcheck disable=SC2162
  if ! read -e -i "$reply_cmd" -p "$ " line; then
    return 130
  fi
  history -s -- "$line"
  eval "$line"
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

  local out reply_cmd reply_note
  out="$(
    HEY_CAPTURE=1 HEY_INTEGRATION=1 \
    HEY_HISTORY="$(_hey_history_blob)" \
    HEY_BUFFER="$READLINE_LINE" \
    HEY_SHELL=bash \
    command hey --fill -- "$q"
  )" || return $?

  _hey_parse "$out"
  if [[ -z "$reply_cmd" ]]; then
    echo "hey: empty suggestion" >&2
    return 1
  fi
  if [[ -n "$reply_note" ]]; then
    _hey_dim "$reply_note" >&2
  fi
  READLINE_LINE="$reply_cmd"
  READLINE_POINT=${#READLINE_LINE}
  unset HEY_INSERT
  rm -f "${HOME}/.hey/pending" 2>/dev/null
}

# After bare `hey` wrote ~/.hey/pending, stage for Up / empty Ctrl+G.
_hey_prompt_insert() {
  local pending="${HOME}/.hey/pending"
  [[ -f "$pending" ]] || return 0
  local out reply_cmd reply_note
  out="$(cat "$pending" 2>/dev/null)" || return 0
  rm -f "$pending" 2>/dev/null
  _hey_parse "$out"
  [[ -n "$reply_cmd" ]] || return 0
  history -s -- "$reply_cmd"
  export HEY_INSERT="$reply_cmd"
  if [[ -n "$reply_note" ]]; then
    _hey_dim "$reply_note" >&2
  fi
  _hey_dim "(suggestion staged — press Up, or Ctrl+G on an empty line)" >&2
}

case ";${PROMPT_COMMAND:-};" in
  *"_hey_prompt_insert"*) ;;
  *) PROMPT_COMMAND="_hey_prompt_insert${PROMPT_COMMAND:+; $PROMPT_COMMAND}" ;;
esac

bind -x '"\C-g": _hey_readline_fill'
