# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repo is

A monorepo of installable Claude Code **skills** plus **papi**, an autoresearch loop
(Karpathy autoresearch style) that self-improves a skill's `SKILL.md` by repeatedly
testing it, asking a research agent to rewrite it, and keeping the change only if the
score goes up.

- `skills/<name>/` — distributable skill files (`SKILL.md` + `README.md`). `SKILL.md` is
  what papi optimizes; its YAML frontmatter `description` is the **primary** optimization target.
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
authenticated. Custom evals/hooks shell out to `tsx`, `node`, `python3`, `bash`, or `go run`
depending on file extension — those runtimes must be installed for the evals that use them.

## The loop architecture (`packages/papi/internal/`)

`loop.Run` is the orchestrator. Per run it acquires a PID lock (`.papi/skills/<name>/lock`),
loads scenarios + hooks, builds the eval registry, then:

1. **Iteration 0 (baseline):** run all scenarios against the current `SKILL.md`, score, commit.
2. **Iterations 1..N:** call the **research agent** (`callResearchAgent`) with the current
   `SKILL.md` + previous scenario results; it returns a proposed new `SKILL.md` as JSON. Write
   it, re-run all scenarios, score. **If the score improved → `git commit`; else → revert the
   file to the best SHA** (`git.RevertSkillFile`). Stops on max-iterations or budget exhaustion.
3. **Finalize:** tag the best commit (`research/<skill>/<ts>-best-<score>`), purge old runs,
   run post-run hooks.

Cancelling the context (ctx) stops gracefully at the next scenario/iteration boundary and
restores the best `SKILL.md`.

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
- **Custom evals:** files named `*.eval.<ext>` in `.papi/skills/<name>/evals/` (`.ts .js .py .sh .go`).
  Each receives the `EvalContext` as JSON on **stdin** and must print an `EvalResult` JSON to **stdout**
  (`evalId`, `name`, `score` 0–1, `reasoning`, optional `required`). Script evals are always treated as
  non-LLM-judge. See `evals/types.ts` for the context/result shapes. The two built-in evals
  (`skill-used`, `output-quality`) are always included.
- **Hooks:** declared in `.papi/skills/<name>/config.yaml` under `hooks:`. Lifecycle points:
  `pre/post-run`, `pre/post-iteration`, `pre/post-scenario`, `pre/post-eval`, `post-quality`.
  Each accepts a single path or an ordered list; runner is chosen by extension. Hooks communicate
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
  gitignored; only `SKILL.md` changes are committed by the loop.
