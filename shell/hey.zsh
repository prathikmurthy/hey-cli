# hey-cli zsh integration
# - preexec/precmd tees the last non-interactive command's output (tail) to ~/.hey/run/out.$$
#   (skipped inside tmux/WezTerm where live scrollback is read on demand, or when HEY_CAPTURE_OUTPUT=0)
# - hey() captures COMMAND/NOTE, fills the next ZLE buffer (print -z), prints NOTE dimmed
# - precmd picks up ~/.hey/pending when bare `hey` (binary) was used without the wrapper
# - Ctrl+G treats the current BUFFER as the question

export HEY_INTEGRATION=1

_hey_bin="$(command -v hey 2>/dev/null)"
if [[ -z "$_hey_bin" ]]; then
  return 0 2>/dev/null || true
fi

zmodload -F zsh/zselect b:zselect 2>/dev/null || true

_hey_run_dir="${HOME}/.hey/run"
_hey_out_file="${_hey_run_dir}/out.$$"
_hey_done_file="${_hey_run_dir}/out.$$.done"
_hey_fallback_file="${HOME}/.hey/last_output"
_hey_capturing=0
_hey_pending_cmd=""
_hey_last_cmd=""
_hey_last_exit=""

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

_hey_is_hey_cmd() {
  emulate -L zsh
  local cmd="$1"
  cmd="${cmd#"${cmd%%[![:space:]]*}"}"
  cmd="${cmd#command }"
  cmd="${cmd#"${cmd%%[![:space:]]*}"}"
  [[ "$cmd" == "hey" || "$cmd" == "hey "* ]]
}

_hey_is_sensitive_cmd() {
  emulate -L zsh
  local lower="${1:l}"
  case "$lower" in
    *.env*|*secret*|*token*|*password*|*passwd*|*credential*|*api_key*|*apikey*|*private_key*|*id_rsa*|*id_ed25519*|*.pem*|*.key*|*.netrc*|*.npmrc*|*.pypirc*|*.aws/credentials*|*kubeconfig*)
      return 0
      ;;
  esac
  return 1
}

_hey_should_capture() {
  emulate -L zsh
  local cmd="$1"

  [[ "${HEY_CAPTURE_OUTPUT:-1}" == "0" ]] && return 1
  # Inside tmux or WezTerm, hey reads live terminal scrollback directly (zero FD overhead).
  [[ -n "${TMUX:-}" || -n "${WEZTERM_PANE:-}" ]] && return 1
  # Only tee interactive terminals (unless forced by tests).
  if [[ -z "${HEY_CAPTURE_FORCE:-}" ]]; then
    [[ -t 1 && -t 2 ]] || return 1
  fi

  # Trim leading/trailing whitespace.
  cmd="${cmd#"${cmd%%[![:space:]]*}"}"
  cmd="${cmd%"${cmd##*[![:space:]]}"}"
  [[ -z "$cmd" || "$cmd" == \#* ]] && return 1

  # Never tee background jobs (avoid inherited pipe FDs).
  if [[ ("$cmd" == *"&" && "$cmd" != *"&&" && "$cmd" != *">&") || "$cmd" == *"&!" || "$cmd" == *"&|" ]]; then
    return 1
  fi

  _hey_is_sensitive_cmd "$cmd" && return 1

  # Extract the primary command word, skipping leading VAR=val assignments and wrappers.
  local -a words
  words=(${=cmd})
  local i=1 first="" rest=""
  while (( i <= ${#words} )); do
    local w="${words[i]}"
    case "$w" in
      [A-Za-z_][A-Za-z0-9_]*=*|noglob|nocorrect|command|builtin|time|env)
        if [[ "$w" == "env" && $i -eq ${#words} ]]; then
          first="env"
          break
        fi
        (( i++ ))
        ;;
      *)
        first="${w:t}"
        if (( i < ${#words} )); then
          rest="${words[i+1,-1]}"
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
      local sub="${words[i+1]:-}"
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

_hey_preexec() {
  local cmd="$1"
  if _hey_is_hey_cmd "$cmd"; then
    return 0
  fi
  if _hey_is_sensitive_cmd "$cmd"; then
    return 0
  fi

  _hey_pending_cmd="$cmd"

  if _hey_should_capture "$cmd" && [[ -x "$_hey_bin" ]]; then
    setopt local_options no_monitor no_notify
    [[ -d "$_hey_run_dir" ]] || mkdir -p -m 700 "$_hey_run_dir" 2>/dev/null
    rm -f "$_hey_done_file" 2>/dev/null
    _hey_capturing=1
    exec 7>&1 8>&2
    exec > >("$_hey_bin" _tee --out "$_hey_out_file" --fallback "$_hey_fallback_file" --done "$_hey_done_file" >&7 2>&8 7>&- 8>&-) 2>&1
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

_hey_context_file() {
  if [[ -n "${_hey_out_file:-}" && -f "$_hey_out_file" ]]; then
    print -r -- "$_hey_out_file"
  fi
}

# Interactive wrapper: ask the model, then insert the command into the ZLE buffer stack.
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
      HEY_BUFFER="${BUFFER:-}" \
      HEY_LAST_CMD="${_hey_last_cmd:-}" \
      HEY_LAST_EXIT="${_hey_last_exit:-}" \
      HEY_LAST_OUTPUT_FILE="$(_hey_context_file)" \
      HEY_SHELL=zsh \
      command hey "$@"
      return $?
      ;;
  esac

  local out reply_cmd reply_note
  out="$(
    HEY_CAPTURE=1 HEY_INTEGRATION=1 \
    HEY_HISTORY="$(_hey_history_blob)" \
    HEY_BUFFER="${BUFFER:-}" \
    HEY_LAST_CMD="${_hey_last_cmd:-}" \
    HEY_LAST_EXIT="${_hey_last_exit:-}" \
    HEY_LAST_OUTPUT_FILE="$(_hey_context_file)" \
    HEY_SHELL=zsh \
    command hey --fill -- "$@"
  )" || return $?

  _hey_parse "$out"
  _hey_apply_fill "$reply_cmd" "$reply_note"
}

# Restore FDs after captured commands, commit last command/exit, and pick up ~/.hey/pending.
_hey_precmd() {
  local _hey_ec=$?

  if [[ "${_hey_capturing:-0}" -eq 1 ]]; then
    _hey_capturing=0
    exec 1>&7 2>&8 7>&- 8>&-
    local _hey_i=0
    while [[ ! -f "$_hey_done_file" && $_hey_i -lt 5 ]]; do
      if (( $+builtins[zselect] )); then
        zselect -t 1 2>/dev/null || break
      else
        sleep 0.01 2>/dev/null || break
      fi
      (( _hey_i++ ))
    done
  fi

  if [[ -n "${_hey_pending_cmd:-}" ]]; then
    _hey_last_cmd="$_hey_pending_cmd"
    _hey_last_exit="$_hey_ec"
    _hey_pending_cmd=""
  fi

  local pending="${HOME}/.hey/pending"
  if [[ -f "$pending" ]]; then
    local out reply_cmd reply_note
    out="$(cat "$pending" 2>/dev/null)"
    rm -f "$pending" 2>/dev/null
    if [[ -n "$out" ]]; then
      _hey_parse "$out"
      if [[ -n "$reply_cmd" ]]; then
        if [[ -n "$reply_note" ]]; then
          _hey_dim "$reply_note" >&2
        fi
        print -z -- "$reply_cmd"
      fi
    fi
  fi

  return $_hey_ec
}

_hey_zshexit() {
  rm -f "${_hey_out_file:-}" "${_hey_done_file:-}" 2>/dev/null
}

# Run _hey_precmd FIRST (before prompt themes) and _hey_preexec LAST (after title hooks).
typeset -ga precmd_functions preexec_functions zshexit_functions
precmd_functions=(_hey_precmd ${precmd_functions:#_hey_precmd})
preexec_functions=(${preexec_functions:#_hey_preexec} _hey_preexec)
autoload -Uz add-zsh-hook 2>/dev/null || true
if typeset -f add-zsh-hook >/dev/null 2>&1; then
  add-zsh-hook zshexit _hey_zshexit
else
  zshexit_functions=(${zshexit_functions:#_hey_zshexit} _hey_zshexit)
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
    HEY_LAST_CMD="${_hey_last_cmd:-}" \
    HEY_LAST_EXIT="${_hey_last_exit:-}" \
    HEY_LAST_OUTPUT_FILE="$(_hey_context_file)" \
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
