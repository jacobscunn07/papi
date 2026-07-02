# Skills

A monorepo of Claude Code skills with a self-improvement loop based on Karpathy's [autoresearch](https://github.com/karpathy/autoresearch) technique.

## Available skills

| Skill | Description |
|---|---|
| `terraform-author` | Terraform/OpenTofu module structure, testing, CI/CD, and state management |

## Repo structure

```
skills/              # Installable skill files (SKILL.md + README.md per skill)
packages/papi/       # Autoresearch loop (Go, private, not distributed)
.papi/               # Scenarios, program.md, run artifacts
```

## Add a new skill

1. Create `skills/<name>/SKILL.md` with YAML frontmatter (`name`, `description`, `version`) and skill body
2. Create `skills/<name>/README.md`
3. Create `.papi/skills/<name>/scenarios/scenarios.yaml` with natural-language test prompts (do not mention the skill name in prompts)
4. Run the research loop to improve it

## Run the research loop

```bash
npm run papi -- --skill terraform-author --iterations 20 --budget 5.0
```

Options:
```
--skill <name>           Skill to research (default: terraform-author)
--iterations <n>         Max iterations (default: 20)
--budget <usd>           Max API spend in USD (default: 5.0)
--tags <tags>            Filter scenarios by tag (comma-separated)
--dry-run                Run evals without modifying SKILL.md or committing
--scenario-model <model> Model for scenario sessions (default: claude-haiku-4-5-20251001)
--quality-model <model>  Model for quality eval (default: claude-sonnet-4-6)
--research-model <model> Model for research agent (default: claude-opus-4-7)
```

## How the research loop works

Each iteration:
1. A **research agent** reads the current `SKILL.md` + previous scenario results and proposes an improved version
2. Each scenario runs through a **two-phase test**:
   - **Invocation check (description test)**: Claude sees only the skill name and `description` frontmatter field and must decide whether to invoke `/skill-name` — no body content shown. This tests the description in isolation.
   - **Quality check**: Full skill loaded via `--plugin-dir`; evals score output quality
3. Score = average across scenarios. Failing the invocation check scores 0 for that scenario.
4. If new score beats the best so far → `git commit`. Otherwise → revert `SKILL.md`.

The `description` field is the primary optimization target — it determines whether Claude naturally reaches for the skill without being told to.

Scenarios can be marked `shouldInvoke: false` (negative cases) to ensure the skill doesn't over-trigger on unrelated tasks.

