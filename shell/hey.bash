# hey-cli bash integration
# - DEBUG trap / PROMPT_COMMAND tees the last non-interactive command's output to ~/.hey/run/out.$$
#   (skipped inside tmux/WezTerm where live scrollback is read on demand, or when HEY_CAPTURE_OUTPUT=0)
# - hey() captures COMMAND/NOTE, presents an editable readline line, prints NOTE dimmed
# - PROMPT_COMMAND stages ~/.hey/pending into history + HEY_INSERT for Ctrl+G
# - Ctrl+G fills READLINE_LINE from the question (or pending insert)

export HEY_INTEGRATION=1

_hey_bin="$(command -v hey 2>/dev/null)"
if [[ -z "$_hey_bin" ]]; then
  return 0 2>/dev/null || true
fi

_hey_run_dir="${HOME}/.hey/run"
_hey_out_file="${_hey_run_dir}/out.$$"
_hey_done_file="${_hey_run_dir}/out.$$.done"
_hey_fallback_file="${HOME}/.hey/last_output"
_hey_in_prompt=1
_hey_capturing=0
_hey_pending_cmd=""
_hey_last_cmd=""
_hey_last_exit=""

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

_hey_is_hey_cmd() {
  local cmd="$1"
  cmd="${cmd#"${cmd%%[![:space:]]*}"}"
  cmd="${cmd#command }"
  cmd="${cmd#"${cmd%%[![:space:]]*}"}"
  [[ "$cmd" == "hey" || "$cmd" == "hey "* ]]
}

_hey_is_sensitive_cmd() {
  local cmd="$1"
  local _ncm=0
  shopt -q nocasematch || { _ncm=1; shopt -s nocasematch; }
  local matched=1
  if [[ "$cmd" =~ \.env|secret|token|password|passwd|credential|api_?key|private_?key|id_rsa|id_ed25519|\.pem|\.key|\.netrc|\.npmrc|\.pypirc|\.aws/credentials|kubeconfig ]]; then
    matched=0
  fi
  (( _ncm )) && shopt -u nocasematch
  return $matched
}

_hey_should_capture() {
  local cmd="$1"

  [[ "${HEY_CAPTURE_OUTPUT:-1}" == "0" ]] && return 1
  # Inside tmux or WezTerm, hey reads live terminal scrollback directly (zero FD overhead).
  [[ -n "${TMUX:-}" || -n "${WEZTERM_PANE:-}" ]] && return 1
  # Only tee interactive terminals (unless forced by tests).
  if [[ -z "${HEY_CAPTURE_FORCE:-}" ]]; then
    [[ -t 1 && -t 2 ]] || return 1
  fi

  cmd="${cmd#"${cmd%%[![:space:]]*}"}"
  cmd="${cmd%"${cmd##*[![:space:]]}"}"
  [[ -z "$cmd" || "$cmd" == \#* ]] && return 1

  # Never tee background jobs.
  if [[ "$cmd" == *"&" && "$cmd" != *"&&" && "$cmd" != *">&"* ]]; then
    return 1
  fi

  _hey_is_sensitive_cmd "$cmd" && return 1

  # Split on whitespace to inspect the primary command word.
  local -a words
  read -r -a words <<< "$cmd"
  local i=0 first="" rest=""
  while (( i < ${#words[@]} )); do
    local w="${words[i]}"
    case "$w" in
      [A-Za-z_][A-Za-z0-9_]*=*|command|builtin|time|env)
        if [[ "$w" == "env" && $((i + 1)) -eq ${#words[@]} ]]; then
          first="env"
          break
        fi
        i=$((i + 1))
        ;;
      *)
        first="${w##*/}"
        if (( i + 1 < ${#words[@]} )); then
          rest="${words[*]:$((i + 1))}"
        fi
        break
        ;;
    esac
  done
  [[ -z "$first" ]] && return 1

  case "$first" in
    # Sensitive commands
    env|printenv|set|export|declare|typeset|pass|op|vault|gpg|gpg2|security|keychain|ssh-keygen|ssh-add|passwd|age|sops|htpasswd|base64|openssl)
      return 1
      ;;
    # hey itself, editors, pagers, TUIs, remote shells, interactive CLIs, builtins, TTY listings
    hey|vi|vim|nvim|nano|emacs|pico|micro|hx|helix|ed|less|more|most|man|info|bat|\
    ssh|mosh|slogin|telnet|tmux|screen|zellij|dtach|byobu|script|clear|reset|tput|stty|\
    top|htop|btop|atop|glances|watch|fzf|tig|lazygit|lazydocker|ranger|lf|mc|ncdu|k9s|nmtui|alsamixer|mutt|neomutt|weechat|irssi|\
    sudo|su|doas|login|zsh|bash|sh|fish|dash|csh|tcsh|claude|codex|agent|cursor-agent|\
    ipython|irb|pry|ghci|iex|psql|mysql|sqlite3|mongosh|redis-cli|\
    cd|pushd|popd|dirs|pwd|ls|ll|la|l|eza|exa|tree|fg|bg|jobs|disown|exit|logout|exec|\
    alias|unalias|unset|source|.|eval|history|fc|setopt|unsetopt|shopt|bind|bindkey|complete|compdef|hash|rehash|read|select|vared|\
    open|pbcopy|pbpaste|xclip|xsel|wl-copy|wl-paste)
      return 1
      ;;
    # Standalone REPLs when invoked with no script arguments
    python|python3|node|ruby|lua|R)
      [[ -z "$rest" ]] && return 1
      ;;
    # Interactive git subcommands (pagers/editors); keep git pull/push/merge/checkout/status/stash/etc.
    git)
      local sub="${words[$((i + 1))]:-}"
      case "$sub" in
        diff|log|show|blame|reflog|mergetool|difftool|citool|gui)
          return 1
          ;;
        commit)
          case " $rest " in
            *" -m "*|*" --message"*|*" -F "*|*" --file"*|*" --no-edit"*) ;;
            *) return 1 ;;
          esac
          ;;
        rebase)
          case " $rest " in
            *" -i "*|*" --interactive"*) return 1 ;;
          esac
          ;;
        add|checkout|reset|stash)
          case " $rest " in
            *" -p "*|*" --patch"*|*" -i "*|*" --interactive"*) return 1 ;;
          esac
          ;;
      esac
      ;;
  esac

  return 0
}

_hey_debug_trap() {
  [[ "${_hey_in_prompt:-1}" -eq 1 ]] && return 0
  [[ "${_hey_capturing:-0}" -eq 1 ]] && return 0
  [[ -n "${COMP_LINE:-}" ]] && return 0
  [[ "${BASH_SUBSHELL:-0}" -gt 0 ]] && return 0
  [[ "${#FUNCNAME[@]}" -gt 1 ]] && return 0

  local cmd="${BASH_COMMAND:-}"
  [[ -z "$cmd" || "$cmd" == _hey_* ]] && return 0
  if [[ -n "${PROMPT_COMMAND:-}" && ";${PROMPT_COMMAND};" == *";${cmd};"* ]]; then
    return 0
  fi

  if _hey_is_hey_cmd "$cmd"; then
    return 0
  fi
  if _hey_is_sensitive_cmd "$cmd"; then
    return 0
  fi

  _hey_pending_cmd="$cmd"

  if _hey_should_capture "$cmd" && [[ -x "$_hey_bin" ]]; then
    [[ -d "$_hey_run_dir" ]] || mkdir -p -m 700 "$_hey_run_dir" 2>/dev/null
    rm -f "$_hey_done_file" 2>/dev/null
    _hey_capturing=1
    exec 7>&1 8>&2
    exec > >("$_hey_bin" _tee --out "$_hey_out_file" --fallback "$_hey_fallback_file" --done "$_hey_done_file" >&7 2>&8 7>&- 8>&-) 2>&1
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

_hey_context_file() {
  if [[ -n "${_hey_out_file:-}" && -f "$_hey_out_file" ]]; then
    printf '%s' "$_hey_out_file"
  fi
}

# Interactive wrapper: suggest a command onto an editable readline (+ dim note).
hey() {
  if [[ $# -eq 0 ]]; then
    command hey --help
    return $?
  fi

  case "$1" in
    install|version|--help|-h|--version|-V|--fill|--capture)
      command hey "$@"
      return $?
      ;;
    --print|--explain|--model)
      HEY_INTEGRATION=1 \
      HEY_HISTORY="$(_hey_history_blob)" \
      HEY_BUFFER="${READLINE_LINE:-}" \
      HEY_LAST_CMD="${_hey_last_cmd:-}" \
      HEY_LAST_EXIT="${_hey_last_exit:-}" \
      HEY_LAST_OUTPUT_FILE="$(_hey_context_file)" \
      HEY_SHELL=bash \
      command hey "$@"
      return $?
      ;;
  esac

  local out reply_cmd reply_note
  out="$(
    HEY_CAPTURE=1 HEY_INTEGRATION=1 \
    HEY_HISTORY="$(_hey_history_blob)" \
    HEY_BUFFER="${READLINE_LINE:-}" \
    HEY_LAST_CMD="${_hey_last_cmd:-}" \
    HEY_LAST_EXIT="${_hey_last_exit:-}" \
    HEY_LAST_OUTPUT_FILE="$(_hey_context_file)" \
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
    HEY_LAST_CMD="${_hey_last_cmd:-}" \
    HEY_LAST_EXIT="${_hey_last_exit:-}" \
    HEY_LAST_OUTPUT_FILE="$(_hey_context_file)" \
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

# Restore FDs after captured commands, commit last command/exit, and stage ~/.hey/pending.
_hey_precmd() {
  local _hey_ec=$?
  _hey_in_prompt=1

  if [[ "${_hey_capturing:-0}" -eq 1 ]]; then
    _hey_capturing=0
    exec 1>&7 2>&8 7>&- 8>&-
    local _hey_i=0
    while [[ ! -f "$_hey_done_file" && $_hey_i -lt 6 ]]; do
      sleep 0.01 2>/dev/null || break
      _hey_i=$((_hey_i + 1))
    done
  fi

  if [[ -n "${_hey_pending_cmd:-}" ]]; then
    _hey_last_cmd="$_hey_pending_cmd"
    _hey_last_exit="$_hey_ec"
    _hey_pending_cmd=""
  fi

  _hey_prompt_insert
  return $_hey_ec
}

_hey_prompt_ready() {
  local _hey_ec=$?
  _hey_in_prompt=0
  return $_hey_ec
}

# Wire PROMPT_COMMAND (supports both string and bash 5.1+ array forms).
if [[ "$(declare -p PROMPT_COMMAND 2>/dev/null)" == "declare -a"* ]]; then
  _hey_has_precmd=0
  for _hey_pc_item in "${PROMPT_COMMAND[@]}"; do
    [[ "$_hey_pc_item" == *"_hey_precmd"* ]] && _hey_has_precmd=1
  done
  if (( _hey_has_precmd == 0 )); then
    PROMPT_COMMAND=("_hey_precmd" "${PROMPT_COMMAND[@]}" "_hey_prompt_ready")
  fi
  unset _hey_has_precmd _hey_pc_item
else
  case ";${PROMPT_COMMAND:-};" in
    *"_hey_precmd"*) ;;
    *)
      _hey_pc="${PROMPT_COMMAND:-}"
      _hey_pc="${_hey_pc#"${_hey_pc%%[![:space:];]*}"}"
      _hey_pc="${_hey_pc%"${_hey_pc##*[![:space:];]}"}"
      PROMPT_COMMAND="_hey_precmd${_hey_pc:+; $_hey_pc}; _hey_prompt_ready"
      unset _hey_pc
      ;;
  esac
fi

# Chain with any existing DEBUG trap.
_hey_prev_debug="$(trap -p DEBUG | sed -E "s/^trap -- '(.*)' DEBUG$/\1/; s/'\\\\''/'/g")"
case "$_hey_prev_debug" in
  *"_hey_debug_trap"*) ;;
  "") trap '_hey_debug_trap' DEBUG ;;
  *)  trap "${_hey_prev_debug}; _hey_debug_trap" DEBUG ;;
esac
unset _hey_prev_debug

bind -x '"\C-g": _hey_readline_fill' 2>/dev/null || true
