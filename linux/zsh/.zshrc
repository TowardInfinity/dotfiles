# ═══════════════════════════════════════════════════════════════
# ~/.zshrc — Linux (Ubuntu servers: a1, v1, v2)
#
# The macOS counterpart is ../../macos/zsh/.zshrc. They are deliberately
# separate rather than one file with OS branches: almost nothing overlaps
# (Homebrew vs apt, launchctl vs systemd, pbcopy vs OSC 52) and the branching
# would outweigh the shared lines.
# ═══════════════════════════════════════════════════════════════

# Fall back to a known terminfo if the client's TERM isn't installed here
# (e.g. Ghostty's xterm-ghostty) — prevents broken backspace/arrow keys.
# Ghostty's ssh-terminfo feature usually installs it, but this covers the
# case where you connect from somewhere else.
if ! infocmp "$TERM" >/dev/null 2>&1; then
  export TERM=xterm-256color
fi

# ──── Oh My Zsh ───────────────────────────────────────────────
export ZSH="$HOME/.oh-my-zsh"
ZSH_THEME="robbyrussell"
plugins=(git sudo colored-man-pages command-not-found zsh-autosuggestions zsh-syntax-highlighting)
# Everything above this line configures Oh My Zsh, but a missing install should
# degrade to a usable shell rather than erroring on every prompt — the aliases,
# PATH and history settings below are still worth having on their own.
if [[ -r "$ZSH/oh-my-zsh.sh" ]]; then
  source "$ZSH/oh-my-zsh.sh"
else
  print -u2 "zshrc: Oh My Zsh not installed — re-run: sh -c \"\$(curl -fsSL https://toin.in/install)\""
fi

# ──── History ─────────────────────────────────────────────────
HISTSIZE=50000
SAVEHIST=50000
setopt SHARE_HISTORY
setopt HIST_IGNORE_DUPS
setopt HIST_IGNORE_SPACE
setopt HIST_VERIFY

# ──── Environment ─────────────────────────────────────────────
export LANG=en_US.UTF-8
export EDITOR='nvim'

# ──── Aliases ─────────────────────────────────────────────────
alias ll='ls -lahF'
alias la='ls -A'
alias ..='cd ..'
alias ...='cd ../..'
alias df='df -h'
alias free='free -h'
alias grep='grep --color=auto'
alias ports='sudo ss -tlnp'
alias myip='curl -s ifconfig.me; echo'
alias reload='exec zsh'
alias update='sudo apt-get update && sudo apt-get upgrade -y'
alias shortcuts="grep -E '^alias ' ~/.zshrc | sed 's/^alias //' | sort"

# ──── tmux ────────────────────────────────────────────────────
# Prefix here is Ctrl-b (stock), NOT the Mac's Ctrl-a — that is what makes
# nesting work: Ctrl-a acts on the Mac, Ctrl-b passes through to this box.
alias t='tmux'
alias tl='tmux list-sessions'
alias ta='tmux attach -t'
alias tk='tmux kill-session -t'
alias tmuxconfig='${EDITOR:-vim} ~/.config/tmux/tmux.conf'

# tm [name] — attach to a session, creating it if absent.
tm() {
  local name="$1"
  if [[ -z "$name" ]]; then
    tmux attach 2>/dev/null || tmux new-session -s main
  else
    tmux attach -t "$name" 2>/dev/null || tmux new-session -s "$name"
  fi
}

# tp — session named after the current directory, rooted here.
tp() {
  local name="${PWD:t:gs/./_}"
  tmux new-session -A -s "$name" -c "$PWD"
}

# fixterm — rescue a terminal left in a weird state (stray mouse reporting,
# alt screen, hidden cursor) after a process died badly.
fixterm() {
  printf '\e[?1000l\e[?1002l\e[?1003l\e[?1006l\e[?1015l'  # mouse tracking off
  printf '\e[?2004l'                                      # bracketed paste off
  printf '\e[?1049l'                                      # leave alt screen
  printf '\e[?7h\e[?25h\e[0m'                             # wrap/cursor/attrs
  stty sane 2>/dev/null
  print -r -- "terminal reset"
}

# ──── Toolchain ───────────────────────────────────────────────
# uv
[[ -f "$HOME/.local/bin/env" ]] && . "$HOME/.local/bin/env"

# fnm — Node version manager (auto-switches on .nvmrc/.node-version)
[[ -d "$HOME/.local/share/fnm" ]] && eval "$("$HOME/.local/share/fnm/fnm" env --use-on-cd --shell=zsh)"

# Go
export GOPATH="$HOME/go"
export PATH="$PATH:/usr/local/go/bin:$GOPATH/bin"

# pnpm
export PNPM_HOME="$HOME/.local/share/pnpm"
case ":$PATH:" in
*":$PNPM_HOME/bin:"*) ;;
*) export PATH="$PNPM_HOME/bin:$PATH" ;;
esac

# ──── cat: render markdown with glow, pass everything else through ────
cat() {
  if [[ $# -eq 1 && "$1" == *.md && -f "$1" ]] && command -v glow >/dev/null 2>&1; then
    glow -- "$1"
  else
    command cat "$@"
  fi
}

# ──── Host-local extras ───────────────────────────────────────
# Anything machine-specific and untracked goes in ~/.zshrc.local — that keeps
# this file identical across every Linux box. On a1 that is where the
# tmux-status banner lives.
[[ -f "$HOME/.zshrc.local" ]] && source "$HOME/.zshrc.local"

#THIS MUST BE AT THE END OF THE FILE FOR SDKMAN TO WORK!!!
export SDKMAN_DIR="$HOME/.sdkman"
[[ -s "$HOME/.sdkman/bin/sdkman-init.sh" ]] && source "$HOME/.sdkman/bin/sdkman-init.sh"
