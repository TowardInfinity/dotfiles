#!/usr/bin/env python3
"""Report Claude Code token spend by model, from local transcripts.

There is no official per-model subscription-quota multiplier, so this uses API
list price as a *proxy* for how much of the allowance each model eats. Treat the
ratios as directional; the turn counts and context sizes are exact.

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

# Cache writes bill above the input rate; cache reads far below it.
CACHE_WRITE_MULT = 1.25
CACHE_READ_MULT = 0.10


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--days", type=int, default=0,
                    help="only count turns from the last N days (0 = all)")
    ap.add_argument("--root", default=os.path.expanduser("~/.claude/projects"))
    args = ap.parse_args()

    cutoff = ""
    if args.days:
        d = datetime.date.today() - datetime.timedelta(days=args.days)
        cutoff = d.isoformat()

    by = collections.defaultdict(collections.Counter)
    ctx = collections.defaultdict(list)
    daily = collections.defaultdict(collections.Counter)

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
                day = (rec.get("timestamp") or "")[:10]
                if cutoff and day < cutoff:
                    continue

                model = msg.get("model") or rec.get("model") or "?"
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

    print("\nlast 14 days, output tokens by model:")
    for day in sorted(daily)[-14:]:
        row = " ".join(f"{m.replace('claude-', '')}={v / 1000:.0f}k"
                       for m, v in daily[day].most_common(4))
        print(f"  {day}  {row}")


if __name__ == "__main__":
    main()
