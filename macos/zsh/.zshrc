# ═══════════════════════════════════════════════════════════════
# ~/.zshrc — Oh My Zsh configuration
# ═══════════════════════════════════════════════════════════════

# Path to your Oh My Zsh installation.
export ZSH="$HOME/.oh-my-zsh"

# ──── Theme ───────────────────────────────────────────────────
ZSH_THEME="robbyrussell"

# ──── Options ─────────────────────────────────────────────────
# Show dots while waiting for completion
COMPLETION_WAITING_DOTS="true"

# Timestamp format in history output
HIST_STAMPS="yyyy-mm-dd"

# ──── Plugins ─────────────────────────────────────────────────
# Add wisely — too many slow startup.
plugins=(git macos pip)

# Docker CLI completions — moved before OMZ source so compinit picks it up
fpath=($HOME/.docker/completions $fpath)

# Everything above this line configures Oh My Zsh, but a missing install should
# degrade to a usable shell rather than erroring on every prompt — the aliases,
# PATH and history settings below are still worth having on their own.
if [[ -r "$ZSH/oh-my-zsh.sh" ]]; then
  source "$ZSH/oh-my-zsh.sh"
else
  print -u2 "zshrc: Oh My Zsh not installed — re-run: sh -c \"\$(curl -fsSL https://toin.in/install)\""
fi

# ──── History ────────────────────────────────────────────────
HISTSIZE=10000
SAVEHIST=10000
setopt HIST_IGNORE_DUPS       # Don't save duplicate commands
setopt HIST_IGNORE_SPACE      # Don't save commands prefixed with a space
setopt SHARE_HISTORY          # Share history across terminal sessions
setopt HIST_VERIFY            # Preview history expansion before running

# ──── Environment ────────────────────────────────────────────
export LANG=en_US.UTF-8
export EDITOR='vim'

# ──── PATH (consolidated) ────────────────────────────────────
# Ties the `path` array to PATH and makes it unique, so zsh drops duplicates as
# they are added — including from any line added below in future. Without this,
# `reload` (which is `source ~/.zshrc`, re-running every export in the same
# process) grew each entry once per invocation. Fixing the mechanism rather
# than guarding five individual lines.
typeset -U path PATH

export PATH="$HOME/.antigravity/antigravity/bin:$PATH"
export PATH="$PATH:$HOME/.lmstudio/bin"
export PATH="$HOME/.local/bin:$PATH"

# pnpm's global bin dir — `pnpm add -g` installs here, and silently succeeds
# even when this is missing from PATH; nothing it puts there runs until it's
# on here too. See docs/gotchas-setup.md. Deliberately not PNPM_HOME: pnpm
# 11 resolves the global bin dir as "$PNPM_HOME/bin" whenever PNPM_HOME is
# set, which would point one level too deep — the plain PATH entry is what
# actually matches where `pnpm add -g` puts things.
export PATH="$HOME/Library/pnpm/bin:$PATH"

# ──── Aliases ────────────────────────────────────────────────
# Python
alias python='python3'
alias pip='pip3'

# Jupyter
alias notebook='jupyter lab'

# Navigation
alias ..='cd ..'
alias ...='cd ../..'
alias ll='ls -lAh'
alias la='ls -A'

# Shell convenience
alias reload='source ~/.zshrc'
alias zshconfig='open ~/.zshrc'

# ──── conda ──────────────────────────────────────────────────
# Legacy and Jupyter only — uv is the default for Python work here.
#
# Rewritten from the block `conda init` generates. That version hardcoded one
# Homebrew path and, when conda was not found there, fell through to
# unconditionally prepending that non-existent directory to PATH — the only
# unguarded optional tool in this file, silently adding a dead entry on every
# shell start on any machine without miniconda at exactly that location.
#
# Now: look in the usual places, do nothing at all if conda is not installed.
# Set CONDA_HOME in the environment to point somewhere else. Deliberately no
# prompting and no mkdir: this file runs on every shell start, so it must stay
# non-interactive and free of side effects — and an empty directory would not
# make conda work anyway. If you want conda installed, that belongs in
# bootstrap.sh --deps.
for _conda_home in "${CONDA_HOME:-}" \
                   /opt/homebrew/Caskroom/miniconda/base \
                   "$HOME/miniconda3" "$HOME/miniforge3"; do
  [[ -n "$_conda_home" && -x "$_conda_home/bin/conda" ]] || continue
  if __conda_setup="$("$_conda_home/bin/conda" shell.zsh hook 2>/dev/null)"; then
    eval "$__conda_setup"
  elif [[ -f "$_conda_home/etc/profile.d/conda.sh" ]]; then
    . "$_conda_home/etc/profile.d/conda.sh"
  fi
  unset __conda_setup
  break
done
unset _conda_home

# ──── Tool integrations ──────────────────────────────────────
# iTerm2 shell integration
test -e "${HOME}/.iterm2_shell_integration.zsh" && source "${HOME}/.iterm2_shell_integration.zsh"

# opencode
export PATH=$HOME/.opencode/bin:$PATH

# fnm — Node version manager (auto-switches on cd into dirs with .nvmrc/.node-version)
# Guarded: without the check, a machine that has the dotfiles but not yet the
# tools prints "command not found: fnm" on every single shell start.
command -v fnm >/dev/null && eval "$(fnm env --use-on-cd --shell zsh)"

# ──── CLI power pack (added 2026-07-19) ──────────────────────
command -v fzf    >/dev/null && eval "$(fzf --zsh)"       # Ctrl-R history, Ctrl-T files
command -v zoxide >/dev/null && eval "$(zoxide init zsh)" # z <dir> — smart cd
alias ls='eza --group-directories-first'
alias ll='eza -lAh --git --group-directories-first'
alias la='eza -A'
alias tree='eza --tree'
alias cat='bat --paging=never'
alias lg='lazygit'

# ──── Local AI (added 2026-07-19) ────────────────────────────
# Open WebUI (chat UI over Ollama) — http://localhost:11435
alias webui-start='launchctl load ~/Library/LaunchAgents/com.towardinfinity.open-webui.plist && echo "Open WebUI starting → http://localhost:11435"'
alias webui-stop='launchctl unload ~/Library/LaunchAgents/com.towardinfinity.open-webui.plist && echo "Open WebUI stopped"'
alias webui-status='lsof -nP -iTCP:11435 -sTCP:LISTEN >/dev/null 2>&1 && echo "Open WebUI: running → http://localhost:11435" || echo "Open WebUI: stopped"'
alias webui='open http://localhost:11435'
alias ai-ram='ollama ps'             # which models are loaded in RAM right now
alias shortcuts="grep -E '^alias ' ~/.zshrc | sed 's/^alias //' | sort"   # list my own shortcuts

# ──── Claude Code (added 2026-08-05) ─────────────────────────
# Auto-approve mode is machine-local on purpose. ~/.claude/settings.json is
# symlinked from the dotfiles repo and shared with a1, which is internet-facing
# — auto-approving tool calls there is not something to inherit by accident.
# So the Mac opts in here rather than via "defaultMode" in the shared file.
alias claude='claude --permission-mode auto'

# ──── Codex: tell tmux which model this pane is on (added 2026-08-08) ────
# Claude Code has a statusLine hook that reports its own model on every refresh
# (common/claude/statusline.sh). Codex has no equivalent — its only hook, the
# `notify` key, is already taken by the Computer Use app — so the pane marker
# has to be written from the shell instead.
#
# The marker holds the launch model; tmux-model then refreshes it from the
# session rollout, which records model and effort on every turn, so a
# mid-session /model still shows up. `-p sol` / `-m <model>` are read here
# because the rollout does not exist yet at launch and the first seconds of an
# escalated session are exactly when you want to see that it is escalated.
codex() {
  local mark="" model="" i
  if [[ -n $TMUX_PANE ]]; then
    local dir="${DOTS_PANE_DIR:-$HOME/.cache/dots/panes}"
    mkdir -p "$dir" 2>/dev/null && mark="$dir/${TMUX_PANE#\%}"
  fi
  if [[ -n $mark ]]; then
    for (( i = 1; i <= $#; i++ )); do
      case ${@[i]} in
        -m|--model)   model=${@[i+1]} ;;
        -p|--profile) model=$(sed -n 's/^model *= *"\([^"]*\)".*/\1/p' \
                                "${CODEX_HOME:-$HOME/.codex}/${@[i+1]}.config.toml" \
                                2>/dev/null | head -1) ;;
      esac
    done
    [[ -n $model ]] || model=$(sed -n 's/^model *= *"\([^"]*\)".*/\1/p' \
                                 "${CODEX_HOME:-$HOME/.codex}/config.toml" 2>/dev/null | head -1)
    printf 'codex\t%s\t%s\n' "$$" "$model" > "$mark" 2>/dev/null
  fi
  command codex "$@"
  local rc=$?
  [[ -n $mark ]] && rm -f "$mark" 2>/dev/null
  return $rc
}

# ──── tmux (added 2026-07-26) ────────────────────────────────
# Config lives at ~/.config/tmux/tmux.conf. Prefix is Ctrl-a.
alias t='tmux'
alias tl='tmux list-sessions'
alias ta='tmux attach -t'
alias tk='tmux kill-session -t'
alias tks='tmux kill-server'
alias tmuxconfig='${EDITOR:-vim} ~/.config/tmux/tmux.conf'

# tm [name] — attach to a session, creating it if absent.
# With no argument: attach to the most recent session, or create "main".
tm() {
  local name="$1"
  if [[ -z "$name" ]]; then
    tmux attach 2>/dev/null || tmux new-session -s main
  else
    tmux attach -t "$name" 2>/dev/null || tmux new-session -s "$name"
  fi
}

# tp — session named after the current directory, rooted here. Great per-project.
tp() {
  local name="${PWD:t:gs/./_}"
  tmux new-session -A -s "$name" -c "$PWD"
}

# tf — fuzzy-pick a session from outside tmux
tf() {
  local s
  s=$(tmux list-sessions -F '#S' 2>/dev/null | fzf --reverse --prompt='session > ') || return
  [[ -n "$s" ]] && { [[ -n "$TMUX" ]] && tmux switch-client -t "$s" || tmux attach -t "$s"; }
}

# ──── Terminal hygiene: stray mouse-reporting guard (added 2026-07-26) ────
# When an SSH session dies uncleanly (timeout / broken pipe), the remote tmux
# never gets to send its "disable mouse reporting" sequence, so the local
# terminal stays in mouse-tracking mode. Every mouse move then arrives as fake
# keyboard input like \e[<35;91;1M, and zsh tries to run the digits:
#   zsh: command not found: 35
# Emitting the disable sequence before each prompt makes that state
# unreachable. It is invisible, and harmless inside tmux — it only tells tmux
# the shell wants no mouse events, so tmux keeps handling them itself and
# pane-click/scroll still work.
_reset_mouse_reporting() {
  [[ -t 1 ]] && printf '\e[?1000l\e[?1002l\e[?1003l\e[?1006l\e[?1015l'
}
autoload -Uz add-zsh-hook
add-zsh-hook precmd _reset_mouse_reporting

# fixterm — manual rescue if a terminal is ever left in a weird state
fixterm() {
  printf '\e[?1000l\e[?1002l\e[?1003l\e[?1006l\e[?1015l'  # mouse tracking off
  printf '\e[?2004l'                                      # bracketed paste off
  printf '\e[?1049l'                                      # leave alt screen
  printf '\e[?7h\e[?25h\e[0m'                             # wrap on, cursor on, attrs reset
  stty sane 2>/dev/null
  print -r -- "terminal reset"
}

# Uncomment to auto-attach whenever you open a Ghostty window:
# if [[ -z "$TMUX" && "$TERM_PROGRAM" == "ghostty" && -z "$SSH_CONNECTION" ]]; then
#   tmux attach 2>/dev/null || tmux new-session -s main
# fi

# Anything machine-specific and untracked goes in ~/.zshrc.local — that keeps
# this file byte-identical across every Mac. The Linux .zshrc has always had
# this hook; the macOS one did not, so per-machine tweaks had nowhere to go but
# the tracked file. Sourced last, so it can override anything above it.
[[ -f "$HOME/.zshrc.local" ]] && source "$HOME/.zshrc.local"

#THIS MUST BE AT THE END OF THE FILE FOR SDKMAN TO WORK!!!
export SDKMAN_DIR="$HOME/.sdkman"
[[ -s "$HOME/.sdkman/bin/sdkman-init.sh" ]] && source "$HOME/.sdkman/bin/sdkman-init.sh"

# >>> grok installer >>>
export PATH="$HOME/.grok/bin:$PATH"
fpath=(~/.grok/completions/zsh $fpath)
autoload -Uz compinit && compinit -C
# <<< grok installer <<<

# bun completions
[ -s "/Users/towardinfinity/.bun/_bun" ] && source "/Users/towardinfinity/.bun/_bun"

# Added by cua-driver-rs installer — see https://github.com/trycua/cua
export PATH="/Users/towardinfinity/.local/bin:$PATH"
