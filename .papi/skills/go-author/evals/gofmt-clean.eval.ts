import { execFileSync } from 'child_process';
import { existsSync } from 'fs';
import type { Eval, EvalContext, EvalResult } from './types.js';
import { collectGoFiles, runEval } from './utils.js';

const EVAL_ID = 'gofmt-clean';
const EVAL_NAME = 'Go source parses and is gofmt-clean';

const result = (score: number, reasoning: string): EvalResult => ({
  evalId: EVAL_ID,
  name: EVAL_NAME,
  score,
  reasoning,
});

/** Runs gofmt over files and returns [stdout, stderr]. gofmt needs no go.mod. */
function gofmt(args: string[], files: string[]): { out: string; err: string } {
  try {
    const out = execFileSync('gofmt', [...args, ...files], {
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'pipe'],
      maxBuffer: 4 * 1024 * 1024,
    });
    return { out: out.trim(), err: '' };
  } catch (e) {
    const errObj = e as { stdout?: string; stderr?: string };
    return { out: (errObj.stdout ?? '').trim(), err: (errObj.stderr ?? '').trim() };
  }
}

const gofmtCleanEval: Eval = {
  id: EVAL_ID,
  name: EVAL_NAME,

  async evaluate(ctx: EvalContext): Promise<EvalResult> {
    if (!ctx.invoked || !ctx.qualityTranscript) return result(0.0, 'Skipped — skill not invoked.');
    if (!ctx.workDir || !existsSync(ctx.workDir)) {
      return result(0.0, 'No work directory available to inspect.');
    }

    const files = collectGoFiles(ctx.workDir);
    if (files.length === 0) return result(0.0, 'No Go files written to disk in the work directory.');

    // -e reports syntax errors on stderr; -l lists files that are not gofmt-formatted.
    const parsed = gofmt(['-e', '-l'], files);
    if (parsed.err) {
      return result(0.2, `Go source does not parse: ${parsed.err.slice(0, 300)}`);
    }

    const unformatted = gofmt(['-l'], files).out;
    if (unformatted) {
      return result(0.7, `Valid Go, but not gofmt-clean: ${unformatted.split('\n').join(' ')}`);
    }

    return result(1.0, 'Go source parses and is gofmt-clean.');
  },
};

export default gofmtCleanEval;

runEval((ctx) => gofmtCleanEval.evaluate(ctx));
