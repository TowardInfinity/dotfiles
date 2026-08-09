#!/usr/bin/env python3
"""Report Claude Code token spend by model, from local transcripts.

There is no official per-model subscription-quota multiplier, so this uses API
list price as a *proxy* for how much of the allowance each model eats. Treat the
ratios as directional; the turn counts and context sizes are exact.

Judge the default model by SESSIONS STARTED, not by turns. `"model": "sonnet"`
only sets what a new session opens on; it cannot retarget one already running,
so a single long Opus session can read as "100% Opus" while every fresh session
is correctly starting on Sonnet. Both views are printed; the turn-weighted one
still shows what the allowance actually paid for.

Validation needs fresh sessions, not elapsed time. Sessions that began before
POLICY_TS are excluded, and the report says so rather than quietly averaging
them in.

The context percentiles are the point of this script. Output tokens are a small
part of the bill — cache reads dominate, because every turn re-reads the whole
window. A p90 of 800k means most turns are paying ~80k input-equivalent before
doing any work.

    ./bin/claude-usage.py [--days N]
"""

import argparse
import collections
import datetime
import glob
import json
import os

# (input, output) USD per million tokens. Proxy only — see module docstring.
PRICE = {
    "claude-fable-5": (10, 50),
    "claude-opus-5": (5, 25),
    "claude-opus-4-8": (5, 25),
    "claude-sonnet-5": (2, 10),
    "claude-sonnet-4-6": (3, 15),
    "claude-haiku-4-5-20251001": (1, 5),
}
DEFAULT_PRICE = (2, 10)

# When the Sonnet-default policy landed: the mtime of the settings.json symlink
# on 2026-08-05. Sessions that began before this cannot say anything about
# whether the policy works, so they are reported but excluded from validation.
#
# Hardcoded rather than read from the file's mtime, because install.sh re-links
# settings.json on every `dots update` — the mtime would reset to "just now"
# and silently exclude everything.
POLICY_TS = "2026-08-05T01:10"

# A date is the wrong trigger for validation; fresh sessions are. Elapsed days
# produce no signal on their own — one long pre-policy session can run for a
# week and contribute nothing but its own turns.
MIN_FRESH_SESSIONS = 10

# Cache writes bill above the input rate; cache reads far below it.
CACHE_WRITE_MULT = 1.25
CACHE_READ_MULT = 0.10


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--days", type=int, default=0,
                    help="only count turns from the last N days (0 = all)")
    ap.add_argument("--root", default=os.path.expanduser("~/.claude/projects"))
    ap.add_argument("--policy", default=POLICY_TS, metavar="TS",
                    help="ISO timestamp the policy landed; sessions that began "
                         f"earlier are excluded from validation (default {POLICY_TS})")
    args = ap.parse_args()

    cutoff = ""
    if args.days:
        d = datetime.date.today() - datetime.timedelta(days=args.days)
        cutoff = d.isoformat()

    by = collections.defaultdict(collections.Counter)
    ctx = collections.defaultdict(list)
    daily = collections.defaultdict(collections.Counter)
    # One transcript file is one session.
    sessions = collections.defaultdict(
        lambda: {"first_ts": None, "first_model": None, "models": set()})

    for path in glob.glob(os.path.join(args.root, "*", "*.jsonl")):
        with open(path, errors="replace") as fh:
            for line in fh:
                # Cheap prefilter: these files run to tens of MB and most lines
                # carry no usage block at all.
                if '"usage"' not in line:
                    continue
                try:
                    rec = json.loads(line)
                except ValueError:
                    continue
                msg = rec.get("message") or {}
                usage = msg.get("usage")
                if not isinstance(usage, dict):
                    continue
                model = msg.get("model") or rec.get("model") or "?"

                # Which model a session STARTED on is the only thing
                # `"model": "sonnet"` actually governs — it sets the default for
                # a new session and cannot retarget one already in flight. So
                # counting raw turns answers the wrong question: one long Opus
                # session outvotes a dozen fresh Sonnet ones. Recorded before
                # the cutoff check, because deciding whether a session BEGAN
                # inside the window needs its true first turn, not the first
                # one that survived filtering.
                ts = rec.get("timestamp") or ""
                s = sessions[path]
                if s["first_ts"] is None or ts < s["first_ts"]:
                    s["first_ts"] = ts
                    s["first_model"] = model
                s["models"].add(model)

                # The turn-weighted aggregates are windowed, but session age
                # is not. Record the transcript's true first usage-bearing
                # turn before filtering, or an old session resumed this week
                # is falsely reported as a fresh post-policy start.
                day = ts[:10]
                if cutoff and day < cutoff:
                    continue

                inp = usage.get("input_tokens") or 0
                out = usage.get("output_tokens") or 0
                cw = usage.get("cache_creation_input_tokens") or 0
                cr = usage.get("cache_read_input_tokens") or 0

                c = by[model]
                c["turns"] += 1
                c["in"] += inp
                c["out"] += out
                c["cw"] += cw
                c["cr"] += cr
                ctx[model].append(inp + cw + cr)
                if day:
                    daily[day][model] += out

    if not by:
        print("no usage found under", args.root)
        return

    print(f"{'model':<28}{'turns':>8}{'output':>12}{'cache read':>14}{'~$ proxy':>10}")
    total = 0.0
    for model, c in sorted(by.items(), key=lambda kv: -kv[1]["out"]):
        pi, po = PRICE.get(model, DEFAULT_PRICE)
        cost = (c["in"] * pi + c["out"] * po
                + c["cw"] * pi * CACHE_WRITE_MULT
                + c["cr"] * pi * CACHE_READ_MULT) / 1e6
        total += cost
        print(f"{model:<28}{c['turns']:>8,}{c['out']:>12,}{c['cr']:>14,}{cost:>10.2f}")
    print(f"{'TOTAL':<28}{'':>8}{'':>12}{'':>14}{total:>10.2f}")

    print("\ncontext per turn (input + cache) — the number that matters:")
    for model, vals in sorted(ctx.items(), key=lambda kv: -len(kv[1])):
        if len(vals) < 50:
            continue
        vals.sort()
        n = len(vals)
        print(f"  {model:<26} p50={vals[n // 2] / 1000:>7.0f}k"
              f"  p90={vals[int(n * .9)] / 1000:>7.0f}k"
              f"  max={vals[-1] / 1000:>7.0f}k")

    in_window = [s for s in sessions.values()
                 if s["first_ts"] and s["first_ts"][:10] >= cutoff]
    post = [s for s in in_window if s["first_ts"] >= args.policy]
    pre = len(in_window) - len(post)

    print(f"\nsessions STARTED in window ({len(in_window)}) — "
          "the real test of the default model:")
    if pre:
        print(f"  {pre} began before the policy ({args.policy}) "
              "— EXCLUDED from validation")
    if post:
        started = collections.Counter(s["first_model"] for s in post)
        for model, n in started.most_common():
            print(f"  {model:<26}{n:>5}  {n / len(post) * 100:>5.0f}%")
        switched = sum(1 for s in post if len(s["models"]) > 1)
        if switched:
            print(f"  ({switched} later switched model mid-session — "
                  "a deliberate /model escalation looks like this)")
    if len(post) < MIN_FRESH_SESSIONS:
        print(f"  NOT YET VALID: {len(post)}/{MIN_FRESH_SESSIONS} "
              "post-policy sessions. Waiting longer does not help — "
              "only starting new sessions does.")

    print("\nlast 14 days, output tokens by model:")
    for day in sorted(daily)[-14:]:
        row = " ".join(f"{m.replace('claude-', '')}={v / 1000:.0f}k"
                       for m, v in daily[day].most_common(4))
        print(f"  {day}  {row}")


if __name__ == "__main__":
    main()
