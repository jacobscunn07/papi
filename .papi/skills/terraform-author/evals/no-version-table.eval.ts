import type { Eval, EvalContext, EvalResult } from './types.js';
import { lintMarkdown, messagesFor, type EvalRule } from './eslint-runner.js';
import { runEval } from './utils.js';

const EVAL_ID = 'no-version-table';
const EVAL_NAME = 'No stale version/SHA catalog table';

const result = (score: number, reasoning: string): EvalResult => ({
  evalId: EVAL_ID,
  name: EVAL_NAME,
  score,
  reasoning,
});

// Header columns that identify a row's subject (the thing being versioned).
const SUBJECT_COL = /\b(module|modules|source|name|provider|registry|chart|image|package)\b/i;
// Header columns that hold a drifting version value.
const VERSION_COL = /\b(version|versions|sha|ref|commit|commits|tag|tags|latest|current|hash|release)\b/i;

// Concrete pinned values in table data cells.
const SEMVER = /\bv?\d+\.\d+(?:\.\d+)?\b/;
const SHA = /\b[0-9a-f]{7,40}\b/i;

/** Flattens an mdast node's text content (cells hold inline nodes, not raw text). */
function textOf(node: any): string {
  if (typeof node?.value === 'string') return node.value;
  if (!Array.isArray(node?.children)) return '';
  return node.children.map(textOf).join('');
}

function isCatalogTable(header: string[], rows: string[][]): boolean {
  const headerText = header.join(' ');
  const hasSubjectCol = SUBJECT_COL.test(headerText);
  const hasVersionCol = VERSION_COL.test(headerText);

  // Strong signal: a "subject × version" header shape (e.g. Module | Version).
  if (hasSubjectCol && hasVersionCol) return true;

  // Weaker signal: a version-ish header column whose data cells actually contain
  // multiple concrete pinned versions/SHAs — i.e. a catalog of values, not a
  // single syntax example.
  if (hasVersionCol) {
    const pinned = rows.filter((r) => r.some((c) => SEMVER.test(c) || SHA.test(c)));
    if (pinned.length >= 2) return true;
  }
  return false;
}

/**
 * Reports GFM tables that catalog versions or SHAs. Replaces a hand-rolled pipe
 * table parser — @eslint/markdown hands us real `table`/`tableRow`/`tableCell`
 * nodes, so alignment rows, escaped pipes and inline code all parse correctly.
 */
const catalogTableRule: EvalRule = {
  create(context) {
    return {
      table(node: any) {
        const [headerRow, ...bodyRows] = node.children ?? [];
        if (!headerRow) return;
        const header = (headerRow.children ?? []).map((c: any) => textOf(c).trim());
        const rows = bodyRows.map((r: any) => (r.children ?? []).map((c: any) => textOf(c).trim()));
        if (!isCatalogTable(header, rows)) return;
        context.report({ node, message: `header "${header.join(' | ')}"` });
      },
    };
  },
};

const noVersionTableEval: Eval = {
  id: EVAL_ID,
  name: EVAL_NAME,

  async evaluate(ctx: EvalContext): Promise<EvalResult> {
    // Checks the skill's own text, so it does not depend on invocation. Reference
    // files count as part of the skill: a catalog table moved into references/ is
    // just as stale as one left in SKILL.md, and scanning only SKILL.md would let
    // progressive disclosure launder it past this guard.
    const sources = [
      { label: 'SKILL.md', text: ctx.skillContent ?? '' },
      ...(ctx.skillFiles ?? []).map((f) => ({ label: f.path, text: f.content })),
    ];

    const offenders: { label: string; message: string }[] = [];
    for (const { label, text } of sources) {
      const found = messagesFor(await lintMarkdown(text, { 'catalog-table': catalogTableRule }), 'catalog-table');
      offenders.push(...found.map((m) => ({ label, message: m.message })));
    }

    const scanned = sources.map((s) => s.label).join(', ');
    if (offenders.length === 0) {
      return result(1.0, `No module/version/SHA catalog table found in ${scanned}.`);
    }

    const first = offenders[0];
    return result(
      0.0,
      `The skill contains ${offenders.length} catalog table(s) enumerating versions/SHAs (e.g. in ${first.label}: ${first.message}). This data goes stale; teach how to find/pin versions instead of listing current values.`,
    );
  },
};

export default noVersionTableEval;

runEval((ctx) => noVersionTableEval.evaluate(ctx));
