import type { Eval, EvalContext, EvalResult } from './types.js';

const ID = 'no-version-table';
const NAME = 'No stale version/SHA catalog table';

// Header columns that identify a row's subject (the thing being versioned).
const SUBJECT_COL = /\b(module|modules|source|name|provider|registry|chart|image|package)\b/i;
// Header columns that hold a drifting version value.
const VERSION_COL = /\b(version|versions|sha|ref|commit|commits|tag|tags|latest|current|hash|release)\b/i;

// Concrete pinned values in table data cells.
const SEMVER = /\bv?\d+\.\d+(?:\.\d+)?\b/;
const SHA = /\b[0-9a-f]{7,40}\b/i;

interface TableBlock {
  header: string[];
  rows: string[][];
}

/** Splits markdown into contiguous pipe-table blocks (header + separator + rows). */
function findTables(md: string): TableBlock[] {
  const lines = md.split('\n');
  const tables: TableBlock[] = [];
  const cells = (line: string): string[] =>
    line.trim().replace(/^\|/, '').replace(/\|$/, '').split('|').map((c) => c.trim());
  const isSeparator = (line: string): boolean =>
    /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)+\|?\s*$/.test(line);

  for (let i = 0; i + 1 < lines.length; i++) {
    if (lines[i].includes('|') && isSeparator(lines[i + 1])) {
      const header = cells(lines[i]);
      const rows: string[][] = [];
      let j = i + 2;
      while (j < lines.length && lines[j].includes('|') && lines[j].trim() !== '') {
        rows.push(cells(lines[j]));
        j++;
      }
      tables.push({ header, rows });
      i = j - 1;
    }
  }
  return tables;
}

function isCatalogTable(t: TableBlock): boolean {
  const headerText = t.header.join(' ');
  const hasSubjectCol = SUBJECT_COL.test(headerText);
  const hasVersionCol = VERSION_COL.test(headerText);

  // Strong signal: a "subject × version" header shape (e.g. Module | Version).
  if (hasSubjectCol && hasVersionCol) return true;

  // Weaker signal: a version-ish header column whose data cells actually contain
  // multiple concrete pinned versions/SHAs — i.e. a catalog of values, not a
  // single syntax example.
  if (hasVersionCol) {
    const pinned = t.rows.filter((r) => r.some((c) => SEMVER.test(c) || SHA.test(c)));
    if (pinned.length >= 2) return true;
  }
  return false;
}

const noVersionTableEval: Eval = {
  id: ID,
  name: NAME,

  async evaluate(ctx: EvalContext): Promise<EvalResult> {
    // Checks the SKILL.md body itself, so it does not depend on invocation.
    const md = ctx.skillContent ?? '';
    const offenders = findTables(md).filter(isCatalogTable);

    if (offenders.length === 0) {
      return { evalId: ID, name: NAME, score: 1.0, reasoning: 'No module/version/SHA catalog table found in SKILL.md.' };
    }

    const sample = offenders[0].header.join(' | ');
    return {
      evalId: ID,
      name: NAME,
      score: 0.0,
      reasoning: `SKILL.md contains ${offenders.length} catalog table(s) enumerating versions/SHAs (e.g. header "${sample}"). This data goes stale; teach how to find/pin versions instead of listing current values.`,
    };
  },
};

export default noVersionTableEval;

// Subprocess entry point: called by papi via `tsx <file>` with EvalContext JSON on stdin
const chunks: Buffer[] = [];
process.stdin.on('data', (c: Buffer) => chunks.push(c));
process.stdin.on('end', async () => {
  const ctx: EvalContext = JSON.parse(Buffer.concat(chunks).toString());
  try {
    const result = await noVersionTableEval.evaluate(ctx);
    process.stdout.write(JSON.stringify(result));
  } catch (err) {
    process.stderr.write(String(err));
    process.exit(1);
  }
});
