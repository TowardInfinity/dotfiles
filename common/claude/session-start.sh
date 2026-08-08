#!/bin/sh
# Claude Code SessionStart hook — see `dots models`.
#
# The problem it exists for: `"model"` in settings.json is the default for a
# *new* session only. Resume an old one and it keeps whatever model it was
# already on, silently, forever. Verified rather than assumed — a session
# started on haiku and resumed with no --model came back on haiku, not on the
# configured sonnet. So every session you started before a policy change goes
# on ignoring that change for as long as you keep resuming it, and the longest
# running sessions are exactly the expensive ones.
#
# Nothing here can change the model: no hook can, and `/model` is the user's
# call. What it can do is stop the drift being invisible — say so in the UI,
# where the person who can act on it will see it, and tell the session itself
# so it can mention it once rather than quietly burning the dear tier.
#
# Quiet when the session is on policy: no output, no injected context, no cost.
# This whole policy is about not paying for tokens nobody reads.
#
# Never fails a session. Every path exits 0 — a broken hook here would break
# starting Claude at all, which is a far worse outcome than a missed warning.

payload=$(cat 2>/dev/null) || exit 0

source_of=$(printf '%s' "$payload" | jq -r '.source // ""' 2>/dev/null)
transcript=$(printf '%s' "$payload" | jq -r '.transcript_path // ""' 2>/dev/null)

# Only on resume. A fresh start already obeys settings.json, and `--model opus`
# on the command line is a deliberate choice that does not need arguing with.
[ "$source_of" = "resume" ] || exit 0
[ -n "$transcript" ] && [ -f "$transcript" ] || exit 0

settings="${CLAUDE_CONFIG_DIR:-$HOME/.claude}/settings.json"
[ -f "$settings" ] || exit 0

want_model=$(jq -r '.model // ""'       "$settings" 2>/dev/null)
want_effort=$(jq -r '.effortLevel // ""' "$settings" 2>/dev/null)
[ -n "$want_model" ] || exit 0

# The last assistant turn is what the session is actually running on. Read the
# tail rather than the file: these reach tens of megabytes, and a hook that
# parses all of it delays every resume. `fromjson? // empty` drops the partial
# first line the byte-offset tail leaves behind.
got=$(tail -c 262144 "$transcript" 2>/dev/null \
        | jq -Rr 'fromjson? // empty
                  | select(.message.model != null)
                  | [.message.model, (.effort // "")] | @tsv' 2>/dev/null \
        | tail -1)
[ -n "$got" ] || exit 0
got_model=$(printf '%s' "$got" | cut -f1)
got_effort=$(printf '%s' "$got" | cut -f2)

# claude-opus-5 and opus are the same policy fact stated two ways.
family() { printf '%s' "$1" | sed -e 's/^claude-//' -e 's/-[0-9].*$//'; }
want_f=$(family "$want_model")
got_f=$(family "$got_model")

# opusplan is Opus while planning and Sonnet while executing, so a session on
# either of those is obeying it, not defying it.
if [ "$want_f" = "opusplan" ]; then
  case "$got_f" in opus|sonnet) exit 0 ;; esac
fi

model_off=false; effort_off=false
[ "$want_f" = "$got_f" ] || model_off=true
if [ -n "$want_effort" ] && [ -n "$got_effort" ] && [ "$want_effort" != "$got_effort" ]; then
  effort_off=true
fi
$model_off || $effort_off || exit 0

# ── say so ────────────────────────────────────────────────────
if $model_off; then
  what="model $got_f"; want="$want_f"; fix="/model $want_f"
  $effort_off && { what="$what at $got_effort"; want="$want_f at $want_effort"
                   fix="$fix and /effort $want_effort"; }
else
  what="effort $got_effort"; want="$want_effort"; fix="/effort $want_effort"
fi

# Naming the cost is the part that changes behaviour. "opus" is a fact; "2.5x"
# is a reason. Only for the tiers where it is true — claiming a multiplier for
# sonnet would train you to ignore the ones that matter.
cost=""
case "$got_f" in
  fable) cost=" — 5x sonnet" ;;
  opus)  cost=" — 2.5x sonnet" ;;
esac

msg="Resumed on $what$cost; policy is $want. Switch with $fix, or carry on if it is deliberate."

# systemMessage reaches the user, who is the only one who can run /model.
# additionalContext reaches the session, so it mentions it once instead of
# spending the dearer tier without comment. jq builds it so a quote or a
# newline in any of this cannot produce broken JSON and a failed hook.
jq -n --arg m "$msg" \
  '{systemMessage: $m,
    hookSpecificOutput: {
      hookEventName: "SessionStart",
      additionalContext: ("Session resumed off model policy: " + $m +
        " Mention this to the user in one short line at the start of your next reply, then continue with the task.")
    }}' 2>/dev/null

exit 0
