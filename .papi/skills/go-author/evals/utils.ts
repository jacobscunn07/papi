import { existsSync, readdirSync, readFileSync } from 'fs';
import { join } from 'path';
import type { EvalContext, EvalResult } from './types.js';

/** Recursively collects the .go files Claude wrote under workDir. */
export function collectGoFiles(dir: string): string[] {
  let entries;
  try {
    entries = readdirSync(dir, { withFileTypes: true });
  } catch {
    return [];
  }
  const out: string[] = [];
  for (const e of entries) {
    const full = join(dir, e.name);
    if (e.isDirectory()) {
      out.push(...collectGoFiles(full));
    } else if (e.isFile() && e.name.endsWith('.go')) {
      out.push(full);
    }
  }
  return out;
}

/** Concatenates the contents of the given files, skipping any that fail to read. */
export function readAll(files: string[]): string {
  return files
    .map((f) => {
      try {
        return readFileSync(f, 'utf8');
      } catch {
        return '';
      }
    })
    .join('\n');
}

/** Counts the lines of src matching re, mirroring `grep -cE`. */
export function countMatchingLines(src: string, re: RegExp): number {
  return src.split('\n').filter((line) => re.test(line)).length;
}

/**
 * Runs the common preamble every go-author eval shares: bail out when the skill
 * was not invoked, when there is no work directory, or when no Go source was
 * written. Returns the concatenated Go source, or an EvalResult to emit as-is.
 */
export function loadGoSource(
  ctx: EvalContext,
  evalId: string,
  name: string,
): { src: string } | { result: EvalResult } {
  const fail = (reasoning: string) => ({ result: { evalId, name, score: 0.0, reasoning } });

  if (!ctx.invoked || !ctx.qualityTranscript) return fail('Skipped — skill not invoked.');
  if (!ctx.workDir || !existsSync(ctx.workDir)) return fail('No work directory available to inspect.');

  const files = collectGoFiles(ctx.workDir);
  if (files.length === 0) return fail('No Go files written to disk.');

  return { src: readAll(files) };
}

/** Subprocess entry point: reads EvalContext JSON from stdin, writes EvalResult to stdout. */
export function runEval(evaluate: (ctx: EvalContext) => Promise<EvalResult>): void {
  const chunks: Buffer[] = [];
  process.stdin.on('data', (c: Buffer) => chunks.push(c));
  process.stdin.on('end', async () => {
    try {
      const ctx: EvalContext = JSON.parse(Buffer.concat(chunks).toString());
      process.stdout.write(JSON.stringify(await evaluate(ctx)));
    } catch (err) {
      process.stderr.write(String(err));
      process.exit(1);
    }
  });
}
