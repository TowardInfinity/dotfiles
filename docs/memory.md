---
title: Memory
group: dots
order: 35
summary: One memory across every AI tool on the machine, keyed by project rather than by directory
---

# Memory

Five AI tools run on this account and each keeps its own history. None can see
another's, so opening a new chat in a repo you have worked in for weeks starts
from nothing. `dots memory` reads what those tools already write to disk,
distils each finished session with local Ollama, and files the result under a
**project** key that every tool agrees on.

```sh
dots memory status     # what is indexed, by project and by tool
dots memory reindex    # rescan every tool now, ignoring the idle gates
dots memory recall     # the short digest for the current project
dots memory search     # find sessions whose title or summary mentions a term
dots memory capture    # what the Stop hook runs; silent and cheap
dots memory mcp        # stdio MCP server for Codex, Claude, Grok, Cursor
```

## Project identity is not the directory

Keying on the working directory breaks across worktrees, second clones and
other machines — and it is already wrong here: a Grok session in
`~/Codes/Projects/screener` has the remote `TowardInfinity/almanac`. The
directory name and the project disagree on this machine today.

Resolution order:

1. `git remote get-url origin`, normalised to `host/owner/repo` — scheme,
   userinfo and `.git` are stripped, so `git@github.com:o/r.git` and
   `https://github.com/o/r` collapse to one key
2. otherwise the basename of `git rev-parse --show-toplevel`
3. otherwise the absolute working directory

`cwd`, `git_root`, `remote` and `branch` are all kept as fields. The key is what
recall groups by; the rest is what tells you which clone it was.

## Three tiers

| tier | what | where | injected? |
|---|---|---|---|
| raw | tool-native transcripts | left in place, never copied | never |
| distilled | one note per session | the Obsidian vault | on demand |
| index | pointers and summaries | `~/.cache/dots/` | the capped digest only |

Only the distilled tier is ever injected, and only under a hard byte cap.
Injected context is re-read on every turn of every session, so an uncapped
digest becomes a standing charge on the account — see
[cost](models-cost.md). The cap is enforced in code, not by convention: the
digest stops adding entries before it crosses the budget, and prints nothing at
all rather than a header with nothing under it.

The index is derived state. Delete it and `dots memory reindex` rebuilds it
from the transcripts and the vault. It is deliberately not synced between
machines; the vault is what travels.

## Recall is small on purpose; search is what goes deeper

`dots memory recall` is what `session-start.sh` calls automatically, every
session — a handful of one-line titles, under the byte cap above. It is
deliberately too small to answer a real question; it exists to remind you
something is there, not to be the interface to it.

`dots memory search <term>` is the on-demand counterpart: a case-insensitive
scan over every Title and Summary in the index, optionally scoped with
`--project`. It reads the same prebuilt index recall does — no Ollama call, no
disk walk of transcripts — so at the corpus sizes this reaches (a few hundred
sessions, roughly a kilobyte of text each) it is comfortably sub-millisecond.
`--json` gives the same results as a structured array, which is what the MCP
tools below hand to a model directly rather than reformatting.

## The MCP server — the same memory, reached directly by a model

`dots memory mcp` is a stdio JSON-RPC server exposing four tools:

| tool | does |
|---|---|
| `memory_search` | the same scan as `dots memory search`, trimmed to titles, dates and a short snippet — never the full summary body, for the same reason `recall` is capped: a search result is a model's own tool output, re-read on every later turn of the same session |
| `memory_timeline` | recent sessions, optionally scoped to one project |
| `memory_get` | one session's full record, including the vault note's body when there is one |
| `memory_remember` | write something worth keeping — a decision, a fact — straight into the index and the vault, redacted the same way capture redacts a transcript |

It is hand-rolled over `encoding/json` rather than built on
`github.com/modelcontextprotocol/go-sdk`: the SDK was the first choice, but
the Go module proxy was unreachable when this was built, and the surface
actually needed (`initialize`, `tools/list`, `tools/call`) is small. Swapping
in the SDK later is a contained change — nothing outside `mcp.go` knows the
wire format exists.

### Registering it

Requires `dots` on `PATH` first — none of these do anything useful until
`install.sh --build --apply` has put the current binary there.

**Codex** needs nothing further: `[mcp_servers.dots-memory]` ships in
`common/codex/config.policy.toml`, which `install.sh`/`dots sync` already
merges into `~/.codex/config.toml` on every machine.

**Claude Code** (per machine — this writes to the untracked `~/.claude.json`,
not something this repo tracks):

```sh
claude mcp add -s user dots-memory -- dots memory mcp
```

**Grok** (per machine, `~/.grok/config.toml`):

```sh
grok mcp add dots-memory -s user dots -- memory mcp
```

**Cursor** has no CLI for this — add the block by hand to
`~/.cursor/mcp.json` (create the file if it does not exist):

```json
{
  "mcpServers": {
    "dots-memory": {
      "command": "dots",
      "args": ["memory", "mcp"]
    }
  }
}
```

## Cadence — why capture does almost nothing

`Stop` fires at the end of **every assistant turn**, not once per session. The
Python hook this replaces treated it as end-of-session, so it re-sent the whole
24 000-character transcript to Ollama every turn and dated each note with
*today*. The vault holds 70 notes for 39 sessions as a result; one session has
eight.

So capture distils only sessions whose transcript has stopped changing
(20 minutes by default). This is an idle gate rather than a debounce on
transcript growth, because a byte-delta debounce still summarises a long
session ten or twenty times. Waiting for the transcript to go quiet summarises
it once, and behaves identically whether the trigger was `Stop`, `SessionEnd`
or a manual reindex. A consequence worth stating: **the session you are in is
never summarised**, which is correct — you are still in it.

Two further bounds, because "cheap" has to hold on a machine with a hundred
sessions and Codex rollouts running to tens of thousands of lines:

- a pass reads at most 40 transcripts and makes at most 3 Ollama calls
- a session read once and judged too slight to describe is recorded as such, so
  it is not read again until it actually grows

Passes are single-flight through a lockfile in the cache directory. The hook is
`async`, so two can otherwise overlap, summarise the same session twice, and
have the later `SaveIndex` discard the earlier one's work.

## Notes in the vault

One note per session at `AI Chats/<Tool> Chats/<date> <id> - <title>.md`, dated
by when the session **started**. That is what makes the path a stable function
of the session rather than of when capture happened to run.

Stable, but not immutable: any improvement to how titles are extracted moves the
path. So every full scan reconciles the vault against the index — matching notes
by the `session:` field in their frontmatter, renaming one to the current
canonical path, and adopting its prose back into the index if there is no
summary yet. Adoption is why backfilling project frontmatter into the existing
notes costs no Ollama calls: they were written by the same model from the same
prompt.

Duplicates are **moved to `AI Chats/_superseded/`, never deleted.** The vault is
iCloud-synced and git-backed, so a deletion reaches every machine on the account
before anyone can look at it. Emptying that folder is a decision for a person.

Reconciliation only touches files whose frontmatter carries a `session:` field.
Everything else in the vault — hand-written notes, other tools' exports — is
walked past untouched.

## Experimental adapters — Cursor and ChatGPT

`--experimental` (on `capture`, `reindex`) adds two more adapters. Off by
default: both tools store their history in undocumented formats that can
change without notice, and a parse failure there must never be able to stop
Claude, Codex or Grok being captured.

**Cursor** reads real plaintext — `~/.cursor/chats/<hash>/<uuid>/meta.json`
for the title, cwd and timestamps Cursor already generated, and
`prompt_history.json` for the raw strings typed into it — but that is all it
reads. The same directory also holds `store.db`, a SQLite file whose `blobs`
table looks like a content-addressed transcript store (each blob's id is the
SHA-256 hash of its own bytes), but `meta.json` also carries a
`blobEncryptionKey`, and every blob inspected while building this adapter was
genuinely encrypted with it. So a Cursor note is built from what the user
typed and nothing else — the assistant's replies are not recoverable. The
summarizer is told this explicitly, in the transcript text itself, so it
describes what was asked rather than inventing what happened next.

**ChatGPT desktop** is not implemented at all, and not because no one got to
it. Every conversation file under
`~/Library/Application Support/com.openai.chat/conversations-v3-*/*.data` is
high-entropy from byte zero — no JSON, no plist, no SQLite header, nothing
`file(1)` can agree on twice across sibling files in the same directory. That
is what encrypted-at-rest content looks like. `ChatGPTAdapter.Available()`
still reports the directory honestly, so `dots memory status` shows the tool
as present rather than silently missing, but `Scan()` returns nothing:
writing a parser here would mean reverse-engineering the app's key handling,
which is out of scope.

## Project index pages

Every project with at least one indexed session gets a page at
`AI Chats/Projects/<project>.md` — not a written list, but a
[Dataview](https://blacksmithgu.github.io/obsidian-dataview/) query scoped to
that project's `project:` frontmatter, sorted newest first. A list goes stale
the instant a new session lands and no one remembers to update it; a query
does not, so these pages are rewritten whole on every scan rather than
reconciled the way session notes are. Deleting one costs nothing — the next
`dots memory reindex` regenerates it.

## Redaction

Every built-in pattern from the original hook is ported: Anthropic, OpenAI,
GitHub, Slack, AWS, Google and Stripe keys, JWTs, PEM blocks, emails, PAN and
Aadhaar numbers, plus `KEY=value` assignments (the key survives, the value does
not) and any literal terms in `~/.claude/hooks/redactions.txt`.

One behavioural change from the original: **redaction happens before
summarisation**, not after. The Python version sent the raw transcript to Ollama
and redacted only the note it got back, so a secret the model reworded past the
regex would land in the vault. Here `Scrub` is the only way to produce the
`Clean` type, and the summariser and the vault writer accept nothing else — the
ordering is checked by the compiler rather than remembered by a person.

Titles are redacted at the ingestion boundary, because a title becomes a
*filename*: a leaked key there shows up in directory listings, in the vault's
git log and in iCloud sync metadata, which is worse than one in a body.

## Hook safety

Both hooks must exit 0 on every path — a failing `SessionStart` hook stops
Claude launching at all, and a failing `Stop` hook interrupts a session over
background bookkeeping. So:

- the shim checks `command -v dots` before `exec`, which is what makes the
  shipped config safe on a box that has no `dots` yet
- `dots memory capture` returns 0 even when it fails, and says nothing unless
  given `--verbose`
- `dots memory recall` takes a `--deadline` (2 s by default) after which it
  prints nothing and exits 0. A stalled iCloud read, a half-written index or a
  missing vault all degrade to silence, never to a hung launch. It reads the
  prebuilt index and never invokes Ollama.

## Configuration

| setting | effect |
|---|---|
| `DOTS_MEMORY_VAULT` | the Obsidian vault root directory. Set per machine by `install.sh`; notes are organized under `AI Chats/<Tool> Chats/` within it. A vault is optional — a fleet box without one keeps the index and writes no notes. |
| `~/.claude/hooks/redactions.txt` | extra literal terms to mask, one per line |

No vault path is compiled into the binary. That is the mistake
[install.sh](install.md) warns about, and the one the Python hook made.
