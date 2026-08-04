# Global preferences

Symlinked to `~/.codex/AGENTS.md` by the dotfiles `install.sh`. Loaded into every
Codex session — keep it short.

## Toolchain (set up 2026-07-19)
- **Java**: SDKMAN manages JDKs — default is Temurin 21 LTS (`sdk default java`). Spring Boot + Gradle Kotlin DSL is the usual stack. Brew's openjdk 26 exists but is not the default.
- **Node**: fnm manages versions (auto-switches via .nvmrc); prefer **pnpm** over npm for new projects (shared store, avoids node_modules bloat).
- **Python**: prefer **uv** (`uv venv`, `uv pip`, `uvx`) over pip/conda for new work. Miniconda exists for legacy/Jupyter.
- **Go**: brew-installed, standard GOPATH (~/go).

## Conventions
- Projects live in `~/Codes/Projects` (active), `~/Codes/Hobby`, `~/Codes/Learning`.
- Main Obsidian vault (the only one to write notes to): `~/Library/Mobile Documents/iCloud~md~obsidian/Documents/Obsidian Notes/`
- Local LLM: Ollama with qwen3:8b; LM Studio and Draw Things also available.
- When suggesting installs, prefer Homebrew.

## Budget

ChatGPT Plus **$20/mo**. Codex usage is capped by a 5-hour rolling window *and* a
weekly cap that stacks on top, shared across CLI, IDE, and web.

Relative cost per token: **Sol 5× · Terra 2.5× · Luna 1×**.
5-hour message allowance on Plus: Sol 15–90 · Terra 20–110 · Luna 50–280.

**Terra at `medium` effort is the default** and is set in `config.toml`. Escalate
a genuinely hard session with `codex -p sol` (Sol at high effort); do not reach
for Sol per-task. Reasoning effort is the sharpest lever — `high`/`xhigh` run
several times the tokens of `medium` for the same prompt.

## Multi-agent orchestration

The primary agent is the orchestrator and accountable owner: requirements,
planning, decomposition, risk and permission decisions, integration, final
review, and the user-facing answer.

**Subagents cost more total tokens, not fewer** — each keeps its own context and
starts cold. They buy a smaller main context and wall-clock parallelism, nothing
else. So:

- **Default to doing the work inline.** Delegate only when a subtask is bounded,
  independent, and produces verbose output that would otherwise flood the main
  thread: exploration, log triage, test execution, inventorying, summarization.
- **Do not delegate** trivial one-step work, tightly sequential work, work whose
  coordination cost exceeds the task, or overlapping writes.
- **Luna at low/medium** for narrow, repetitive, read-heavy work: targeted
  searches, file inventories, metadata extraction, log triage, mechanical QA.
  This is the configured subagent default.
- **Terra at medium** when a delegated task needs real judgment.
- **Never spawn Sol subagents** unless the user asks explicitly.
- **Cap active subagents at 1** (the configured default). Raise deliberately for
  read-only fan-out; never as a standing default.
- Prefer parallel read-heavy work. For writes, give each subagent exclusive file
  ownership — never let two agents edit the same file concurrently.
- Give every subagent scope, constraints, expected evidence, and output format.
  Require concise summaries with file references, not raw logs.
- Keep destructive actions, credentials, external publishing, and financial or
  broker mutations with the primary agent unless the user authorizes the exact
  action. Subagent use never broadens permissions or scope.

Project-level or nested `AGENTS.md` may narrow this policy. Explicit user
instructions for the current task take precedence.

## Two tools, two quotas

Claude Code runs on a separate Claude Pro $20 allowance. Draining one while the
other idles is the most common waste — route deliberately:

| Work | Tool |
|---|---|
| Architecture, planning, "what should I build" | Claude Code (`/model opusplan`) |
| Multi-file refactors, cross-cutting changes, review | Claude Code (Sonnet) |
| Bulk implementation from a written spec | **Codex** (Terra) |
| Test writing, log/CI triage, mechanical transforms | **Codex** (Luna) |
| Anything after one tool caps out | the other one |

When handing a task across, write the plan to a file first. Re-explaining context
to a cold tool is the expensive part; a spec file makes it nearly free.
