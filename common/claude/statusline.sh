#!/bin/sh
# Claude Code statusLine command — see `dots models`.
#
# Claude Code pipes a JSON blob in on stdin on every UI refresh and prints
# whatever comes back out under the prompt. Two jobs from one invocation:
#
#   1. print the in-session line, with the model colour-coded by what it costs
#   2. drop a marker naming this tmux pane's model, so the tmux status bar can
#      show it too — tmux has no way to ask a running Claude what it is on
#
# Why the model has to come from here rather than from settings.json: the
# `"model"` key is only the default for *new* sessions. `/model opus` mid-session
# changes what you are actually paying for and leaves the file untouched, which
# is exactly the case an indicator exists to catch.
#
# It runs on every refresh, so it stays cheap: one jq, no network, no git.

set -u

payload=$(cat)

# One jq pass, @tsv so empty fields still hold their position.
IFS='	' read -r model_id model_name dir <<EOF
$(printf '%s' "$payload" | jq -r '[
    (.model.id // ""),
    (.model.display_name // ""),
    (.workspace.current_dir // .cwd // "")
  ] | @tsv' 2>/dev/null)
EOF

# claude-opus-5 → opus; claude-haiku-4-5-20251001 → haiku. The family is the
# only part that maps to a price tier, and it is the part that fits in a pane.
tag=$(printf '%s' "$model_id" | sed -e 's/^claude-//' -e 's/-[0-9].*$//')
[ -n "$tag" ] || tag=$(printf '%s' "$model_name" | tr 'A-Z ' 'a-z-')
[ -n "$tag" ] || tag="?"

# Colour by price tier, not by prettiness. Opus is 2.5x Sonnet and Fable is 5x,
# and both share one quota with claude.ai — so they get the alarm colour rather
# than a neutral one. See `dots models`.
case "$tag" in
  fable|opus) colour=$(printf '\033[38;2;247;118;142m') ;;  # red
  sonnet)     colour=$(printf '\033[38;2;158;206;106m') ;;  # green
  haiku)      colour=$(printf '\033[38;2;125;207;255m') ;;  # cyan
  *)          colour=$(printf '\033[38;2;169;177;214m') ;;
esac
dim=$(printf '\033[38;2;86;95;137m')
reset=$(printf '\033[0m')

# ── the tmux pane marker ──────────────────────────────────────
#
# $PPID is the Claude Code process that spawned us, which makes it a free and
# exact liveness token: tmux-model refuses a marker whose owner has exited, so
# quitting Claude clears the segment without anything having to clean up.
if [ -n "${TMUX_PANE:-}" ]; then
  dir_marks="${DOTS_PANE_DIR:-$HOME/.cache/dots/panes}"
  if mkdir -p "$dir_marks" 2>/dev/null; then
    # Write-then-rename: the status bar reads this file on a timer and a
    # half-written line would render as garbage in the middle of the bar.
    tmp="$dir_marks/.${TMUX_PANE#%}.$$"
    if printf 'claude\t%s\t%s\n' "$PPID" "$tag" > "$tmp" 2>/dev/null; then
      mv -f "$tmp" "$dir_marks/${TMUX_PANE#%}" 2>/dev/null || rm -f "$tmp"
    else
      rm -f "$tmp" 2>/dev/null
    fi
  fi
fi

# ── the line Claude Code shows ────────────────────────────────
printf '%s%s%s' "$colour" "$tag" "$reset"
[ -n "$dir" ] && printf ' %s%s%s' "$dim" "$(basename "$dir")" "$reset"
printf '\n'
