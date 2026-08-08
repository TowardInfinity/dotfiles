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
#
# It also prints the account's quota burn, which is not a per-pane fact and so
# shows on every pane — see quota_segment at the bottom.

set -u

pane_id="${1:-${TMUX_PANE:-}}"
pane_cmd="${2:-}"
[ -n "$pane_id" ] || exit 0

MARKS="${DOTS_PANE_DIR:-$HOME/.cache/dots/panes}"
mark="$MARKS/${pane_id#%}"

# In a function so the many "nothing to show" exits stay simple `return`s: the
# quota segment below has to print whether or not this pane has an agent in it.
model_segment() {

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

[ -n "$tool" ] || return 0

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
[ -n "$tag" ] || return 0

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
}

# ── the quota gauge ───────────────────────────────────────────
#
# Written by common/claude/statusline.sh from the statusLine payload, which is
# the only place the live 5-hour and 7-day burn is readable without spending a
# turn on /usage. It is an account fact rather than a pane fact, so it renders
# on every pane — the point is to see the number from a plain shell, not only
# while staring at a Claude session.
#
# Codex contributes nothing here: its rollouts carry no quota figures, and its
# allowance is a separate bucket on a separate subscription anyway.
quota_segment() {
  q="${DOTS_STATE_DIR:-$HOME/.cache/dots}/claude-quota"
  [ -f "$q" ] || return 0

  written=""; five=""; five_at=""; seven=""; seven_at=""
  {
    IFS= read -r written
    IFS= read -r five
    IFS= read -r five_at
    IFS= read -r seven
    IFS= read -r seven_at
  } < "$q" 2>/dev/null || return 0

  now=$(date +%s 2>/dev/null) || return 0
  case "$written" in ''|*[!0-9]*) return 0 ;; esac

  # Age out rather than show a stale number as current. Fifteen minutes is
  # long enough to survive a quiet spell between turns and short enough that
  # the figure is still true — quota moves in minutes, not hours.
  [ $((now - written)) -le 900 ] || return 0

  # Whichever window is closer to capping is the one that will actually stop
  # you. Showing both and leaving you to compare them is the version of this
  # that gets ignored.
  # Truncate a decimal tail before anything compares these. statusline.sh
  # rounds them at write time — the payload really does carry values like
  # 28.999999999999996 — but a file written by an older copy would otherwise be
  # rejected as non-numeric and silently read as 0, which shows a busy quota as
  # empty. Wrong in the reassuring direction is the worst way for this to fail.
  win=5h; pct="${five%%.*}"
  case "$pct" in ''|*[!0-9]*) pct=0 ;; esac
  s="${seven%%.*}"; case "$s" in ''|*[!0-9]*) s=0 ;; esac
  resets="$five_at"
  [ "$s" -gt "$pct" ] && { win=7d; pct="$s"; resets="$seven_at"; }
  [ "$pct" -gt 0 ] || return 0

  if   [ "$pct" -ge 85 ]; then fg='#f7768e'
  elif [ "$pct" -ge 60 ]; then fg='#e0af68'
  else                         fg='#565f89'   # fine: say so quietly
  fi

  # Time to reset only once it matters. Below 60% the answer to "how long until
  # this clears" is "you do not care", and the width is better spent elsewhere.
  left=""
  if [ "$pct" -ge 60 ]; then
    case "$resets" in
      ''|*[!0-9]*) ;;
      *) d=$((resets - now))
         if   [ "$d" -le 0 ];    then left=" now"
         elif [ "$d" -lt 3600 ]; then left=" $((d / 60))m"
         elif [ "$d" -lt 86400 ];then left=" $((d / 3600))h"
         else                         left=" $((d / 86400))d"
         fi ;;
    esac
  fi

  printf '#[fg=%s]%s %s%%%s#[default] ' "$fg" "$win" "$pct" "$left"
}

model_segment
quota_segment
