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

/**
 * Runs the common preamble every go-author eval shares: bail out when the skill
 * was not invoked, when there is no work directory, or when no Go source was
 * written. Returns the contents of each Go file, or an EvalResult to emit as-is.
 *
 * Files are kept separate rather than concatenated: every Go file opens with its
 * own `package` clause, so joining them produces source that does not parse.
 */
export function loadGoSource(
  ctx: EvalContext,
  evalId: string,
  name: string,
): { sources: string[] } | { result: EvalResult } {
  const fail = (reasoning: string) => ({ result: { evalId, name, score: 0.0, reasoning } });

  if (!ctx.invoked || !ctx.qualityTranscript) return fail('Skipped — skill not invoked.');
  if (!ctx.workDir || !existsSync(ctx.workDir)) return fail('No work directory available to inspect.');

  const files = collectGoFiles(ctx.workDir);
  if (files.length === 0) return fail('No Go files written to disk.');

  const sources: string[] = [];
  for (const f of files) {
    try {
      sources.push(readFileSync(f, 'utf8'));
    } catch {
      // Unreadable file: skip it rather than failing the whole eval.
    }
  }
  if (sources.length === 0) return fail('No Go files could be read.');

  return { sources };
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
