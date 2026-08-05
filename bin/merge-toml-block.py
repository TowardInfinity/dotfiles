#!/usr/bin/env python3
"""Splice a block of shared TOML into a file the application also writes to.

Used by install.sh for ~/.codex/config.toml. Codex appends
[projects."/abs/path"] trust blocks and [plugins.*] entries to that file, so it
cannot be a symlink into the repo — one machine's absolute paths would land on
all the others. But the model policy still has to travel, so it is merged in
between markers instead.

Contract:

  * Everything outside the markers belongs to the machine and is preserved.
  * Anything the source defines — top-level keys and whole tables — is removed
    from wherever the machine had it, so there is exactly one definition.
  * Output order respects TOML: the machine's own bare keys, then the managed
    block, then the machine's tables. Bare keys after a table header would
    silently become members of that table.
  * Idempotent. The previous managed block is stripped before the new one is
    written, so re-running produces a byte-identical file and reports
    "unchanged".

Prints one word to stdout: unchanged | merged | would-merge | would-create.
Exits non-zero with a message on stderr if the merge cannot be done safely.
"""

import argparse
import os
import re
import sys

# A table header at the start of a line: [foo] or [["a.b"]]. Indented headers
# are not valid TOML at the top level, so anchoring to column 0 is correct and
# avoids matching an array value that happens to start a continuation line.
HEADER = re.compile(r'^\[\[?\s*(.+?)\s*\]\]?\s*(?:#.*)?$')
# key = ... / "quoted key" = ... — enough to spot a top-level assignment.
ASSIGN = re.compile(r'^\s*((?:[A-Za-z0-9_-]+|"[^"]*"|\'[^\']*\'))\s*=')


def table_root(name):
    """Root table of a dotted header: agents.sub -> agents, "a.b" -> a.b"""
    if name.startswith('"') or name.startswith("'"):
        return name[1:name.index(name[0], 1)] if name.count(name[0]) > 1 else name
    return name.split(".", 1)[0]


def split_sections(lines):
    """[(header_or_None, [lines])] — preamble first, then one entry per table.

    Comment lines immediately preceding a header travel with that header, so
    removing a table takes its documentation with it instead of orphaning it.
    """
    sections, cur, head = [], [], None
    pending = []
    for line in lines:
        m = HEADER.match(line)
        if m:
            sections.append((head, cur))
            head, cur = m.group(1), list(pending)
            pending = []
            cur.append(line)
            continue
        stripped = line.strip()
        if stripped.startswith("#") or not stripped:
            pending.append(line)
        else:
            cur.extend(pending)
            pending = []
            cur.append(line)
    cur.extend(pending)
    sections.append((head, cur))
    return sections


def strip_block(lines, begin, end):
    """Remove a previously written managed block, if present."""
    try:
        i = next(n for n, l in enumerate(lines) if l.strip() == begin.strip())
    except StopIteration:
        return lines, False
    try:
        j = next(n for n, l in enumerate(lines[i:], i) if l.strip() == end.strip())
    except StopIteration:
        sys.exit(f"found the opening marker but not the closing one in the target; "
                 f"refusing to guess where the managed block ends")
    return lines[:i] + lines[j + 1:], True


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--src", required=True)
    ap.add_argument("--dst", required=True)
    ap.add_argument("--begin", required=True)
    ap.add_argument("--end", required=True)
    ap.add_argument("--dry", action="store_true")
    a = ap.parse_args()

    src = open(a.src).read().splitlines()

    # What does the policy claim ownership of?
    src_sections = split_sections(src)
    owned_tables = {table_root(h) for h, _ in src_sections if h}
    owned_keys = set()
    for head, body in src_sections:
        if head is None:
            for line in body:
                m = ASSIGN.match(line)
                if m:
                    owned_keys.add(m.group(1).strip("\"'"))

    existing = []
    if os.path.exists(a.dst):
        existing = open(a.dst).read().splitlines()
    elif a.dry:
        print("would-create")
        return

    existing, _ = strip_block(existing, a.begin, a.end)

    keep_pre, keep_tables = [], []
    for head, body in split_sections(existing):
        if head is None:
            # Drop the machine's copy of any key the policy owns; keep the rest
            # (Codex writes `notify` up here, and it must stay above all tables).
            #
            # A comment above an owned key is that key's documentation, so it
            # goes with it. Without this the old hand-written rationale outlives
            # the setting it described and drifts against the managed block.
            pending = []
            for line in body:
                m = ASSIGN.match(line)
                if m:
                    if m.group(1).strip("\"'") not in owned_keys:
                        keep_pre.extend(pending)
                        keep_pre.append(line)
                    pending = []
                elif line.strip().startswith("#") or not line.strip():
                    pending.append(line)
                else:
                    keep_pre.extend(pending)
                    keep_pre.append(line)
                    pending = []
            keep_pre.extend(pending)
        elif table_root(head) in owned_tables:
            continue  # the policy owns this table outright
        else:
            keep_tables.extend(body)

    def trim(block):
        while block and not block[0].strip():
            block.pop(0)
        while block and not block[-1].strip():
            block.pop()
        return block

    out = trim(keep_pre)
    if out:
        out.append("")
    out += [a.begin] + src + [a.end]
    tail = trim(keep_tables)
    if tail:
        out += [""] + tail
    text = "\n".join(out) + "\n"

    if os.path.exists(a.dst) and text == open(a.dst).read():
        print("unchanged")
        return
    if a.dry:
        print("would-merge")
        return

    os.makedirs(os.path.dirname(a.dst) or ".", exist_ok=True)
    tmp = a.dst + ".dots-tmp"
    with open(tmp, "w") as fh:
        fh.write(text)
    os.replace(tmp, a.dst)  # atomic: never leave a half-written config
    print("merged")


if __name__ == "__main__":
    main()
