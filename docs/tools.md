# Tools

Installed by `--deps`. The list is driven by what the configs actually
call — if `.zshrc` references a tool, it is here.

| Tool | What it is for | Where |
| --- | --- | --- |
| `neovim` | The editor. On Ubuntu it comes from the upstream tarball — apt's is too old | both |
| `tmux` | Terminal multiplexer | both |
| `ghostty` | The terminal itself — servers are headless and need none | mac |
| JetBrainsMono NF | Nerd Font — supplies the status bar glyphs. Only needed where you *look* | mac |
| `ripgrep` | Fast search, used by `rg` and by Telescope | both |
| `fzf` | Fuzzy finder — history search, file search, tmux pickers | mac |
| `zoxide` | Smart `cd` that learns the directories you use | mac |
| `eza` | Backs `ls`, `ll`, `la`, `tree` | mac |
| `bat` | Backs `cat` | mac |
| `lazygit` | Git UI — `lg`, and the tmux `g` popup | mac |
| `glow` | Markdown renderer — backs Linux's `cat` | both |
| `jq` | JSON on the command line | both |
| `fd` | Friendlier `find` | mac |
| `btop` | Process and resource monitor | mac |
| `uv` | Python packages and virtualenvs — the default for Python work here | both |
| `jupyterlab` | Notebooks, installed via `uv tool` so each project registers its own kernel | both |
| `fnm` | Node version manager — switches automatically on `.nvmrc` | both |
| `pnpm` | Node packages — preferred over npm | both |
| `go` | Go toolchain | both |
| `gh` | GitHub CLI — convenience only now the repo is public | both |
| Oh My Zsh | Installed on every run, not just `--deps` | both |
| TPM | tmux plugin manager. `prefix I` installs plugins | both |

Java is left to SDKMAN, which `.zshrc` sources if present. Version
managers rather than languages, so versions stay per-project.
