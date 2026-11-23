# hey-cli zsh integration
# - hey() captures COMMAND/NOTE, fills the next ZLE buffer (print -z), prints NOTE dimmed
# - precmd picks up ~/.hey/pending when bare `hey` (binary) was used without the wrapper
# - Ctrl+G treats the current BUFFER as the question

export HEY_INTEGRATION=1

if ! command -v hey >/dev/null 2>&1; then
  return 0 2>/dev/null || true
fi

_hey_history_blob() {
  local n="${HEY_HISTORY_LINES:-8}"
  fc -ln -"${n}" 2>/dev/null | sed 's/^[[:space:]]*//'
}

_hey_dim() {
  if [[ -n "${NO_COLOR:-}" ]]; then
    print -r -- "$*"
  else
    print -r -- $'\033[2m'"$*"$'\033[0m'
  fi
}

_hey_parse() {
  # Sets reply_cmd / reply_note from capture stdout.
  local out="$1"
  reply_cmd=""
  reply_note=""
  local line
  while IFS= read -r line; do
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
  # Fallback: first non-empty line
  if [[ -z "$reply_cmd" ]]; then
    reply_cmd="$(print -r -- "$out" | sed -n '/./{p;q;}')"
  fi
}

_hey_apply_fill() {
  local cmd="$1" note="$2"
  if [[ -z "$cmd" ]]; then
    print -u2 "hey: empty suggestion"
    return 1
  fi
  if [[ -n "$note" ]]; then
    _hey_dim "$note" >&2
  fi
  # Push onto editing buffer so the user can edit / Enter on the next prompt.
  print -z -- "$cmd"
  rm -f "${HOME}/.hey/pending" 2>/dev/null
  return 0
}

# Interactive wrapper: ask the model, then insert the command into the ZLE buffer stack.
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
    HEY_BUFFER="${BUFFER:-}" \
    HEY_SHELL=zsh \
    command hey --fill -- "$@"
  )" || return $?

  _hey_parse "$out"
  _hey_apply_fill "$reply_cmd" "$reply_note"
}

# After bare `hey` (binary) wrote ~/.hey/pending, fill the next prompt.
_hey_precmd() {
  local pending="${HOME}/.hey/pending"
  [[ -f "$pending" ]] || return 0
  local out reply_cmd reply_note
  out="$(cat "$pending" 2>/dev/null)" || return 0
  rm -f "$pending" 2>/dev/null
  _hey_parse "$out"
  [[ -n "$reply_cmd" ]] || return 0
  if [[ -n "$reply_note" ]]; then
    _hey_dim "$reply_note" >&2
  fi
  print -z -- "$reply_cmd"
}

autoload -Uz add-zsh-hook 2>/dev/null || true
if typeset -f add-zsh-hook >/dev/null 2>&1; then
  add-zsh-hook precmd _hey_precmd
else
  # Fallback: append once
  case ";${precmd_functions[*]};" in
    *"_hey_precmd"*) ;;
    *) precmd_functions+=(_hey_precmd) ;;
  esac
fi

# Ctrl+G: treat current BUFFER as the question (or prompt if empty).
_hey_zle_fill() {
  local q="${BUFFER}"
  if [[ -z "$q" ]]; then
    zle -I
    print -u2 "hey: type a question on the line, then press Ctrl+G"
    return 1
  fi
  if [[ "$q" == hey\ * ]]; then
    q="${q#hey }"
  fi

  local out reply_cmd reply_note
  out="$(
    HEY_CAPTURE=1 HEY_INTEGRATION=1 \
    HEY_HISTORY="$(_hey_history_blob)" \
    HEY_BUFFER="$BUFFER" \
    HEY_SHELL=zsh \
    command hey --fill -- "$q"
  )" || return $?

  _hey_parse "$out"
  if [[ -z "$reply_cmd" ]]; then
    print -u2 "hey: empty suggestion"
    return 1
  fi
  if [[ -n "$reply_note" ]]; then
    zle -I
    _hey_dim "$reply_note" >&2
  fi
  BUFFER="$reply_cmd"
  CURSOR=${#BUFFER}
  rm -f "${HOME}/.hey/pending" 2>/dev/null
  zle redisplay
}

zle -N _hey_zle_fill
bindkey '^G' _hey_zle_fill
