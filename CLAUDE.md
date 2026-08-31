# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repo is

A monorepo of installable Claude Code **skills** plus **papi**, an autoresearch loop
(Karpathy autoresearch style) that self-improves a skill's `SKILL.md` by repeatedly
testing it, asking a research agent to rewrite it, and keeping the change only if the
score goes up.

- `skills/<name>/` — distributable skill files (`SKILL.md` + `README.md`, plus optional
  `references/*.md`). `SKILL.md` is what papi optimizes; its YAML frontmatter `description`
  is the **primary** optimization target. Past ~200 lines the agent is told to keep
  `SKILL.md` a short router and move depth into `references/<topic>.md` (progressive
  disclosure); those files are loaded on demand via `--plugin-dir`, and papi owns the whole
  `references/` tree — it is rewritten wholesale on every accepted proposal.
- `packages/papi/` — the Go CLI/TUI that runs the loop (private, not distributed).
- `.papi/skills/<name>/` — per-skill test harness: `scenarios/`, `evals/`, `hooks/`,
  `config.yaml`, and `runs/` (generated artifacts, gitignored).
- `.papi/config` — global defaults (viper config file). `.papi/program.md` — the research
  agent's system prompt (a `program.md` under `.papi/skills/<name>/` overrides it per skill).

## Commands

```bash
# Run the loop on a skill (npm wrapper passes --repo-root $INIT_CWD so it works from repo root)
npm run papi -- --skill terraform-author --iterations 20 --budget 5.0
# Equivalent direct invocation (note: subcommand is `run`, the npm script omits it):
go run -C packages/papi . run terraform-author --iterations 20 --budget 5.0

# Launch the interactive TUI (no subcommand): skill picker + live/past run browser
go run -C packages/papi .

# Useful flags: --dry-run (eval without writing SKILL.md or committing), --tags a,b
#   --scenario-model / --quality-model / --research-model, --max-runs, --llm-weight/--weight

# Go build / test (run from the module dir)
cd packages/papi && go build ./... && go test ./...
go test ./internal/scorer -run TestScoreScenario   # single test
```

Flags are bound to viper: every flag is also settable via `.papi/config` (YAML) or env vars
prefixed `RESEARCH_` with `-`→`_` (e.g. `RESEARCH_BUDGET`, `RESEARCH_ITERATIONS`).

The loop shells out to the `claude` CLI for all model calls, so `claude` must be on PATH and
authenticated. Custom evals and hooks must be written in **TypeScript or JavaScript** — nothing
else is supported. `.ts` runs under `tsx`, `.js` under `node`; both must be on PATH. Any other
extension is a hard error, not a silent skip.

## The loop architecture (`packages/papi/internal/`)

`loop.Run` is the orchestrator. Per run it acquires a PID lock (`.papi/skills/<name>/lock`),
loads scenarios + hooks, builds the eval registry, then:

1. **Iteration 0 (baseline):** run all scenarios against the current `SKILL.md`, score, commit.
2. **Iterations 1..N:** call the **research agent** (`callResearchAgent`) with the current
   skill (`SKILL.md` + `references/*.md`) + previous scenario results; it returns a proposed
   skill as JSON (`{description, skillMd, files[]}`). `config.ValidateProposal` checks it —
   frontmatter repair, `references/`-only paths, and link integrity in both directions (a
   SKILL.md pointing at a file the proposal never supplies is rejected outright and scores 0)
   — then `config.WriteSkillFiles` applies it, and all scenarios re-run and score. **If the
   score improved → `git commit`; else → revert the skill dir to the best SHA**
   (`git.RevertSkillDir`, which is `checkout` + `clean -fd` so files the rejected proposal
   added do not survive). Stops on max-iterations or budget exhaustion.
3. **Finalize:** tag the best commit (`research/<skill>/<ts>-best-<score>`), purge old runs,
   run post-run hooks.

Cancelling the context (ctx) stops gracefully at the next scenario/iteration boundary and
restores the best version of the skill.

### Per-scenario three-phase pipeline (`runner.RunScenario` → `scorer.ScoreScenario`)

This split is the core mental model of the whole system:

1. **Invocation phase (gating).** Claude sees **only the skill name + `description`** (not the
   body), via a dispatcher system prompt, and must decide whether to emit `/skill-name`.
   `detectInvocation` checks the transcript. This isolates and tests the `description` field.
2. **Quality phase.** Only runs if the skill was invoked. Loads the full skill via
   `--plugin-dir <skillDir>` and `--dangerously-skip-permissions`, executes the task, captures
   output. Skipped for negative scenarios (`shouldInvoke: false`).
3. **Assessment phase.** `scorer.ScoreScenario` runs all evals against the `EvalContext`.

### Scoring (`scorer.go`)

- The built-in `skill-used` eval is **required**: if it scores 0 (skill not invoked when it
  should be, or invoked when it shouldn't), the scenario short-circuits to **0** and remaining
  evals are skipped. This is why a failed invocation check zeros the scenario regardless of body quality.
- Non-required evals are split into **LLM-judge** (`IsLLMJudge() == true`) and **non-LLM** groups.
  Scenario score = `llmWeight*llmCategoryScore + nonLLMWeight*nonLLMCategoryScore` (defaults 30/70,
  set via `--llm-weight`/`--weight`, must sum to 100). If a category is empty the other gets 100%.
- For negative scenarios, content evals are scored N/A (1.0) — only invocation is judged.
- Run score = simple average across scenarios (`AggregateScore`).

## Adding scenarios, evals, and hooks

- **Scenarios:** one YAML file per scenario in `.papi/skills/<name>/scenarios/` (`id`, `prompt`,
  optional `fixtures`, `tags`, `shouldInvoke`). **Do not name the skill in the prompt** — the
  invocation phase tests whether the description alone triggers it. Set `shouldInvoke: false` for
  negative cases that must *not* trigger the skill.
- **Custom evals:** files named `*.eval.ts` or `*.eval.js` in `.papi/skills/<name>/evals/`.
  Each receives the `EvalContext` as JSON on **stdin** and must print an `EvalResult` JSON to **stdout**
  (`evalId`, `name`, `score` 0–1, `reasoning`, optional `required`). Script evals are always treated as
  non-LLM-judge. See `evals/types.ts` for the context/result shapes. The two built-in evals
  (`skill-used`, `output-quality`) are always included. An eval that inspects the skill itself
  must read **both** `ctx.skillContent` (SKILL.md's body) and `ctx.skillFiles` (the reference
  files), or content moved out of SKILL.md silently evades it.
- **Hooks:** declared in `.papi/skills/<name>/config.yaml` under `hooks:`. Lifecycle points:
  `pre/post-run`, `pre/post-iteration`, `pre/post-scenario`, `pre/post-eval`, `post-quality`.
  Each accepts a single path or an ordered list, and must be a `.ts` or `.js` file. Hooks communicate
  by printing `KEY=VALUE` lines to stdout (injected as env vars into subsequent commands/phases);
  all other stdout/stderr is routed to the progress reporter, never the terminal.

## Conventions

- `appconfig.Resolve` walks up from `--repo-root` to find the nearest `.papi/` dir, so commands
  work from the repo root or a subdirectory. `appconfig.Build` assembles the `ResearchConfig`.
- Keep `internal/types` free of dependencies on other internal packages — it's the shared schema
  imported everywhere (`appconfig` exists separately to avoid a cmd↔tui import cycle).
- Progress is event-driven: business logic emits `progress.*` events to a `progress.Reporter`
  (CLI reporter or the bubbletea TUI); never write directly to stdout from the loop/runner.
- Generated run artifacts live under `.papi/skills/<name>/runs/<timestamp>/iteration-NNN/` and are
  gitignored; only changes under `skills/<name>/` are committed by the loop.
- An iteration's stored snapshot is a **bundle** (`config.MarshalBundle`): SKILL.md followed by
  each reference file behind a `<!-- papi:file ... -->` marker. One string keeps the store
  schema and the TUI diff unchanged, and it round-trips exactly, which is what lets an
  interrupted iteration resume with its reference files intact.
