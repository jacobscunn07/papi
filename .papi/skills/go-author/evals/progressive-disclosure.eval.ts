import { existsSync, readFileSync } from 'fs';
import { join } from 'path';
import type { Eval, EvalContext, EvalResult } from './types.js';
import { runEval } from './utils.js';

const EVAL_ID = 'progressive-disclosure';
const EVAL_NAME = 'SKILL.md routes to references instead of growing';

/**
 * Everything in SKILL.md is loaded on every invocation, so a long one spends context
 * on detail the current task does not need. Past LINE_BUDGET the skill is expected to
 * become a router: a short SKILL.md plus `references/<topic>.md` files Claude opens on
 * demand.
 *
 * Below the budget this scores N/A (1.0) - splitting a short skill is pure indirection,
 * and the loop only keeps a change that strictly improves the score, so a neutral
 * result here means "leave it alone".
 */
const LINE_BUDGET = 200;
/** Above this, having split at all is worth half credit and no more. */
const NO_CREDIT_ABOVE = 300;

const result = (score: number, reasoning: string): EvalResult => ({
  evalId: EVAL_ID,
  name: EVAL_NAME,
  score,
  reasoning,
});

const clamp01 = (n: number): number => Math.max(0, Math.min(1, n));

/** Matches a references/<path>.md mention anywhere: markdown link, inline code, or bare. */
const REFERENCE_RE = /references\/[A-Za-z0-9._-]+(?:\/[A-Za-z0-9._-]+)*\.md/g;

/**
 * Reads the SKILL.md that ran. Measuring the file on disk (rather than ctx.skillContent,
 * which has the frontmatter stripped) keeps this eval's line count identical to the
 * budget stated in program.md, so the research agent is scored against the number it
 * was told to hit.
 */
function readSkillMd(ctx: EvalContext): string {
  const path = join(ctx.skillDir ?? '', 'SKILL.md');
  if (ctx.skillDir && existsSync(path)) {
    try {
      return readFileSync(path, 'utf8');
    } catch {
      /* fall through to the in-context copy */
    }
  }
  return ctx.skillContent ?? '';
}

function countLines(text: string): number {
  const trimmed = text.replace(/\n+$/, '');
  return trimmed === '' ? 0 : trimmed.split('\n').length;
}

const progressiveDisclosureEval: Eval = {
  id: EVAL_ID,
  name: EVAL_NAME,

  async evaluate(ctx: EvalContext): Promise<EvalResult> {
    // Judges the skill's own shape, so it does not depend on invocation or output.
    const skillMd = readSkillMd(ctx);
    if (!skillMd) return result(0.0, 'Could not read SKILL.md to measure.');

    const lines = countLines(skillMd);
    if (lines <= LINE_BUDGET) {
      return result(
        1.0,
        `N/A: SKILL.md is ${lines} lines, within the ${LINE_BUDGET}-line budget, so progressive disclosure is not required.`,
      );
    }

    const linked = [...new Set(skillMd.match(REFERENCE_RE) ?? [])].sort();
    if (linked.length === 0) {
      return result(
        0.0,
        `SKILL.md is ${lines} lines (over the ${LINE_BUDGET}-line budget) and links no reference files. ` +
          `Move the task-specific depth into references/<topic>.md and leave a routing table behind.`,
      );
    }

    const missing = linked.filter((p) => !existsSync(join(ctx.skillDir ?? '', p)));
    if (missing.length > 0) {
      return result(
        0.0,
        `SKILL.md points at ${missing.length} reference file(s) that do not exist: ${missing.join(', ')}. ` +
          `A dangling pointer is worse than a long file - Claude follows it and finds nothing.`,
      );
    }

    const overflow = (NO_CREDIT_ABOVE - lines) / (NO_CREDIT_ABOVE - LINE_BUDGET);
    const score = 0.5 + 0.5 * clamp01(overflow);
    return result(
      score,
      `SKILL.md is ${lines} lines (budget ${LINE_BUDGET}) and routes to ${linked.length} reference file(s): ${linked.join(', ')}. ` +
        `Score rises toward 1.0 as SKILL.md shrinks to ${LINE_BUDGET} lines.`,
    );
  },
};

export default progressiveDisclosureEval;

runEval((ctx) => progressiveDisclosureEval.evaluate(ctx));
