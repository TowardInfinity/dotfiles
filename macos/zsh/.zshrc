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

source $ZSH/oh-my-zsh.sh

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
export PATH="$HOME/.antigravity/antigravity/bin:$PATH"
export PATH="$PATH:$HOME/.lmstudio/bin"
export PATH="$HOME/.local/bin:$PATH"

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

# ──── conda initialize ───────────────────────────────────────
# !! Contents managed by 'conda init' — do not edit manually !!
__conda_setup="$('/opt/homebrew/Caskroom/miniconda/base/bin/conda' 'shell.zsh' 'hook' 2> /dev/null)"
if [ $? -eq 0 ]; then
    eval "$__conda_setup"
else
    if [ -f "/opt/homebrew/Caskroom/miniconda/base/etc/profile.d/conda.sh" ]; then
        . "/opt/homebrew/Caskroom/miniconda/base/etc/profile.d/conda.sh"
    else
        export PATH="/opt/homebrew/Caskroom/miniconda/base/bin:$PATH"
    fi
fi
unset __conda_setup

# ──── Tool integrations ──────────────────────────────────────
# iTerm2 shell integration
test -e "${HOME}/.iterm2_shell_integration.zsh" && source "${HOME}/.iterm2_shell_integration.zsh"

# opencode
export PATH=$HOME/.opencode/bin:$PATH

# fnm — Node version manager (auto-switches on cd into dirs with .nvmrc/.node-version)
eval "$(fnm env --use-on-cd --shell zsh)"

# ──── CLI power pack (added 2026-07-19) ──────────────────────
eval "$(fzf --zsh)"          # Ctrl-R fuzzy history, Ctrl-T file picker
eval "$(zoxide init zsh)"    # z <dir> — smart cd
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

#THIS MUST BE AT THE END OF THE FILE FOR SDKMAN TO WORK!!!
export SDKMAN_DIR="$HOME/.sdkman"
[[ -s "$HOME/.sdkman/bin/sdkman-init.sh" ]] && source "$HOME/.sdkman/bin/sdkman-init.sh"
