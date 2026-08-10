#!/bin/sh
# tmux-pane-theme — recolour every pane currently running Claude Code.
#
# Called from status-right, once per status-interval, with no arguments — see
# common/tmux/model.sh for why that job must be fast and must never block.
#
# It takes no #{...} arguments on purpose. A #() job only ever sees the one
# pane tmux considers current, but a Claude pane sitting unfocused in a split
# should stay themed too, so this walks every pane on the server itself.
#
# Detection reuses the exact marker file model.sh already trusts:
# ${DOTS_PANE_DIR:-~/.cache/dots/panes}/<pane>, written by
# common/claude/statusline.sh, proven live the same way — the recorded owner
# pid must still be running, or the session ended and the marker is stale.
#
# State is cached per pane in <mark>.theme so a steady-state pane costs one
# stat() per interval instead of a tmux select-pane call every 5 seconds.

set -u

MARKS="${DOTS_PANE_DIR:-$HOME/.cache/dots/panes}"
GREEN='#55b571'
NAVY='#151628'

command -v tmux >/dev/null 2>&1 || exit 0
[ -d "$MARKS" ] || exit 0

live_ids=""
# No `|| exit 0` here: a dead server (last session just closed) makes this
# fail too, but that means zero live panes, not "give up" — the empty $panes
# still has to reach the sweep below so its .theme files get cleaned instead
# of orphaned until a future pane_id collides with one by coincidence.
panes=$(tmux list-panes -a -F '#{pane_id}' 2>/dev/null)

for pane_id in $panes; do
  live_ids="$live_ids ${pane_id#%}"
  mark="$MARKS/${pane_id#%}"
  state_file="$mark.theme"

  tool=""; owner=""
  if [ -f "$mark" ]; then
    IFS='	' read -r tool owner _ < "$mark" 2>/dev/null || true
  fi
  # Same staleness check model.sh applies before trusting a claude marker: the
  # recorded pid is the Claude process itself, and a gone pid means the
  # session ended whatever the file still says.
  if [ "$tool" = claude ]; then
    if [ -z "$owner" ] || ! kill -0 "$owner" 2>/dev/null; then
      tool=""
    fi
  fi

  want=none
  [ "$tool" = claude ] && want=claude

  have=$(cat "$state_file" 2>/dev/null || echo none)
  [ "$want" = "$have" ] && continue

  if [ "$want" = claude ]; then
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
