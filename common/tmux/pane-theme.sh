#!/bin/sh
# tmux-pane-theme — recolour every pane currently running Claude Code or Codex.
#
# Called from status-right, once per status-interval, with no arguments — see
# common/tmux/model.sh for why that job must be fast and must never block.
#
# It takes no #{...} arguments on purpose. A #() job only ever sees the one
# pane tmux considers current, but an unfocused agent pane in a split should
# stay themed too, so this walks every pane on the server itself.
#
# Detection reuses the exact marker file model.sh already trusts:
# ${DOTS_PANE_DIR:-~/.cache/dots/panes}/<pane>, written by
# common/claude/statusline.sh or the Codex zsh wrapper, and proven live the
# same two ways model.sh already does:
#   claude — the marker's recorded owner pid must still be running.
#   codex  — the marker is only trusted while tmux's own view of the pane
#            still says "codex"; a bare `command codex` with no wrapper at
#            all still counts, same as model.sh's fallback.
#
# State is cached per pane in <mark>.theme so a steady-state pane costs one
# stat() per interval instead of a tmux select-pane call every 5 seconds.

set -u

MARKS="${DOTS_PANE_DIR:-$HOME/.cache/dots/panes}"
GREEN='#55b571'
NAVY='#151628'

command -v tmux >/dev/null 2>&1 || exit 0
[ -d "$MARKS" ] || exit 0

# No `|| exit 0` here: a dead server (last session just closed) makes this
# fail too, but that means zero live panes, not "give up" — the empty $panes
# still has to reach the sweep below so its .theme files get cleaned instead
# of orphaned until a future pane_id collides with one by coincidence.
panes=$(tmux list-panes -a -F '#{pane_id} #{pane_current_command}' 2>/dev/null)

# Computed up front, not inside the loop below: that loop runs in a subshell
# (POSIX sh pipes both sides), so nothing it sets would survive past `done`.
# Single space-joined line, not one id per line — the sweep's `case " $x "
# in *" $id "*` match needs a literal space on both sides of every id.
live_ids=$(printf '%s\n' "$panes" | awk '{sub(/^%/,"",$1); printf "%s ", $1}')

printf '%s\n' "$panes" | while IFS=' ' read -r pane_id pane_cmd; do
  [ -n "$pane_id" ] || continue
  mark="$MARKS/${pane_id#%}"
  state_file="$mark.theme"

  tool=""; owner=""
  if [ -f "$mark" ]; then
    IFS='	' read -r tool owner _ < "$mark" 2>/dev/null || true
  fi
  case "$tool" in
    claude)
      # Same staleness check model.sh applies: the recorded pid is the Claude
      # process itself, and a gone pid means the session ended whatever the
      # file still says.
      if [ -z "$owner" ] || ! kill -0 "$owner" 2>/dev/null; then
        tool=""
      fi
      ;;
    codex)
      # The zsh wrapper removes its marker on exit, but a killed terminal
      # never gets to — trust tmux's live view of the pane instead.
      [ "$pane_cmd" = codex ] || tool=""
      ;;
    *)
      tool=""
      ;;
  esac
  # Codex started without the wrapper still deserves the theme — tmux's own
  # view of the pane is enough to know it is there.
  if [ -z "$tool" ] && [ "$pane_cmd" = codex ]; then
    tool=codex
  fi

  want=none
  case "$tool" in
    claude|codex) want="$tool" ;;
  esac

  have=$(cat "$state_file" 2>/dev/null || echo none)
  [ "$want" = "$have" ] && continue

  if [ "$want" != none ]; then
    tmux select-pane -t "$pane_id" -P "fg=$GREEN,bg=$NAVY" 2>/dev/null
  else
    tmux select-pane -t "$pane_id" -P 'fg=default,bg=default' 2>/dev/null
  fi
  printf '%s' "$want" > "$state_file" 2>/dev/null
done

# Sweep .theme files left behind by panes that closed since the last tick —
# same cleanup instinct as the resolver's release-cache pruning: small state
# that nothing else ever reclaims otherwise.
for state_file in "$MARKS"/*.theme; do
  [ -e "$state_file" ] || continue
  id=$(basename "$state_file" .theme)
  case " $live_ids " in
    *" $id "*) ;;
    *) rm -f "$state_file" 2>/dev/null ;;
  esac
done

exit 0
