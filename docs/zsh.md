# zsh

Forgotten what you named something? `shortcuts` lists every alias
straight from your own `.zshrc`.

## Files & navigation

| Name | Does | Where |
| --- | --- | --- |
| `..` / `...` | Up one directory / up two | both |
| `ll` | Long listing with git status, directories first (`eza`) | mac |
| `ll` | Long listing (`ls -lahF`) | linux |
| `la` | List including hidden files | both |
| `ls` | Replaced by `eza`, directories first | mac |
| `tree` | Directory tree (`eza --tree`) | mac |
| `cat` | Replaced by `bat` — syntax highlighting, no pager | mac |
| `cat` | Renders a single `.md` file through `glow`; anything else passes through unchanged | linux |

## Tmux from the shell

| Name | Does | Where |
| --- | --- | --- |
| `t` | tmux | both |
| `tl` | List sessions | both |
| `ta <name>` | Attach to a session | both |
| `tk <name>` | Kill a session | both |
| `tm [name]` | Attach to that session or create it. Bare `tm` attaches the most recent, or makes `main` | both |
| `tp` | Session named after the current directory — the fastest way into a project | both |
| `tf` | Fuzzy-pick a session from outside tmux | mac |
| `tks` | Kill the whole tmux server | mac |

## System & rescue

| Name | Does | Where |
| --- | --- | --- |
| `reload` | Reload the shell — `source` on macOS, `exec zsh` on Linux | both |
| `shortcuts` | List every alias defined in your `.zshrc` | both |
| `fixterm` | Rescue a wedged terminal — clears mouse tracking, bracketed paste, alt-screen | both |
| `tmuxconfig` | Open `tmux.conf` in your editor | both |
| `lg` | lazygit | mac |
| `ports` | Show listening ports | linux |
| `myip` | Print this machine's public IP | linux |
| `update` | apt update and upgrade | linux |
| `df` / `free` | Disk and memory, human-readable | linux |
| `notebook` | Launch JupyterLab | mac |
| `webui` | Open the local Open WebUI; `webui-start` / `-stop` / `-status` control it | mac |
| `ai-ram` | Which Ollama models are currently in RAM | mac |
