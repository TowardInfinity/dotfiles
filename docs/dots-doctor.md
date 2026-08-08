---
title: Doctor
group: dots
order: 30
summary: What doctor checks, its three states, and the two repair keys
---

# Doctor

Two different questions, in two groups.

**Tools** — Core, Tools and Frameworks — answer *can the configs run at all*: is
nvim here, is tpm cloned. A `--light` box is checked against the shorter list it
actually asked for, not the full one.

**Config** answers something quieter and worse: everything is installed, but has
the configuration drifted?

| Row | Fails when |
|---|---|
| `codex config` | `~/.codex/config.toml` is missing or does not parse as TOML |
| `codex mode` | it is anything other than `0600` |
| `managed block` | the markers are missing, duplicated or out of order, or the block no longer matches `common/codex/config.policy.toml` |
| `dots binary` | never — it reports the version and where the binary came from |
| `signing key` | the key this binary trusts differs from the one in the checkout, or the checkout has none |
| `release` | `--online` only: the installed version is behind Latest |

The mode row exists because that file went `0664` on three servers and nothing
errored. It can hold MCP credentials, so wider than owner-only is a finding.

The signing-key row prints a fingerprint you can compare by hand. When the
binary and the checkout disagree, `dots update` is about to start refusing
releases — see `dots signing`.

## Three states, and why warnings do not fail

`✓` passed · `✗` failed · `!` could not be answered.

A `!` never changes the exit status. Being offline, or running a `--copy`
install with no checkout to compare the policy against, is not an unhealthy
machine — and an exit code that cannot tell *unreachable* from *broken* is one
you stop trusting. Missing, malformed, insecure and stale all stay `✗`.

## Two repair keys, deliberately

In the pane, `i` installs missing packages and `c` repairs configuration by
re-running `install.sh`. They are separate because they fix disjoint problems:
folding them together would let a missing brew formula block a config repair.
`i` is never offered for a Config row — none of them names a package.

`--online` is CLI-only. The pane's pass stays offline so opening it never
stalls on a slow link for a check nobody asked for.
