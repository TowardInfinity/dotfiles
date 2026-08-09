---
title: Operations
group: dots
order: 15
summary: The mutation matrix — what each command may change and what it must never reach
---

# Operations

Every mutation now goes through one typed Plan and Runner. The CLI and the
current TUI dispatch the same action IDs; neither screen owns a private shell
command. A plan names its scope, risk, ordered steps, confirmation, verification,
recovery, and the state that should refresh afterward. Constructing a plan may
perform bounded, read-only state checks, but it never mutates. The Runner is the
only mutation boundary, and it admits one operation at a time.

## Command matrix

| Command | May change | Must never do |
|---|---|---|
| `dots status` | nothing | repair, install, commit, push |
| `dots apply` | local links, backups, managed policy | network, package managers, plugin installers |
| `dots deps` | local packages and third-party tools | commit, push, SSH rollout |
| `dots update` | current checkout, configs, installed `dots` | commit, push, SSH rollout |
| `dots publish` | selected paths, one local commit, `origin/<branch>` | stage an unselected path, SSH, remote Apply |
| `dots rollout` | explicitly selected SSH hosts at one pinned released revision | stage, commit, or push |
| `dots sync --check` | the remote-tracking ref only | worktree changes, Apply, add, commit, push, SSH |

`dots publish` and `dots rollout` are deliberately separate. Publishing ends
by saying the fleet was not changed. Rollout accepts only a revision carrying a
`vMAJOR.MINOR.PATCH` tag that is also Latest, pins its full commit ID, refuses a
dirty remote checkout, and verifies the revision, applied configuration,
release-binary version, installed symlink, and signing-key provenance marker.

## Compatibility window

Bare `dots sync` still has its old **outbound** meaning for one compatibility
release: it stages every local change, commits, pushes, and then invokes the
inbound recovery command on configured hosts. It prints an explicit warning and
renders that entire scope before asking. Use `dots publish` and `dots rollout`
instead now.

After every machine is proven to run the compatibility binary admitted by the
expected signing key, bare `dots sync` flips to the inbound-only planner. Old
`-m`/`--push-only` flags will point to `dots publish`; `--remotes-only` will
point to `dots rollout`. The checked-in Bash fallback cannot publish, roll out,
install dependencies, or Apply: it refuses those verbs and keeps only `dots
update`, because that is the recovery path back to the signed Go binary.

## Apply versus dependencies

`dots apply` runs `install.sh --apply`. That mode reuses the installed binary
or the checked-in fallback and skips the resolver, TPM clone, plugin restore,
and every package manager. A second dry pass verifies the result. If a real
destination must be backed up or a managed file changes, the plan asks first;
an already-current install runs without ceremony.

`dots deps` owns the opposite boundary: package repositories, `sudo`, and
third-party latest installers. It always shows the source-bearing plan and asks
unless `-y` was explicit.
