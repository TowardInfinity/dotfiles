#!/bin/sh
# tmux-model — print the model the current pane's agent is running on.
#
# Called from status-right as
#
#   #(~/.local/bin/tmux-model '#{pane_id}' '#{pane_current_command}')
#
# and re-run every status-interval, so it must be fast and must never block:
# a status-line job that hangs freezes the whole bar.
#
# Why a marker file instead of asking the process: neither Claude Code nor
# Codex answers questions from outside, and the model is not in the command
# line — `claude` started on Sonnet and switched to Opus with `/model` looks
# identical in ps. So each side leaves its current model somewhere the bar can
# read it:
#
#   Claude — common/claude/statusline.sh, wired up as the statusLine command.
#            Runs on every UI refresh, so a mid-session /model shows up here.
#   Codex  — the zsh wrapper in $OS/zsh/.zshrc writes the launch model, and we
#            refresh it from the session rollout, which records model+effort on
#            every turn. That catches a mid-session /model too.
#
# Prints nothing at all when the pane is not running one of them: a status bar
# segment that is empty when it has nothing to say costs no width.

set -u

pane_id="${1:-${TMUX_PANE:-}}"
pane_cmd="${2:-}"
[ -n "$pane_id" ] || exit 0

MARKS="${DOTS_PANE_DIR:-$HOME/.cache/dots/panes}"
mark="$MARKS/${pane_id#%}"

tool=""; owner=""; label=""
if [ -f "$mark" ]; then
  IFS='	' read -r tool owner label < "$mark" 2>/dev/null || true
fi

# ── is the marker still true? ─────────────────────────────────
#
# Two different liveness tests because the two sides can prove it two different
# ways, and both are free.
case "$tool" in
  claude)
    # statusline.sh records its $PPID — the Claude process itself. If that pid
    # is gone the session is over, whatever the file still says.
    if [ -z "$owner" ] || ! kill -0 "$owner" 2>/dev/null; then
      rm -f "$mark" 2>/dev/null
      tool=""
    fi
    ;;
  codex)
    # The zsh wrapper removes its marker on exit, but a killed terminal never
    # gets to. tmux already knows what is in the pane's foreground, so use that
    # as the authority rather than trusting the cleanup to have happened.
    [ "$pane_cmd" = "codex" ] || { rm -f "$mark" 2>/dev/null; tool=""; }
    ;;
  *)
    tool=""
    ;;
esac

# Codex started without the wrapper (`command codex`, or a shell that predates
# it) still deserves a segment — tmux's own view of the pane is enough to know
# it is there, and the config default is the right guess for the model.
if [ -z "$tool" ] && [ "$pane_cmd" = "codex" ]; then
  tool=codex
  label=""
fi

[ -n "$tool" ] || exit 0

# ── codex: refresh from the live session rollout ──────────────
if [ "$tool" = codex ]; then
  sessions="${CODEX_HOME:-$HOME/.codex}/sessions"
  if [ -d "$sessions" ]; then
    # Only rollouts touched in the last five minutes can belong to a session
    # that is on screen right now. -mmin is in both BSD and GNU find; -newermt
    # takes a relative string only under GNU, and a1/v1/v2 are the machines
    # where that would have failed silently.
    cands=$(find "$sessions" -name 'rollout-*.jsonl' -type f -mmin -5 2>/dev/null)
    pick=""
    if [ -n "$cands" ]; then
      # Newest by mtime, not by name: a resumed session keeps its original
      # timestamp in the filename but is the one actually being written to.
      pick=$(printf '%s\n' "$cands" | tr '\n' '\0' \
               | xargs -0 ls -t 2>/dev/null | head -1)
    fi
    if [ -n "$pick" ] && [ -f "$pick" ]; then
      # Read the tail only: rollouts grow without bound and the newest
      # turn_context is what we want. "model_provider" does not match, and
      # anchoring effort on a brace or comma keeps "reasoning_effort" out.
      chunk=$(tail -c 65536 "$pick" 2>/dev/null)
      m=$(printf '%s' "$chunk" | grep -o '"model":"[^"]*"' | tail -1 \
            | sed -e 's/.*:"//' -e 's/"$//')
      e=$(printf '%s' "$chunk" | grep -o '[,{]"effort":"[^"]*"' | tail -1 \
            | sed -e 's/.*:"//' -e 's/"$//')
      [ -n "$m" ] && label="$m"
      # Two letters, because effort is a modifier and should not out-width the
      # model name it modifies. "hig" read as a truncation bug; "hi" reads as
      # shorthand.
      case "$e" in
        low)    label="$label/lo" ;;
        medium) label="$label/md" ;;
        high)   label="$label/hi" ;;
        xhigh)  label="$label/xh" ;;
        max)    label="$label/mx" ;;
        ?*)     label="$label/$(printf '%s' "$e" | cut -c1-2)" ;;
      esac
    fi
  fi
  # Still nothing: fall back to the configured default rather than a blank.
  if [ -z "$label" ]; then
    label=$(sed -n 's/^model *= *"\([^"]*\)".*/\1/p' \
              "${CODEX_HOME:-$HOME/.codex}/config.toml" 2>/dev/null | head -1)
  fi
fi

# gpt-5.6-terra → terra. The family name is the part that maps to a price tier.
tag=$(printf '%s' "$label" | sed -e 's|^gpt-[0-9.]*-||' -e 's/^claude-//')
[ -n "$tag" ] || exit 0

# ── colour by price tier ──────────────────────────────────────
#
# Deliberately loud for the expensive tiers. The whole reason this segment
# exists is that Opus and Fable emptied a shared quota without anyone noticing,
# and a neutral grey label would have gone on not being noticed.
case "$tag" in
  fable*|opus*|sol*) fg='#f7768e' ;;   # red    — 2.5x to 5x the default
  sonnet*|terra*)    fg='#9ece6a' ;;   # green  — on policy
  haiku*|luna*)      fg='#7dcfff' ;;   # cyan   — cheap
  *)                 fg='#a9b1d6' ;;
esac

printf '#[fg=%s]󰚩 %s#[default] ' "$fg" "$tag"
