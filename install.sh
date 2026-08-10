#!/usr/bin/env bash
#
# Symlink this repo's configs into place, picking the right set for the OS.
#
#   ./install.sh          link everything
#   ./install.sh --dry    show what would happen, change nothing
#   ./install.sh --apply  link/merge only; never fetch, build, or install plugins
#   ./install.sh --nvim   restore nvim plugins from lazy-lock.json (:Lazy restore)
#   ./install.sh --build  force `dots` to be built from source, not fetched
#
#   common/   linked everywhere      (nvim, claude — no OS-specific anything)
#   macos/    linked on Darwin only  (zsh, tmux, ghostty)
#   linux/    linked on Linux only   (zsh, tmux — no ghostty, servers are headless)
#
# Existing files are moved aside to <path>.backup.<timestamp> — nothing is ever
# overwritten silently. Re-running is safe: correct links are left alone.

set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STAMP="$(date +%Y%m%dT%H%M%S)"
# Plain `~` in a ${var/pat/repl} replacement can be tilde-expanded by some bash
# builds; escaping it as \~ printed a literal backslash ("\~/.config/nvim").
# A variable sidesteps both.
TILDE="~"
DRY=false
COPY=false
NVIM=false
BUILD=false
APPLY=false
for arg in "$@"; do
  case "$arg" in
    --dry)   DRY=true ;;
    --copy)  COPY=true ;;
    --nvim)  NVIM=true ;;
    --build) BUILD=true ;;
    --apply) APPLY=true ;;
    # Prints the header block above by *following* it — from line 2 until the
    # first line that is not a comment — rather than a hardcoded line range,
    # same trick bootstrap.sh uses so the two don't drift apart.
    -h|--help) awk 'NR>1 && /^#/ { sub(/^# ?/, ""); print; next } NR>1 { exit }' "$0"; exit 0 ;;
    *) echo "Unknown option: $arg" >&2; exit 1 ;;
  esac
done

case "$(uname -s)" in
  Darwin) OS=macos ;;
  Linux)  OS=linux ;;
  *) echo "Unsupported OS: $(uname -s)" >&2; exit 1 ;;
esac

# --copy writes real files instead of symlinks, for machines where you don't
# want the repo hanging around (containers, throwaway boxes). The trade is
# real: edits are no longer tracked, and updating means re-running this — a
# symlinked install just needs `git pull`.
VERB=linked;   $COPY && VERB=copied
ACTION=link;   $COPY && ACTION=copy

MISSING=0

# How many timestamped backups to keep per destination. Symlink mode makes at
# most one (the pre-dotfiles original), so this is really about --copy, which
# had no "already correct" shortcut and so backed up a full recursive copy of
# every config on every run — on exactly the throwaway machines that are told
# to re-run in order to update.
BACKUPS_KEPT=2

prune_backups() {
  local dst="$1" old
  # Newest first, drop everything past the keep count. stderr is discarded
  # because "no backups yet" is the normal case, not an error.
  while IFS= read -r old; do
    [[ -n "$old" ]] || continue
    rm -rf "$old"
    printf '  \033[90mpruned\033[0m   %s\n' "$(basename "$old")"
  done < <(ls -1dt "$dst".backup.* 2>/dev/null | tail -n +$((BACKUPS_KEPT + 1)))
}

# Move $dst aside, never overwriting an existing backup. $STAMP has one-second
# resolution, so two runs within the same second would otherwise collide and
# the second `mv` would clobber the first run's backup.
backup_dst() {
  local dst="$1" bk="$1.backup.$STAMP" n=1
  while [[ -e "$bk" ]]; do
    bk="$dst.backup.$STAMP-$n"
    n=$((n + 1))
  done
  mv "$dst" "$bk"
  printf '  \033[33mbacked up\033[0m %s -> %s\n' "${dst/#$HOME/$TILDE}" "$(basename "$bk")"
  prune_backups "$dst"
}

# merge_toml <src> <dst> — keep a block of shared TOML inside a file the
# application also writes to.
#
# Some configs cannot be symlinked because the tool owns them: Codex appends a
# [projects."/abs/path"] trust block every time you trust a directory. Linking
# that file would push one machine's paths to all the others and conflict on
# every pull. Leaving it unlinked meant the policy had to be copied by hand,
# which is exactly the drift this repo exists to prevent.
#
# So $src is spliced into $dst between markers. Everything outside them is the
# machine's own and is preserved; anything the policy defines is removed from
# where the machine had it, so there is one definition, not two. Re-running is
# idempotent: the previous managed block is stripped before the new one goes in.
#
# Order matters in TOML — bare top-level keys must precede every table header —
# so the output is: the machine's own top-level keys, then the managed block,
# then the machine's tables.
MERGE_BEGIN="# >>> dots: managed block — edit the source in the dotfiles repo >>>"
MERGE_END="# <<< dots: end managed block <<<"

merge_toml() {
  local src="$1" dst="$2"
  case "$src" in /*) : ;; *) src="$REPO/$src" ;; esac

  if [[ ! -e "$src" ]]; then
    printf '  \033[31mmissing\033[0m  %s\n' "$src"
    MISSING=$((MISSING + 1))
    return 0
  fi

  # python3 does the parse. It ships with macOS and every server image here; if
  # it is ever absent, say so rather than silently skipping the policy.
  if ! command -v python3 >/dev/null 2>&1; then
    printf '  \033[31mmissing\033[0m  python3 (needed to merge %s)\n' "${dst/#$HOME/$TILDE}"
    MISSING=$((MISSING + 1))
    return 0
  fi

  local out status
  out="$(python3 "$REPO/bin/merge-toml-block.py" \
           --src "$src" --dst "$dst" \
           --begin "$MERGE_BEGIN" --end "$MERGE_END" \
           $($DRY && echo --dry) 2>&1)" && status=0 || status=$?

  if [[ $status -ne 0 ]]; then
    printf '  \033[31mfailed\033[0m   %s\n%s\n' "${dst/#$HOME/$TILDE}" "$out"
    MISSING=$((MISSING + 1))
    return 0
  fi

  case "$out" in
    unchanged) printf '  \033[90mok\033[0m       %s (managed block)\n' "${dst/#$HOME/$TILDE}" ;;
    would-*)   printf '  \033[36mwould merge\033[0m %s (managed block)\n' "${dst/#$HOME/$TILDE}" ;;
    *)         printf '  \033[32mmerged\033[0m   %s (managed block)\n' "${dst/#$HOME/$TILDE}" ;;
  esac
}

link() {
  # $1 is usually a path relative to $REPO, but callers that already have an
  # absolute path (e.g. a resolved `dots` binary living under ~/.cache) pass
  # it straight through instead.
  local src="$1" dst="$2"
  case "$src" in
    /*) : ;;
    *) src="$REPO/$src" ;;
  esac

  # A missing source used to `return 1`, and under `set -e` a bare call to this
  # function then killed the script on the spot. One absent file left the home
  # directory half-linked — later entries never ran, TPM never installed, and
  # the closing instructions never printed, so the only clue was a single red
  # line scrolled off the top. Record it and carry on; the summary at the end
  # reports it and the exit status still reflects the failure.
  if [[ ! -e "$src" ]]; then
    printf '  \033[31mmissing\033[0m  %s\n' "$src"
    MISSING=$((MISSING + 1))
    return 0
  fi

  if ! $COPY && [[ -L "$dst" && "$(readlink "$dst")" == "$src" ]]; then
    printf '  \033[90mok\033[0m       %s\n' "${dst/#$HOME/$TILDE}"
    return 0
  fi

  # Copy mode's equivalent of the shortcut above. If what is already there is
  # byte-for-byte what we would write, it is our own previous copy: there is
  # nothing to preserve and nothing to do. This is what stops the backup pile.
  if $COPY && [[ -e "$dst" ]] && diff -rq "$src" "$dst" >/dev/null 2>&1; then
    printf '  \033[90mok\033[0m       %s\n' "${dst/#$HOME/$TILDE}"
    return 0
  fi

  if $DRY; then
    if [[ -e "$dst" || -L "$dst" ]]; then
      printf '  \033[33mwould back up + %s\033[0m  %s\n' "$ACTION" "${dst/#$HOME/$TILDE}"
    else
      printf '  \033[36mwould %s\033[0m  %s\n' "$ACTION" "${dst/#$HOME/$TILDE}"
    fi
    return 0
  fi

  mkdir -p "$(dirname "$dst")"

  if [[ -e "$dst" || -L "$dst" ]]; then
    backup_dst "$dst"
  fi

  if $COPY; then
    cp -R "$src" "$dst"
  else
    ln -s "$src" "$dst"
  fi
  printf '  \033[32m%s\033[0m   %s\n' "$VERB" "${dst/#$HOME/$TILDE}"
}

echo "dotfiles: $REPO"
echo "os:       $OS"
$DRY && echo "(dry run — nothing will change)"
echo

# ── Shared ────────────────────────────────────────────────────
link common/nvim "$HOME/.config/nvim"

# ── dots ──────────────────────────────────────────────────────
# The reference/maintenance CLI. Linked into ~/.local/bin, which both .zshrc
# files already put on PATH. It resolves back through this symlink to find the
# repo, so `dots update` works from anywhere.
#
# `dots` is a Go program (./cmd/dots), and building it cold costs ~10s and
# ~120MB of module downloads — too much to pay on every machine. bin/dots-resolve.sh
# picks the best available copy instead: a prebuilt release binary from GitHub
# Releases (verified against its checksum), falling back to a local `go build`,
# falling back to bin/dots, the bash implementation that always works. See
# bin/dots-resolve.sh for the full tier order.
#
# --build forces the source-build tier, for when the Go code itself changed
# and a stale release binary would hide it.
DOTS_CACHE_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/dots"

describe_dots_tier() {
  case "$1" in
    "$DOTS_CACHE_DIR"/dots-*)
      printf 'release binary %s\n' "${1##*/dots-}"
      ;;
    "$REPO/bin/dots-bin")
      echo "built from source"
      ;;
    *)
      echo "shell fallback"
      ;;
  esac
}

if $DRY; then
  if $APPLY; then
    printf '  \033[36mwould reuse\033[0m  installed dots binary (else shell fallback; no resolver)\n'
  elif $BUILD; then
    printf '  \033[36mwould build\033[0m  dots (forced --build)\n'
  else
    printf '  \033[36mwould resolve\033[0m  dots (release binary, else build, else shell fallback)\n'
  fi
else
  echo "Resolving dots..."
  if $APPLY; then
    # Apply is the deliberately network-free relink path. Preserve the
    # currently installed binary when it is usable; otherwise the checked-in
    # shell fallback keeps recovery available without downloading or building.
    DOTS_TARGET=""
    if [[ -L "$HOME/.local/bin/dots" ]]; then
      DOTS_TARGET="$(readlink "$HOME/.local/bin/dots")"
      case "$DOTS_TARGET" in
        /*) : ;;
        *) DOTS_TARGET="$(cd "$(dirname "$HOME/.local/bin/dots")" && pwd)/$DOTS_TARGET" ;;
      esac
      [[ -x "$DOTS_TARGET" ]] || DOTS_TARGET=""
    fi
    [[ -n "$DOTS_TARGET" ]] || DOTS_TARGET="$REPO/bin/dots"
  else
    if $BUILD; then
      export DOTS_FORCE_BUILD=1
    else
      # An install should pick up a newer release rather than reusing whatever
      # this machine happens to have cached. Costs one redirect request; the
      # binary is only re-downloaded when the version actually changed.
      export DOTS_FORCE_FETCH=1
    fi
    DOTS_TARGET="$(sh "$REPO/bin/dots-resolve.sh")" || DOTS_TARGET=""
  fi
  if [[ -z "$DOTS_TARGET" || ! -e "$DOTS_TARGET" ]]; then
    printf '  \033[33mwarning\033[0m  dots-resolve.sh produced nothing usable — linking the shell fallback\n'
    DOTS_TARGET="$REPO/bin/dots"
  fi
  printf '  \033[36mdots:\033[0m %s\n' "$(describe_dots_tier "$DOTS_TARGET")"
  link "$DOTS_TARGET" "$HOME/.local/bin/dots"

  # Record where this checkout lives, so `dots` can find it later.
  #
  # It normally finds the repo by resolving its own symlink and walking up,
  # which works when ~/.local/bin/dots points into the checkout. It does not
  # when a release binary won: that symlink resolves into ~/.cache/dots/, and
  # a repo at a non-default path (DOTFILES_DIR=...) is then unreachable —
  # `dots update`, `sync`, `path` and `install` all report "no checkout
  # found" in any shell where DOTFILES_DIR is no longer exported.
  #
  # Rewritten on every install, so moving the repo self-heals. --copy is
  # excluded on purpose: it deliberately keeps no checkout to point at.
  if ! $COPY; then
    mkdir -p "$HOME/.config/dots" 2>/dev/null || true
    printf '%s\n' "$REPO" > "$HOME/.config/dots/repo" 2>/dev/null || true
  fi
fi

# --copy keeps no repo, so `dots` has nothing to read its docs out of — its
# own parent directory becomes ~/.local. The docs have to travel with it.
# Symlink installs skip this: there the repo is right there and current.
if $COPY; then
  link docs "$HOME/.local/share/dots/docs"
fi

# ── Claude Code ───────────────────────────────────────────────
# Each box's ~/.claude/CLAUDE.md stays untracked and machine-specific (different
# toolchains, different guardrails) and pulls the shared policy in with a single
# `@~/.claude/delegation.md` line.
#
# settings.json IS tracked now: the model/effort/compaction keys in it are the
# whole point of the policy, and leaving them per-machine meant a1 quietly kept
# running Opus on everything. Claude Code writes to this file via /config, and
# those writes pass through the symlink into the repo — the same arrangement as
# common/nvim/lazy-lock.json. Anything genuinely machine-local (one-off
# permission grants) belongs in the untracked ~/.claude/settings.local.json.
link common/claude/settings.json "$HOME/.claude/settings.json"
link common/claude/delegation.md "$HOME/.claude/delegation.md"
# settings.json names this by $HOME rather than an absolute path, so the shared
# file works on every box — the Obsidian hook's baked-in /Users/... path is the
# mistake this avoids repeating.
link common/claude/statusline.sh "$HOME/.claude/statusline.sh"
link common/claude/session-start.sh "$HOME/.claude/session-start.sh"

# ── Codex CLI ─────────────────────────────────────────────────
# ~/.codex/config.toml cannot be a symlink: Codex writes machine-specific
# [projects."/abs/path"] trust blocks and [plugins.*] entries into it, so sharing
# the whole file would push local paths across machines and conflict on every
# pull. But the model policy inside it still has to travel, so it is merged in
# instead — see merge_toml() above. What is wholly safe to share is linked.
link common/codex/AGENTS.md       "$HOME/.codex/AGENTS.md"
link common/codex/sol.config.toml "$HOME/.codex/sol.config.toml"
merge_toml common/codex/config.policy.toml "$HOME/.codex/config.toml"

# ── tmux ──────────────────────────────────────────────────────
# Linked as individual files, not the whole directory, so TPM plugins and
# resurrect snapshots keep living in ~/.config/tmux untracked.
link "$OS/tmux/tmux.conf" "$HOME/.config/tmux/tmux.conf"
# The status bar's model segment. On PATH rather than referenced into the repo:
# tmux status jobs run under /bin/sh with the tmux server's environment, which
# is whatever it was when the server started — not necessarily one that can find
# the checkout.
link common/tmux/model.sh "$HOME/.local/bin/tmux-model"
# Recolours a pane while Claude Code is running in it. Same PATH reasoning as
# tmux-model above, and it piggybacks the same status-interval tick.
link common/tmux/pane-theme.sh "$HOME/.local/bin/tmux-pane-theme"

# ── Shell ─────────────────────────────────────────────────────
link "$OS/zsh/.zshrc" "$HOME/.zshrc"

# ── macOS only ────────────────────────────────────────────────
if [[ $OS == macos ]]; then
  link macos/ghostty "$HOME/.config/ghostty"
fi

# A legacy ~/.tmux.conf does not shadow the XDG path (tmux 3.1+ prefers
# ~/.config/tmux/tmux.conf), but leaving one around is confusing — you edit it
# and nothing changes. Move it aside.
if [[ -f "$HOME/.tmux.conf" && ! -L "$HOME/.tmux.conf" ]]; then
  if $DRY; then
    printf '  \033[33mwould retire\033[0m  ~/.tmux.conf (superseded by the XDG path)\n'
  else
    mv "$HOME/.tmux.conf" "$HOME/.tmux.conf.backup.$STAMP"
    printf '  \033[33mretired\033[0m  ~/.tmux.conf (superseded by the XDG path)\n'
  fi
fi

if $NVIM && $DRY; then
  printf '  \033[36mwould restore\033[0m  nvim plugins from lazy-lock.json\n'
fi

report_missing() {
  (( MISSING > 0 )) || return 0
  printf '\033[31m%d source file(s) were missing\033[0m — those configs are NOT installed.\n' "$MISSING"
  echo "A partial checkout will do this; try: git -C \"$REPO\" status"
}

echo
if $DRY; then
  echo "Dry run complete."
  report_missing
  (( MISSING == 0 ))
  exit $?
fi

# TPM — not vendored here, fetch if absent.
#
# The presence check is `-d "$TPM/.git"`, not `-d "$TPM"`, and a failed clone
# cleans up after itself. A clone interrupted mid-transfer (dropped link, lid
# closed, Ctrl-C) leaves the directory behind; with the looser check that
# directory then counted as "installed" forever, so tmux.conf's `run '.../tpm'`
# silently did nothing on every start and re-running this never repaired it.
TPM="$HOME/.config/tmux/plugins/tpm"
if ! $APPLY && [[ ! -d "$TPM/.git" ]]; then
  echo "Installing TPM..."
  rm -rf "$TPM"
  if git clone --depth 1 -q https://github.com/tmux-plugins/tpm "$TPM"; then
    echo "  done — press prefix + I inside tmux to install plugins"
  else
    rm -rf "$TPM"
    printf '  \033[33mwarning\033[0m  TPM clone failed — tmux plugins will not load; re-run to retry\n'
  fi
  echo
fi

# nvim plugins — best-effort commit sync, NOT a guarantee of convergence.
#
# Know what this does and does not do. Lazy only ever runs `git checkout
# <sha>`, never `git checkout <branch>`, so the `branch` field in
# lazy-lock.json is decorative — restore does not honour it. Which branch a
# plugin tracks comes from its spec's `branch =`, or absent that, from
# whatever the remote's default branch was *when that machine first cloned
# it*. Two boxes cloning months apart can therefore sit on different
# branches with nobody running a wrong command.
#
# Worse, lazy regenerates the whole lockfile from on-disk state after every
# operation — including this one — so a plugin that is already off-branch
# gets its drift written back in as truth.
#
# The only thing that actually pins a plugin is `branch =` in the spec.
# nvim-treesitter, base46 and ui are pinned there for exactly this reason;
# anything else you care about should be too. Treat this step as "line up
# commits within the branches the specs already chose".
if $NVIM && ! $APPLY; then
  if command -v nvim >/dev/null 2>&1; then
    echo "Restoring nvim plugins from lazy-lock.json..."
    nvim --headless "+Lazy! restore" +qa
  else
    printf '  \033[33mwarning\033[0m  nvim not on PATH — skipping plugin restore\n'
  fi
fi

cat <<EOF
Done. To finish:
  1. exec zsh
  2. tmux source-file ~/.config/tmux/tmux.conf     (never kill-server on a
                                                    box with live sessions)
EOF

# Only mention the editor steps on a machine that has the editor. A --light
# box does not, and telling someone to run :MasonToolsInstall there is an
# instruction that cannot be followed.
if command -v nvim >/dev/null 2>&1; then
  cat <<EOF
  3. nvim, :Lazy restore      lines plugins up with lazy-lock.json (commits
                              only — branches come from the specs; see the
                              caveat above install.sh's restore step)
  4. :MasonToolsInstall       inside nvim, installs LSPs + formatters
EOF
fi

if [[ $OS == macos ]]; then
  echo "  Ghostty reloads its config on restart, or Cmd+R."
else
  cat <<'EOF'

Machine-specific shell bits go in ~/.zshrc.local — it is sourced if present
and stays untracked, so this repo's .zshrc is identical on every server.
EOF
fi

# Surfaced at the end rather than only at the point of failure, where a single
# red line scrolls away behind everything that follows.
if (( MISSING > 0 )); then
  echo
  report_missing
  exit 1
fi
