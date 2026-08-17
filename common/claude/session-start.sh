#!/bin/sh
# Claude Code SessionStart hook — see `dots models` and `dots memory`.
#
# Two independent things happen here:
#  1. Model policy drift check — resume only, reaches the user (systemMessage)
#     and the session (additionalContext).
#  2. Memory digest — every session, session-facing only (additionalContext).
#     `dots memory recall` reads the prebuilt index and never invokes Ollama.
#
# They used to be exclusive: a dozen early `exit 0`s, correct back when there
# was only ever one thing to say. With two independent things, an early exit
# for "policy check doesn't apply here" must not also skip memory injection on
# a fresh (non-resume) session — the common case, and the one the old
# `[ "$source_of" = "resume" ] || exit 0` would otherwise throw away. So both
# sections accumulate into variables and fall through; one `jq -n` at the end
# assembles whichever combination is non-empty.
#
# Never fails a session. Every path exits 0 — a broken hook here would break
# starting Claude at all, which is far worse than a missed warning or a missed
# digest.

payload=$(cat 2>/dev/null) || exit 0

msg=""      # systemMessage — user-facing, policy drift only
ctx=""      # additionalContext — session-facing, policy drift + memory digest

source_of=$(printf '%s' "$payload" | jq -r '.source // ""' 2>/dev/null)
transcript=$(printf '%s' "$payload" | jq -r '.transcript_path // ""' 2>/dev/null)
cwd=$(printf '%s' "$payload" | jq -r '.cwd // ""' 2>/dev/null)

# ── 1. model policy drift (resume only) ──────────────────────────────────
#
# The problem it exists for: `"model"` in settings.json is the default for a
# *new* session only. Resume an old one and it keeps whatever model it was
# already on, silently, forever. Verified rather than assumed — a session
# started on haiku and resumed with no --model came back on haiku, not on the
# configured sonnet. So every session started before a policy change goes on
# ignoring that change for as long as you keep resuming it.
#
# Only on resume: a fresh start already obeys settings.json, and `--model
# opus` on the command line is a deliberate choice that does not need
# arguing with.
if [ "$source_of" = "resume" ] && [ -n "$transcript" ] && [ -f "$transcript" ]; then
  settings="${CLAUDE_CONFIG_DIR:-$HOME/.claude}/settings.json"
  if [ -f "$settings" ]; then
    want_model=$(jq -r '.model // ""'       "$settings" 2>/dev/null)
    want_effort=$(jq -r '.effortLevel // ""' "$settings" 2>/dev/null)
    if [ -n "$want_model" ]; then
      # The last assistant turn is what the session is actually running on.
      # Read the tail rather than the file: these reach tens of megabytes,
      # and a hook that parses all of it delays every resume. `fromjson? //
      # empty` drops the partial first line the byte-offset tail leaves
      # behind.
      got=$(tail -c 262144 "$transcript" 2>/dev/null \
              | jq -Rr 'fromjson? // empty
                        | select(.message.model != null)
                        | [.message.model, (.effort // "")] | @tsv' 2>/dev/null \
              | tail -1)
      if [ -n "$got" ]; then
        got_model=$(printf '%s' "$got" | cut -f1)
        got_effort=$(printf '%s' "$got" | cut -f2)

        # claude-opus-5 and opus are the same policy fact stated two ways.
        family() { printf '%s' "$1" | sed -e 's/^claude-//' -e 's/-[0-9].*$//'; }
        want_f=$(family "$want_model")
        got_f=$(family "$got_model")

        # opusplan is Opus while planning and Sonnet while executing, so a
        # session on either of those is obeying it, not defying it.
        skip_opusplan=false
        if [ "$want_f" = "opusplan" ]; then
          case "$got_f" in opus|sonnet) skip_opusplan=true ;; esac
        fi

        if ! $skip_opusplan; then
          model_off=false; effort_off=false
          [ "$want_f" = "$got_f" ] || model_off=true
          if [ -n "$want_effort" ] && [ -n "$got_effort" ] && [ "$want_effort" != "$got_effort" ]; then
            effort_off=true
          fi

          if $model_off || $effort_off; then
            if $model_off; then
              what="model $got_f"; want="$want_f"; fix="/model $want_f"
              $effort_off && { what="$what at $got_effort"; want="$want_f at $want_effort"
                               fix="$fix and /effort $want_effort"; }
            else
              what="effort $got_effort"; want="$want_effort"; fix="/effort $want_effort"
            fi

            # Naming the cost is the part that changes behaviour. "opus" is a
            # fact; "2.5x" is a reason. Only for the tiers where it is true —
            # claiming a multiplier for sonnet would train you to ignore the
            # ones that matter.
            cost=""
            case "$got_f" in
              fable) cost=" — 5x sonnet" ;;
              opus)  cost=" — 2.5x sonnet" ;;
            esac

            msg="Resumed on $what$cost; policy is $want. Switch with $fix, or carry on if it is deliberate."
            ctx="Session resumed off model policy: $msg Mention this to the user in one short line at the start of your next reply, then continue with the task."
          fi
        fi
      fi
    fi
  fi
fi

# ── 2. memory digest (every session) ─────────────────────────────────────
#
# `command -v dots` is what makes this safe to ship to a box that has no
# `dots` yet, or one where the release binary predates the memory subcommand.
# `--deadline` is a hard bound, not a hope: a stalled iCloud read or a
# half-written index degrades to silence, never to a hang that blocks
# starting Claude. Reads the prebuilt index only; never invokes Ollama.
digest=""
if command -v dots >/dev/null 2>&1; then
  digest=$(dots memory recall --dir "$cwd" --budget 1024 --deadline 2s 2>/dev/null)
fi

if [ -n "$digest" ]; then
  if [ -n "$ctx" ]; then
    ctx="$ctx

$digest"
  else
    ctx="$digest"
  fi
fi

# ── emit ───────────────────────────────────────────────────────────────
#
# Four shapes: policy only, memory only, both, neither. jq builds whichever
# applies so a quote or a newline in any of this cannot produce broken JSON
# and a failed hook. Nothing is printed, and the exit is silent, when neither
# msg nor ctx has anything to say — that is the common, correct case.
if [ -n "$msg" ] && [ -n "$ctx" ]; then
  jq -n --arg m "$msg" --arg c "$ctx" \
    '{systemMessage: $m,
      hookSpecificOutput: {hookEventName: "SessionStart", additionalContext: $c}}' 2>/dev/null
elif [ -n "$msg" ]; then
  jq -n --arg m "$msg" '{systemMessage: $m}' 2>/dev/null
elif [ -n "$ctx" ]; then
  jq -n --arg c "$ctx" \
    '{hookSpecificOutput: {hookEventName: "SessionStart", additionalContext: $c}}' 2>/dev/null
fi

exit 0
