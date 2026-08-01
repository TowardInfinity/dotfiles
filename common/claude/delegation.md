# Subagent delegation

Shared across every machine — imported by each box's `~/.claude/CLAUDE.md` via
`@~/.claude/delegation.md`. The rest of that file stays machine-specific (the
Mac's toolchain preferences, a server's cost/security guardrails); only this
policy is common, so edit it here and `git pull` everywhere else.

Main session = planner/orchestrator (run it on Opus via `/model` when planning quality matters — CLAUDE.md can't force the parent session's model, only `/model` does).
- Delegate freely: whenever a piece of work can be handed to a subagent (Agent tool) without losing important context, do so by default rather than doing it inline — this keeps token spend on the pricier main session low. This overrides the usual "don't spawn agents unless asked" default.
- Pick the built-in agent type that fits (`general-purpose`, `Explore` for search/lookups, `Plan` for design work, etc.) — no need for custom-named agents.
- Keep orchestration-only work (synthesizing subagent reports, user-facing decisions, anything needing full conversation context) on the main session rather than delegating it — the goal is to offload token-heavy execution, not the planning role itself.

## Model tiering for delegated work

Pass an explicit `model` override on every Agent tool call, chosen by task weight — don't leave it unset for delegated work:
- **Opus** — stays on the main session only; never used for a spawned subagent.
- **Sonnet** (`model: "sonnet"`) — default tier for delegated execution: multi-file implementation, non-trivial refactors/bug fixes, research or exploration that requires judgment about relevance, test writing, debugging that needs real reasoning.
- **Haiku** (`model: "haiku"`) — cheap tier, reserved for genuinely mechanical work only: well-defined lookups ("find file X", "grep for Y", "list usages of Z"), rote transforms with an exact spec (rename a variable everywhere, formatting fixes), running a command and reporting raw output, single-file edits with unambiguous instructions. Batch similar mechanical tasks into one Haiku call rather than spawning several.
- **Fable** — not part of this tiering; only relevant for explicitly creative/narrative tasks, not general engineering delegation.
- **Decision rule**: if a task's tier is ambiguous, round up (Sonnet over Haiku, Opus-on-main over delegating at all) — a redo after a bad cheap result burns more tokens than starting at the right tier.

**Do not set `CLAUDE_CODE_SUBAGENT_MODEL`.** It is a hard override that beats both
the per-call `model` parameter and any agent's `model:` frontmatter, so it would
silently collapse the tiering above onto a single model.
