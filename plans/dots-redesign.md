# dots redesign — product, interaction, and command plan

Status: proposed; no implementation has started

Date: 2026-08-09

## Executive decision

Rebuild `dots` around one navigation model and one operation model.

- Keep Go and Bubble Tea. Upgrade Bubble Tea, Bubbles, Lip Gloss, and Glamour
  to their stable v2 lines in an isolated foundation change before redesigning
  the UI.
- Replace the three top tabs plus Manage's nested rail with one global route
  sidebar: Overview, Changes, Fleet, Health, Services, Packages, Projects,
  and Docs.
- Make keyboard and mouse two ways of issuing the same named actions. No
  feature may be mouse-only or hidden behind an unexplained letter.
- Make `dots sync` inbound-only: fetch, fast-forward when safe, apply on this
  machine, and verify. It must never stage, commit, push, or contact another
  machine.
- Split outward mutations into `dots publish` and `dots rollout`. Publishing
  changes the canonical repository; rollout applies an already-published
  revision to selected machines.
- Put every mutation through a shared plan/review/run/result pipeline used by
  both the TUI and CLI. Views must not construct shell commands themselves.
- Default to a compact, quiet visual system: one header row, one footer row,
  few borders, terminal-background transparency, restrained Tokyo Night
  accents, and semantic state shown with text and shape as well as color.

The redesign is not a reskin. The command semantics, information architecture,
focus behavior, async loading, confirmation rules, and layout engine change as
one product.

## Why the current app feels patched

The current code is careful and well tested, but its shape records the order in
which features arrived.

| Current behavior | Result |
|---|---|
| Docs, Doctor, and Manage are top-level tabs, while Manage contains another six-section rail | Two unrelated navigation systems must be learned |
| Docs uses horizontal and vertical movement to change topics; Manage reserves horizontal movement for sections and vertical movement for rows | The same key means a different spatial operation depending on the route |
| Mouse reporting is globally enabled, but only Docs handles the wheel | The app looks clickable but is mostly inert |
| Each section owns raw key-to-command logic | Similar operations differ in confirmation, naming, refresh behavior, and discoverability |
| `dots sync` stages, commits, pushes, and then SSH-updates every machine | A safe-sounding default verb performs the broadest outward mutation in the product |
| CLI update logic and Manage's update action are separate implementations | Behavior can drift between the two front doors |
| The app starts checks for Doctor and all six Manage sections together | Startup performs work for screens the user may never open, including SSH and package discovery |
| Layout safety is enforced through many local width caps, border corrections, truncations, and final clamps | Each fix is reasonable, but geometry has no single owner and the rendered result feels constrained rather than composed |
| The footer tries to expose a variable list of direct letter bindings | It either becomes crowded or hides important actions |

Specific evidence in the current tree:

- Root tabs and global input routing: `cmd/dots/app.go`.
- Per-pane bindings and the Manage navigation contract: `cmd/dots/keys.go`.
- Nested Manage sections and direct action construction: `cmd/dots/manage.go`.
- Mouse handling only in Docs: `cmd/dots/docs.go` and `cmd/dots/main.go`.
- Shared geometry workarounds and fixed rails: `cmd/dots/layout.go`.
- Current push-plus-rollout meaning of sync: `cmd/dots/sync.go`.
- Duplicate update implementations: `cmd/dots/actions_cli.go` and
  `cmd/dots/manage.go`.

There are also two smaller inconsistencies to remove during migration:

- Doctor's config repair can run without a confirmation while Manage's relink
  asks first.
- `README.md` still says the Go TUI uses Glow and fzf opportunistically; the
  current Go path renders with Bubble Tea and Glamour.

## Reference products and what to borrow

The goal is not to imitate one screenshot. It is to combine interaction
patterns that have survived real use.

### Posting

Closest interaction reference. Posting has a searchable command palette,
focus-local help, predictable Tab navigation, jump mode, and genuine
keyboard/mouse parity.

Borrow:

- `Ctrl-P` command palette.
- `?`/F1 contextual help based on the focused component.
- Tab and Shift-Tab as focus movement, not route switching.
- Click-to-focus, wheel-over-hovered-region, and draggable/clickable controls
  where the terminal protocol makes them reliable.
- Compact and comfortable density modes, with compact as the `dots` default.

Sources:

- <https://posting.sh/guide/navigation/>
- <https://posting.sh/guide/command_palette/>
- <https://posting.sh/guide/help_system/>

### Lazygit

Lazygit succeeds because selection, context, detail, and available actions are
visible together. It also scopes actions to the currently focused item.

Borrow:

- List-detail layouts where selection immediately explains itself.
- Context actions in a stable footer.
- Review-before-mutation flows and visible command progress.

Do not borrow its maximum pane density. `dots` has fewer objects and should be
quieter.

Source: <https://github.com/jesseduffield/lazygit>

### K9s

K9s makes a large surface navigable through a command mode and keeps a real
read-only mode.

Borrow:

- `:` as an alias for the searchable action/route palette.
- Human command names and aliases rather than requiring route-number memory.
- Explicit read-only versus mutating actions.

Source: <https://k9scli.io/topics/commands/>

### Yazi

Yazi treats each interaction layer—manager, input, confirmation, help—as a
separate keymap context. That is the right cure for keys leaking through
filters and overlays.

Borrow:

- Binding actions by stable action ID inside explicit input contexts.
- A hierarchy of overlay/input/screen/global handling.
- Optional multi-key route chords later, only if real use justifies them.

Source: <https://yazi-rs.github.io/docs/configuration/keymap/>

### btop

btop demonstrates that a terminal UI can be mouse-complete without becoming
mouse-dependent: visible key labels are also click targets, and the wheel acts
on the relevant list.

Borrow:

- Clickable visible actions.
- Mouse scrolling in every scrollable region.
- Shape-plus-color status communication.

Source: <https://github.com/aristocratos/btop>

## Framework decision

### Keep Bubble Tea

The problem is not Elm-style update/view architecture. The problem is that the
current root model grew multiple local interaction systems around it.

Keeping Bubble Tea preserves:

- the single cross-compiled Go binary;
- the existing service, package, SSH, health, signing, and repository logic;
- the action streaming and cancellation work;
- the current Go test suite and macOS/Linux release pipeline;
- the no-runtime requirement on the light servers.

Textual would add a Python runtime to a product deliberately distributed as a
single binary. Ratatui would require a Rust rewrite with no user-facing gain.

### Upgrade the Charm stack mechanically before the UI rewrite

The repository currently uses Bubble Tea 1.3.10, Bubbles 1.0.0, Lip Gloss 1.x,
and Glamour 1.x. The stable v2 stack provides a newer renderer, declarative
terminal modes, better keyboard disambiguation on Ghostty and other modern
terminals, improved mouse handling, color downsampling, and better viewport
wrapping.

This upgrade must be one isolated change with zero intended product behavior
change. It should not be mixed into the UI rewrite, so any renderer or input
regression has one obvious cause.

Source: <https://github.com/charmbracelet/bubbletea/releases/tag/v2.0.0>

## Product model

`dots` manages four different scopes. The interface and command names must say
which scope an operation touches.

| Scope | Meaning | Examples |
|---|---|---|
| Observe | Read state without changing it | status, doctor, diff, search |
| Local machine | Change this checkout or this machine only | sync inbound, apply links, install dependencies, restart a local service |
| Canonical repository | Publish local changes to origin | commit and push |
| Fleet | Change one or more remote machines | rollout, remote doctor |

No command may silently cross from one scope to another. In particular:

- inbound sync never publishes;
- publish never rolls out;
- rollout never creates or pushes a commit;
- apply never installs packages;
- Doctor never repairs until a repair action is explicitly selected.

## Information architecture

Use one global route tree. Remove the top tab bar and the Manage sub-rail.

```text
OVERVIEW
  Overview

WORKSPACE
  Changes

FLEET
  Machines

THIS MACHINE
  Health
  Services
  Packages
  Projects

REFERENCE
  Docs
```

User-facing route labels should be short:

1. **Overview** — what needs attention now.
2. **Changes** — repository, local edits, incoming commits, apply, sync, and
   publish.
3. **Fleet** — reachability, commit, binary, trust state, Doctor summary, and
   rollout.
4. **Health** — current Doctor checks and repairs.
5. **Services** — discovered launchd/systemd/Docker services.
6. **Packages** — installed and outdated packages across managers.
7. **Projects** — repositories and tmux sessions.
8. **Docs** — topics, rendered Markdown, outline, and search.

The starting route is Overview, not Docs. A maintenance application should
first answer “is everything okay?” Docs remain one key or click away.

## Application shell

### Wide layout (120 columns and above)

```text
┌ dots ───────────────────────── Changes ───── mac · main · v0.1.15 ┐
│ OVERVIEW        │ local changes             │ README.md            │
│  Overview       │ ● README.md            M  │ @@ -31,7 +31,7 @@    │
│                 │ ● cmd/dots/app.go       M  │ - old                │
│ WORKSPACE       │ ○ plans/redesign.md     ?  │ + new                │
│ ▌Changes        │                           │                      │
│                 │ incoming                 │                      │
│ FLEET           │ 2 commits from origin     │                      │
│  Machines       │                           │                      │
│                 │                           │                      │
│ THIS MACHINE    │                           │                      │
│  Health         │                           │                      │
│  Services       │                           │                      │
│  Packages       │                           │                      │
│  Projects       │                           │                      │
│                 │                           │                      │
│ REFERENCE       │                           │                      │
│  Docs           │                           │                      │
├─────────────────┴───────────────────────────┴──────────────────────┤
│ ↑↓ move  enter inspect  a actions  / filter  ? help               │
└────────────────────────────────────────────────────────────────────┘
```

### Standard layout (76–119 columns)

- Keep the 20-column route sidebar.
- Use one main region.
- Open selected-item detail as a drill-in screen or overlay instead of a
  permanently squeezed third column.
- Never compress a five-column table until every field becomes unreadable;
  progressively remove low-priority columns and expose them in detail.

### Compact layout (below 76 columns)

- Hide the sidebar.
- Show a one-line breadcrumb and one focused region.
- `Esc`/Left goes back to the route list or previous level.
- The command palette remains available.
- Tables become labeled rows, not horizontally truncated tables.

### Too-small layout

Below 44×14, render a stable minimal message with the current route and the
minimum useful size. Do not lie to components by inflating dimensions and then
clip their output; that is how terminal layouts become distorted.

## Visual system

### Character

Quiet, dense, and precise—not decorative dashboard cards.

- Default density: compact.
- One row for the header and one for the footer.
- One-column inner gutters; no default two-column padding around every pane.
- Use borders only for major region separation and modals.
- Use the terminal background by default; apply a surface background only to
  selected rows, focused controls, menus, and dialogs.
- Cap prose around 88 columns, but let data tables use available width.
- Every list row is exactly one terminal row unless it is an explicit detail
  view.
- No Nerd Font requirement. Use basic Unicode with an ASCII fallback.

### Palette

Keep Tokyo Night because it ties Ghostty, tmux, Neovim, and `dots` together,
but reduce its use.

- Blue: focus and primary selection only.
- Green: healthy/succeeded only.
- Yellow: attention, pending, or partial only.
- Red: failed, stopped unexpectedly, or destructive warning only.
- Violet/cyan: sparingly for keycaps and links.
- Muted neutral: structure and metadata.

Add a light-background palette and `NO_COLOR`/16-color degradation. Continue
using symbols and words (`✓ healthy`, `! partial`, `× failed`) so color is never
the only signal.

### Component states

Every component uses the same states:

- loading: skeleton text or spinner in place, never a whole-screen blank;
- empty: explain what was searched and give the next useful action;
- stale: show when the data was last refreshed;
- warning: action can proceed, but the uncertainty is named;
- error: concise message plus `Details`, `Retry`, and `Copy` actions;
- success: result summary with verification, not only “done.”

## Navigation and focus model

### One rule per axis

- Up/Down or `j`/`k`: move vertically inside the focused region.
- Left/Right or `h`/`l`: move between adjacent regions or enter/back out of a
  list-detail hierarchy.
- Tab/Shift-Tab: move focus among interactive regions.
- Enter: inspect or perform the primary non-destructive action.
- Space: toggle selection in a selectable list.
- Esc: unwind one layer in this order: close modal, cancel input, clear filter,
  close detail, return to route list. It never quits.
- `q`: quit only at the base application layer; input fields and dialogs own
  it while focused.

### Global bindings

| Key | Action |
|---|---|
| `Ctrl-P` or `:` | Open searchable command/action palette |
| `?` or `F1` | Contextual help for the focused region |
| `/` | Filter/search the focused collection |
| `a` | Open actions available for the current route/selection |
| `r` | Refresh the current route |
| `Tab` / `Shift-Tab` | Next/previous focus region |
| `Esc` | Back or close one interaction layer |
| `q` / `Ctrl-C` | Quit at base; cancel safely when an action owns input |
| `g g` / `G` | First/last row |

Remove numeric top-level navigation. Eight route numbers are not a mental
model. Do not replace them with eight route chords in the first release: the
sidebar and searchable command palette are sufficient, and accelerators added
before the route vocabulary settles would be another table to memorise. Keep
`g g`/`G` for first/last-row movement because those describe position rather
than navigation. Reconsider route chords only after real use shows a repeated
need.

### Footer

The footer shows at most four high-value actions for the focused component,
plus `? help`. Full bindings live in contextual help and the command palette.
It must never wrap. Visible footer actions are clickable.

### Command palette

The palette searches both navigation targets and actions by human wording:

```text
> sync

  Go to Changes
  Sync this machine from origin       LOCAL
  Roll out origin/main to machines    FLEET
  Read “Updating”                     DOC
```

Each result carries a scope/risk label. Selecting a mutation opens its plan; it
does not immediately run it.

## Mouse contract

Mouse behavior must be complete enough that the UI does not advertise a false
GUI.

- Click a sidebar entry to navigate.
- Click a region to focus it.
- Click a row to select it.
- Click a visible action/button to invoke the same action ID as its key.
- Wheel scrolls the region under the pointer, not merely whichever route owns
  a hardcoded mouse handler.
- Scrollbar click/drag is desirable after the core hit-testing model exists,
  but is not required for the first redesign release.
- A click on a destructive action opens review/confirmation; it never executes
  directly.
- No right-click dependency and no mouse-only action.
- Request the least intrusive terminal mouse mode that supports clicks and
  wheel events. Document the terminal modifier for native text selection.

Implementation should build a hit-map while rendering. Components register
rectangles against action or focus IDs; the root routes mouse coordinates
through that map. Do not duplicate coordinate arithmetic in every screen.

## Screen specifications

### Overview

Purpose: show only state that can change the next decision.

Order:

1. **Needs attention** — local changes, incoming commits, unhealthy checks,
   mismatched config, unreachable rollout targets, outdated release.
2. **Workspace** — branch, clean/dirty, ahead/behind, applied commit.
3. **Fleet** — current/total machines and revision agreement.
4. **This machine** — health count, services running, outdated packages.

Each row navigates to the route that owns the detail. Do not put mutation
buttons on Overview; it is a triage page, not a second copy of every screen.

First paint should use local cached/cheap state. SSH, package inventories, and
deep checks fill in asynchronously after the interface is visible only under
the TTL, invalidation, and retry rules in Async data.

### Changes

Purpose: make inbound, local, and outbound repository state understandable.

Wide view:

- left/main list grouped as Local changes and Incoming commits;
- right inspector containing selected file diff or commit summary;
- visible actions: Sync, Apply, Publish.

Behavior:

- `/` filters files and commits.
- Enter opens detail; Space selects files for a publish plan.
- Local files show tracked/untracked/staged state explicitly.
- Incoming commits are learned by fetch and never auto-merged merely by
  opening the route.
- The diff renderer handles binary files, renamed files, and long lines without
  breaking frame geometry.
- If local state is dirty and origin is ahead, explain the conflict of intents
  and offer Review local changes or Stash manually—not an automatic stash.

### Fleet

Purpose: know what is actually running where and roll out deliberately.

Columns, removed from right to left as width shrinks:

- machine alias;
- reachable state;
- checkout commit;
- `dots` version/source;
- signing-key agreement;
- config health summary;
- last checked age.

Behavior:

- Space selects machines.
- Enter opens the machine inspector.
- Actions: Check selected, Roll out selected, Open SSH command.
- `orb`-style aliases are visible but can be marked ignored for fleet rollout
  in machine-local configuration instead of being rediscovered and skipped on
  every run.
- Rollout results distinguish Updated, Already current, Unreachable, Dirty
  checkout, Verification failed, and Command failed.

### Health

Keep the current Doctor groups: Core, Tools, Frameworks, Config, Packages.

- Default filter is Problems when failures exist, otherwise All.
- Enter shows evidence, expected state, and repair scope.
- `a` exposes Repair selected and Repair all repairable.
- Warnings never affect exit status; failures do.
- Release/config verification remains independent from optional package gaps,
  so a missing `pnpm` cannot make a rollout look like a signing failure.

### Services

- One list with source, state, optional health/port, and concise detail.
- Default filter: running plus failed; All is one filter choice, not an
  unrelated letter toggle.
- Enter opens details/logical identity.
- Action menu offers Start, Stop, Restart only when supported.
- All mutations name the backend (`launchd`, `systemd --user`, Docker), target,
  expected state transition, and verification step.

### Packages

- Outdated packages appear first by default; current packages remain available.
- Manager is a filter menu/chip, not a cycle whose next value must be memorized.
- Selected package inspector shows manager, installed/latest version, source,
  advisory, and exact upgrade mechanism.
- Upgrade selected and Upgrade all are separate plans. The latter groups work
  by manager and stops one manager's failure from disguising the others.

### Projects

- List name, branch, dirty/ahead state, and tmux state.
- Enter is Open/Switch and remains non-destructive.
- Inside tmux, switch client. Outside tmux, use Bubble Tea's process-suspend
  mechanism where reliable; otherwise show and copy the exact command.
- Search is always available; project roots become data/config rather than
  hardcoded presentation logic.

### Docs

Retain the useful three-part wide layout—topics, rendered page, outline—but
place it inside the global application shell.

- Topic movement is vertical only.
- Enter selects/opens a topic; Left returns to topics from content.
- `/` searches title, summary, and body.
- Wheel scrolls whichever docs region is under the pointer.
- Same-page heading navigation remains; external links can offer Copy URL/Open
  URL actions.
- Fix the README's stale Glow/fzf description as part of the migration docs.

## New CLI contract

### Core commands

| Command | Scope | Contract |
|---|---|---|
| `dots` | Observe/local actions via TUI | Open Overview |
| `dots status [--online] [--fleet] [--json]` | Observe | Concise state; never repair |
| `dots doctor [--online] [--json]` | Observe | Detailed health with scriptable exit status |
| `dots sync [--check] [--dry-run]` | Local inbound | Fetch origin, fast-forward safely, apply, verify; never push or SSH |
| `dots apply [--dry-run]` | Local | Relink and merge from current checkout; no package install and no network |
| `dots deps [--dry-run] [-y]` | Local/network | Install missing dependencies explicitly |
| `dots publish [paths…] [-m message] [--all] [--dry-run]` | Canonical repo | Review, validate, commit selected changes, push; never roll out |
| `dots rollout [hosts…] [--all] [--revision sha] [--dry-run] [-y]` | Fleet | Apply an already-published revision to selected machines; never commit or push |
| `dots docs [topic]` | Observe | List or print docs |
| `dots search`, `edit`, `path`, `version` | Existing scopes | Keep current pipe-friendly behavior |

### `dots sync`

This is the most important semantic change.

Algorithm:

1. Resolve the checkout and current branch; refuse detached HEAD.
2. Fetch the named origin branch once.
3. Compute local versus `origin/<branch>`.
4. If current: run no Git mutation; optionally verify applied config.
5. If behind and clean: show the incoming commits, fast-forward only, then run
   Apply and config verification.
6. If dirty, ahead, or diverged: refuse with a precise explanation and route
   to Changes. Never auto-stash, rebase, commit, reset, or push.
7. If Apply fails after the fast-forward, retain the checkout revision, report
   the failed step, and offer `dots apply` retry. Existing installer backups
   remain the recovery boundary.

Flags:

- `--check`: fetch and report only; do not change the worktree or configs.
- `--dry-run`: produce the complete plan after fetch, then stop.
- `--json`: optional only when the operation result schema is stable.

Invariant test: no execution path from `dots sync` may invoke `git add`,
`git commit`, `git push`, or `ssh`.

### `dots publish`

TTY behavior with no paths opens a focused file-selection flow. Already staged
files start selected; other files are visible but not silently added.

Non-interactive behavior requires explicit paths or `--all`, a commit message,
and `-y`. This prevents a script from inheriting today's `git add -A` behavior
without stating its scope.

Preflight:

1. Fetch origin and refuse if the branch is behind or diverged.
2. Show selected files and diff summary.
3. Run fast validations appropriate to changed file types: JSON/TOML parse,
   shell syntax, Go formatting/build/tests when Go files changed, and the
   repository self-test for installer/resolver changes.
4. Stage only the selection, commit, and push the current branch.
5. Report the pushed commit and explicitly say the fleet was not changed.

Offer `--no-verify` only as an explicit escape hatch with a warning. Never make
it the default.

### `dots rollout`

TTY behavior with no hosts opens a machine selector. Non-interactive behavior
requires named hosts or `--all` plus `-y`.

The plan pins a commit, not “whatever main is when each host happens to pull”:

1. Fetch origin locally.
2. Resolve the requested revision, defaulting to `origin/main`.
3. Verify the revision is on origin.
4. For each selected host, fetch and fast-forward to that exact revision, then
   Apply.
5. Verify checkout commit, config managed block, binary signing-key agreement,
   and release-binary provenance. Do not use all-package Doctor health as the
   rollout gate.
6. Print a result table and return a distinct partial result when any host is
   unreachable or skipped.

No remote dirty checkout is overwritten. It is reported as Drift blocked.

A canary is naturally expressed as `dots rollout a1`; the remaining hosts can
then be selected in a second run. A special canary state machine is unnecessary.

### `dots apply`

Run the linking and managed-block merge from the current checkout.

- No network.
- No dependency installation.
- No confirmation when every operation is already idempotent/no-op.
- If a real file must be moved to a backup or a managed file changes, show the
  plan and ask in a TTY; require `-y` without one.
- Verify symlinks, mode, and managed block after execution.

### `dots deps`

This owns package managers, upstream installer scripts, `sudo`, and the
documented third-party trust boundary. It is deliberately separate from Apply.
Always show sources and ask before running unless `-y` is explicit.

## Compatibility and migration

Command semantics migrate through two releases, but elapsed release count is
not the safety gate. A cached older binary keeps its old command meanings until
that machine is explicitly reinstalled and relinked. Each transition completes
only when the Mac, a1, v1, and v2 pass the same version-and-provenance probe.

### Compatibility release

- Complete the mutation operation engine: typed actions, plans, steps, results,
  runner, and the providers those mutations require. Do not split a temporary
  version of this core around the Charm migration.
- Add `apply`, `deps`, `publish`, `rollout`, and inbound-sync planners.
- Rewire every existing CLI and TUI mutation to dispatch action IDs through the
  same planners and runner. Read-only screen data may remain in `package main`
  until its route migrates.
- Keep old `update` and `sync` behavior temporarily, but print exact migration
  messages before they act.
- Update docs and the shell fallback in the same release.
- Publish, roll out, and run the fleet convergence probe below. Do not begin the
  semantic flip until all four machines pass.

### Semantic-flip release

- `dots sync` becomes inbound-only.
- `dots update` becomes a deprecated alias for `dots sync`. Retain or remove the
  alias based on fleet convergence and actual use, not an arbitrary number of
  elapsed releases.
- Old sync flags hard-error with mappings instead of being reinterpreted:

```text
dots sync -m / --push-only     → dots publish
dots sync --remotes-only       → dots rollout
```

- Bare old `dots sync` cannot be safely aliased because its old outward scope
  is exactly what is changing. The new safer behavior wins, with a prominent
  release note.
- Publish, canary, roll out, and run the identical convergence probe again.

### Shell fallback contract

The checked-in `bin/dots` is a Bash availability and recovery tier, not a
second implementation of the operation engine. Its existing `update` is
load-bearing: it performs an inbound `git pull --ff-only`, runs `install.sh`,
and therefore invokes the resolver that can lift a machine from tier 4 back to
the signed Go binary. Keep that recovery path.

During the compatibility release:

- keep `update` as the inbound recovery command;
- make `sync`, `publish`, `rollout`, `deps`, `apply`, and their flags explicit
  command cases rather than allowing the default `*) show "$1"` docs lookup to
  misreport them as missing topics;
- refuse unsupported mutations non-zero, name the required Go command, and tell
  the user to run `dots update` to recover the real binary.

At the semantic flip, make bare fallback `sync` invoke the same inbound
pull/install recovery path and retain `update` as its deprecated alias. Any old
sync flag must refuse with the same `publish`/`rollout` mapping as the Go CLI.
The fallback must never implement commit, push, SSH rollout, package-manager, or
service mutations.

### Resolver cache retention

The release cache is currently unbounded: seven retained binaries consume about
93 MB on the Mac, and every signed release adds roughly another 14 MB. This is a
bounded-resource defect on v1/v2 and becomes more visible once rollout makes
small releases routine.

Retain the current verified release and one verified predecessor (**N=2**).
Everything older is unreachable through tier 1 and is dead weight. The previous
entry is intentional: it permits a manual rollback without downloading while
GitHub or the network is unavailable.

Implementation rules:

- Track `current-version` and `previous-version` explicitly; do not infer order
  from lexical filename sorting (`v0.1.10` sorts before `v0.1.9`) or from
  platform-specific `stat` flags.
- Before a download, a best-effort cleanup may remove stale entries to make room
  but must preserve `current-version` and the actual target of
  `~/.local/bin/dots`. On the first migration, having only the current entry
  until the next release succeeds is safer than guessing which old binary is
  the predecessor.
- After a new binary, digest, and signature-provenance marker have all landed,
  atomically move the old current version to `previous-version`, update
  `current-version`, then prune. Re-resolving the same version must preserve the
  existing predecessor.
- Remove only the recognised binary and matching `.sha256`/`.sig-ok` files for
  discarded versions. Never touch unrelated cache files, source builds, the
  checked-in Bash fallback, or a binary targeted by the installed symlink.
- Cleanup failure warns but does not turn a verified install into failure. A
  dangling installed symlink is never an acceptable way to meet N=2.

Regression tests populate a fake cache with several versions and cover first
migration, same-version resolution, failed download, live-symlink protection,
matched metadata removal, and preservation of unrelated files. After a normal
successful install the cache contains exactly the current and previous verified
release families.

### Fleet convergence probe

Set the expected tag, then run this exact probe locally and over SSH after both
transition releases. It verifies the running version, the resolver's selected
version, the installed symlink target, the checkout commit, and the signing key
that admitted the cached binary. `dots doctor --online` cannot replace this: it
reports binary source and key agreement but does not read `.sig-ok`.

Do not advance `main` between tagging and completing this gate. `dots update`
correctly pulls `main`, while the expected binary and commit below are pinned to
the tag; a newer checkout during the rollout would make the exact-commit check
fail and destroy the meaning of the convergence result.

```sh
EXPECTED_VERSION=vX.Y.Z
EXPECTED_COMMIT=$(git rev-parse "$EXPECTED_VERSION^{commit}")
if command -v sha256sum >/dev/null 2>&1; then
  EXPECTED_KEY=$(sha256sum keys/release.pub | awk '{print $1}')
else
  EXPECTED_KEY=$(shasum -a 256 keys/release.pub | awk '{print $1}')
fi

probe='
set -eu
expected_version=$1
expected_commit=$2
expected_key=$3
PATH="$HOME/.local/bin:$PATH"
cache="${XDG_CACHE_HOME:-$HOME/.cache}/dots"
version=$(dots version)
cache_version=$(cat "$cache/current-version")
target=$(readlink "$HOME/.local/bin/dots")
marker=$(cat "$cache/dots-$version.sig-ok")
repo=$(dots path)
commit=$(git -C "$repo" rev-parse HEAD)
printf "version=%s cache=%s target=%s commit=%s marker=%s\n" \
  "$version" "$cache_version" "$target" "$commit" "$marker"
test "$version" = "$expected_version"
test "$cache_version" = "$expected_version"
test "$target" = "$cache/dots-$version"
test "$commit" = "$expected_commit"
test "$marker" = "$expected_key"
'

sh -c "$probe" sh "$EXPECTED_VERSION" "$EXPECTED_COMMIT" "$EXPECTED_KEY"
for host in a1 v1 v2; do
  ssh -o BatchMode=yes "$host" \
    "sh -c '$probe' sh '$EXPECTED_VERSION' '$EXPECTED_COMMIT' '$EXPECTED_KEY'"
done
```

Any mismatch blocks the next release or phase. A machine on a source build or
shell fallback fails deliberately because its installed symlink does not point
at the expected signed cache entry.

## Shared operation architecture

Today a view often turns a letter directly into `actionSpec{Argv: ...}`. Replace
that with named intents and plans.

```text
keyboard / mouse / CLI
          │
          ▼
     Action registry
          │ action ID + typed target
          ▼
       Planner  ────── reads providers/state
          │
          ▼
  Plan {scope, risk, steps, targets, verification, rollback note}
          │
          ├── dry-run / JSON render
          ├── TUI review dialog
          └── CLI confirmation
                    │
                    ▼
              single Runner
                    │
                    ▼
          progress → result → refresh affected data
```

Suggested types:

```go
type Scope int // Observe, Local, Repository, Fleet
type Risk int  // ReadOnly, Reversible, Outbound, Disruptive

type Action struct {
    ID          ActionID
    Label       string
    Description string
    Scope       Scope
    Available   func(State, Target) Availability
    Plan        func(context.Context, State, Target) (Plan, error)
}

type Plan struct {
    Action       ActionID
    Scope        Scope
    Risk         Risk
    Summary      string
    Targets      []Target
    Steps        []Step
    Verification []Check
    Recovery     string
}
```

Rules:

- Actions use argv arrays or typed provider calls, never view-built `sh -c`.
- Plan construction performs no mutation.
- The runner allows one mutating operation at a time.
- Cancellation stops future steps and waits for the current process to exit.
- A result names completed, failed, skipped, and unverified steps separately.
- Refresh is driven by affected resource IDs, not “rerun every fetch after
  every overlay closes.”
- CLI and TUI format the same Plan and Result; neither reimplements behavior.

## Application architecture

Move the product out of a 10,000-line `package main` without a big-bang rewrite.

```text
cmd/dots/
  main.go                  dispatch only

internal/dots/
  domain/                  RepoState, Machine, Check, Service, Package, Project
  providers/               git, ssh, launchd, systemd, docker, package managers
  ops/                     action registry, planners, runner, results
  cli/                     parsing and text/JSON formatting
  app/                     routes, focus, overlays, async job supervision
  ui/
    layout/                Rect solver and responsive breakpoints
    theme/                 semantic tokens and color profiles
    components/            sidebar, table, list, inspector, palette, dialog
    screens/               one package/model per route
```

### Root state

```go
type App struct {
    Route       RouteID
    History     []RouteState
    Focus       FocusID
    Overlay     []Overlay
    Screens     map[RouteID]Screen
    Jobs        Supervisor
    Activity    []Result // session-local initially
    Layout      Layout
    Hits        HitMap
}
```

Input precedence is structural:

1. running action/confirmation;
2. command palette/help/dialog;
3. text input/filter;
4. focused component;
5. route;
6. global keys.

This replaces ad-hoc checks for whether individual panes happen to be
capturing input.

### Layout ownership

One responsive solver returns exact rectangles for header, sidebar, main,
inspector, footer, and overlay.

- Borders are counted inside their assigned rectangle.
- Every component receives its content rectangle, not the terminal size and a
  collection of implied subtractions.
- Every component must render exactly its assigned width and at most its
  assigned height.
- Render tests strip ANSI and assert both width and height at each breakpoint.
- Columns collapse according to priority metadata; callers do not hand-tune
  widths screen by screen.

### Async data

- Paint the shell immediately.
- Load only cheap Overview state at startup.
- Load route-specific providers on first entry.
- Persist fleet observations atomically at
  `${XDG_CACHE_HOME:-$HOME/.cache}/dots/fleet-status-v1.json`, mode `0600`.
  Store a schema version and, per host, only the alias, observed revision,
  binary version/source, signing-key/provenance result, outcome, `checked_at`,
  and `last_attempt_at`; do not cache SSH addresses or raw command output.
- Treat fleet observations as event-driven state with a 12-hour TTL. A cold
  cache starts one asynchronous probe. A fresh cache starts no SSH. A stale
  cache remains visible with its age and may refresh in the background only
  after its persisted retry backoff; an offline laptop must not retry four SSH
  connections every time Overview opens.
- `r` bypasses the TTL/backoff. Entering Fleet may explicitly refresh.
- Rollout updates each host entry only after its post-rollout verification has
  observed the exact revision, binary version/source, signing key, and cache
  provenance. An exit code alone is not authoritative; unreachable, skipped,
  failed, and unverified hosts remain explicit partial results.
- Cache other route results only where a named TTL and invalidation event are
  specified; do not introduce a generic cache whose freshness cannot be
  explained in the UI.
- Attach request generation IDs so stale refresh results cannot overwrite newer
  state.
- Cancel route-specific work when superseded where the underlying process can
  be stopped safely.
- Network state is always Unknown before it is known; never render unknown as
  clean or current.

## Test strategy

### Operation invariants

- `sync` can never call add/commit/push/ssh.
- `publish` can never call ssh or Apply on a remote.
- `rollout` can never stage/commit/push.
- `apply` can never reach the network or a package manager.
- Every outbound/disruptive action renders a plan before execution.
- Dirty/diverged repositories are preserved byte-for-byte on refusal.

Use temporary repositories with bare remotes for sync/publish tests and fake
SSH homes for rollout.

### TUI contract tests

Test at least:

- 44×14 minimum;
- 60×18 compact;
- 76×22 standard boundary;
- 100×30 standard;
- 120×32 wide boundary;
- 160×45 wide.

For every route and interaction state:

- no rendered line exceeds terminal width;
- total height never exceeds terminal height;
- focused element is visible;
- compact and standard layouts preserve all information through detail views;
- overlays take all input;
- text inputs prevent global/action leakage;
- async results reach only the intended request generation;
- no hidden action is advertised as clickable.

Run these property assertions across the full size/route/state matrix. Keep
full-screen golden maintenance deliberately small: exactly three canonical,
ANSI-stripped semantic layouts initially:

1. wide Fleet, exercising priority-based column collapse;
2. standard Changes with its review/confirmation dialog;
3. compact Docs with headings, a table, code, links, and narrow wrapping.

Overview remains property-tested rather than golden-tested because it
summarises every other route and would churn whenever any summary field changes.

### Keyboard/mouse equivalence

For every visible action, tests should drive its action ID once by keyboard and
once through the registered hit rectangle and assert the same plan is produced.

Test:

- click route;
- click/focus/select row;
- wheel over each scrollable region;
- click footer action;
- click confirm/cancel;
- click behind modal does nothing;
- resize invalidates and rebuilds the hit map.

### Accessibility and terminal coverage

- truecolor, 256-color, 16-color, and `NO_COLOR` token/component render checks;
- dark and light background palettes;
- ASCII fallback;
- Ghostty directly and through nested tmux;
- tmux 3.2a Linux path as well as current macOS tmux;
- pipe/non-TTY output remains unstyled and stable.

### CI and fleet validation

- Keep Ubuntu and macOS CI gates.
- Run unit, integration, render, and self-tests on both.
- Canary each signed release on the Mac first.
- Then a1, then v1/v2.
- Verify exact commit, binary version/source, signing key, cache provenance,
  managed config, and route smoke tests after rollout. The Phase -1 transition
  releases must use the literal fleet convergence probe in Compatibility and
  migration; version output alone is not sufficient evidence.

## Implementation sequence

Each phase must leave a releasable program. Do not maintain a months-long
parallel UI. Sizes below are relative scope, not calendar promises: **S** is one
bounded behavior and its tests, **M** crosses several files or one subsystem,
and **L** is a multi-subsystem tranche that should be split into reviewable
commits while preserving one phase gate.

From the Phase -1A merge until the legacy screens are deleted, the old
Docs/Doctor/Manage UI receives correctness, security, compatibility, and
regression fixes only, plus the explicitly mechanical framework migration in
Phase 0. New features and visual improvements land only in shared operations or
the new shell. This prevents implementing them twice.

### Phase -1A — complete mutation core and compatibility release (**L**)

1. Record the existing action matrix and add missing CLI/TUI behavior tests.
2. Introduce the complete mutation core: action registry, typed Plans/Steps,
   providers needed by mutations, Runner, progress, Result, and affected-state
   refresh metadata.
3. Move Apply/relink, dependency repair, Doctor repair, service actions, package
   actions, update, and the legacy outbound sync through that core. The legacy
   sync action is temporary but must still be typed, scoped, and visibly warned.
4. Implement the new inbound sync, publish, and rollout planners and invariants;
   add `status`, `apply`, `deps`, `publish`, and `rollout` CLI commands.
5. Rewire existing Manage and CLI handlers to dispatch action IDs. No mutation
   handler may construct an `actionSpec`, argv, or `sh -c` string in a view.
6. Apply the shell-fallback compatibility contract: keep inbound `update` as
   recovery and add explicit lifecycle-command refusals instead of docs-topic
   fallthrough.
7. In an isolated **S** commit, implement and regression-test the resolver's
   N=2 cache-retention contract before producing the compatibility release.
8. Publish a signed compatibility release, canary it, roll it to all four
   machines, and require the fleet convergence probe to pass.

Exit gate: CLI and the existing TUI produce the same Plans and Results for every
shared mutation; all four machines run the signed compatibility binary admitted
by the expected key.

If work stops here: the product has one reusable mutation architecture and safe,
explicit new verbs, a bounded release cache, and legacy bare `sync` still works
only with a prominent outbound-scope warning.

### Phase -1B — inbound-sync semantic flip (**S**)

1. Map bare `dots sync` to the inbound planner and delete the temporary legacy
   sync action.
2. Make old flags fail with exact `publish`/`rollout` replacements; retain
   `update` as a deprecated inbound alias.
3. Make fallback bare `sync` call its inbound pull/install recovery path and
   reject every old or unsupported mutation flag explicitly.
4. Publish a second signed release, canary it, roll it out, and require the same
   convergence probe to pass before any UI-framework work begins.

Exit gate: no binary or fallback reachable on the fleet gives bare `sync`
outbound behavior.

If work stops here: the dangerous command meaning is retired fleet-wide, and
the CLI/fallback safety model is complete without depending on the redesign.

### Phase 0 — mechanical Charm v2 port (**M**)

1. Capture current behavior and renderer baselines, especially representative
   Markdown headings, tables, code, links, and narrow wrapping through Glamour.
2. Upgrade Bubble Tea, Bubbles, Lip Gloss, and Glamour to their v2 module paths
   and make only the import/API changes required to compile and preserve
   behavior.
3. Do not refactor, rename, restyle, change navigation, or fix opportunistic
   defects in this phase. Put any discovered defect in a separate commit before
   or after the port.
4. Run current render/property tests at every breakpoint and compare the focused
   Markdown fixtures before and after.
5. Confirm binary sizes and cold-start time remain acceptable on all four
   targets.

Exit gate: existing behavior and tests are green on macOS/Linux under the v2
stack with no intended product change.

If work stops here: the current product is on the maintained framework and the
future shell starts directly on v2; no half-migrated program or dual Tea runtime
exists.

### Phase 1 — one shell and Docs (**L**)

1. Implement the route model, responsive rectangles, focus manager, overlays,
   hit map, contextual help, command palette, and global sidebar.
2. Move Docs into the shell first, preserving topic/body/outline behavior and
   validating the v2 Markdown renderer in the new geometry.
3. Make the new shell the only root Program. Until later routes migrate, wrap
   each legacy Doctor/Manage section as the body of its corresponding global
   route; do not render the old top tabs or Manage rail and do not retain an
   alternate legacy entry point.

Exit gate: one navigation/focus system owns the application, Docs is native,
and every existing feature remains reachable without a second UI.

If work stops here: navigation and layout have one owner, while the stable
legacy screen bodies remain usable behind the new routes.

### Phase 2 — Changes and Overview (**L**)

1. Build Changes with list/diff, selection, typed lifecycle actions, and the
   shared review dialog.
2. Implement the persisted fleet cache and post-rollout invalidation contract.
3. Build Overview last in this phase so each summary row links to an existing
   owner or an explicitly labelled compatibility route.
4. Make Overview the start route.

Exit gate: the primary edit → review → publish → rollout workflow works through
the new shell with keyboard or mouse, and Overview never renders unknown or
stale fleet state as current.

If work stops here: the main daily workflow and start screen are complete; the
remaining work is route migration rather than architectural invention.

### Phase 3 — Health, Services, and Packages (**L**)

1. Migrate Doctor to Health.
2. Migrate Services and Packages to shared list/inspector/action components.
3. Normalize confirmation, progress, error, empty, and refresh states and remove
   those legacy route wrappers.

Exit gate: Health, Services, and Packages are native routes with keyboard/mouse
equivalence and no raw process construction in their views.

If work stops here: all local-machine inspection and maintenance routes use the
new interaction model; only Fleet/Projects retain compatibility bodies.

### Phase 4 — Fleet and Projects (**L**)

1. Build exact-revision Fleet checks, cache age/refresh behavior, rollout
   results, and the wide canonical golden.
2. Add ignored/non-fleet host configuration for aliases such as `orb`.
3. Migrate Projects and terminal handoff behavior.
4. Remove the final legacy Doctor/Manage models and route wrappers.

Exit gate: partial/unreachable/dirty fleet states are explicit and scriptable,
Projects handoff works, and no legacy screen model remains.

If work stops here: every route is native to the new shell; only cross-cutting
polish and compatibility cleanup remain.

### Phase 5 — mouse, accessibility, and cleanup (**M**)

1. Finish hit targets and wheel routing for every screen.
2. Add light/no-color/ASCII modes and the density setting.
3. Replace old navigation and command docs.
4. Remove dead adapters, layout styles, compatibility messages whose fleet gate
   has passed, and unused dependencies.
5. Run the full property matrix and review the three canonical goldens.

Exit gate: every route passes the supported-size and keyboard/mouse matrix, and
the repository contains one operation model, one navigation model, and no dead
legacy path.

If work stops here: the redesign is complete.

## Acceptance criteria

The redesign is complete only when all are true:

### Comprehension

- The first screen identifies machine, repository, fleet, and attention state
  within one glance.
- There is one top-level navigation model.
- A user can discover every action through `a`, `Ctrl-P`/`:`, or contextual
  help without reading source or memorizing a table.
- The footer never wraps and never lies about an inert key.

### Consistency

- Directional keys keep the same spatial meaning everywhere.
- Tab always changes focus, never route.
- Esc always moves one layer back; it never unexpectedly quits.
- Enter is never a destructive action.
- Keyboard and mouse produce the same named intents.

### Safety

- Bare `dots sync` cannot push or contact the fleet.
- Publish and rollout are separate plans and confirmations.
- No dirty checkout is automatically stashed, reset, rebased, committed, or
  overwritten.
- A rollout targets an exact published Git revision.
- Partial fleet results cannot exit or render as complete success.

### Rendering

- No tested route overflows at any supported width/height.
- Narrow screens reorganize instead of truncating essential state.
- Color is never the only status signal.
- Startup paints before SSH/package/deep health work completes.

### Architecture

- CLI and TUI share planners and runners.
- Views dispatch action IDs, not shell commands.
- One layout solver owns geometry.
- One focus/input stack owns precedence.
- The shell fallback cannot reintroduce retired mutation semantics.

### Resource bounds

- The release cache retains the current and previous verified release families;
  cleanup never deletes the installed symlink target.
- Cache files use named schemas, TTLs, invalidation events, and bounded retry
  behavior rather than growing or refreshing implicitly forever.

## Deliberate non-goals

- Do not add a web UI.
- Do not replace Go/Bubble Tea with another runtime.
- Do not auto-remediate remote machines from a health finding.
- Do not auto-stash, rebase, reset, force-push, or resolve conflicts.
- Do not build a plugin system during the redesign.
- Do not add telemetry; this is a personal fleet tool.
- Do not require Nerd Fonts for the TUI.
- Do not add decorative charts that do not change a decision.

## Recommended decisions to approve

These are the defaults this plan assumes:

1. Keep Bubble Tea. Complete command safety and the mutation core first, then
   make the stable v2 Charm upgrade a mechanical port before building the new
   shell.
2. Use the eight-route global sidebar and Overview as the start route.
3. Make compact density the default.
4. Use `Ctrl-P` and `:` for the same command palette.
5. Make `a` the universal context-action entry point.
6. Rename lifecycle operations to Sync inbound, Apply local, Publish repo, and
   Rollout fleet.
7. Migrate the changed sync semantics across two signed releases, gating each
   on all four machines passing the same version-and-provenance probe rather
   than on elapsed release count.
8. Keep direct service/package hotkeys out of the first version; expose them
   through the action menu and add accelerators only after the vocabulary is
   stable.
9. Keep the Bash fallback as a reference/recovery tier: inbound update/sync may
   recover the Go binary, while unsupported mutations refuse explicitly.
