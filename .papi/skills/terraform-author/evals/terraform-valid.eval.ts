import { execFile } from 'child_process';
import { readdir } from 'fs/promises';
import { promisify } from 'util';
import type { Eval, EvalContext, EvalResult } from './types.js';

const execFileAsync = promisify(execFile);

const EVAL_ID = 'terraform-valid';
const EVAL_NAME = 'Terraform fmt and validate';

const terraformValidEval: Eval = {
  id: EVAL_ID,
  name: EVAL_NAME,

  async evaluate(ctx: EvalContext): Promise<EvalResult> {
    if (!ctx.invoked || !ctx.qualityTranscript) {
      return { evalId: EVAL_ID, name: EVAL_NAME, score: 0.0, reasoning: 'Skipped — skill not invoked.' };
    }

    const cwd = ctx.workDir;

    const files = await readdir(cwd).catch(() => [] as string[]);
    const hasTf = files.some(f => f.endsWith('.tf'));
    if (!hasTf) {
      return { evalId: EVAL_ID, name: EVAL_NAME, score: 0.0, reasoning: 'No .tf files found in workDir — Claude did not write files to disk.' };
    }

    let validateOk = false;
    let validateReason = '';
    try {
      await execFileAsync('terraform', ['validate', '-no-color'], { cwd });
      validateOk = true;
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      validateReason = msg.slice(0, 300);
    }

    let fmtOk = false;
    try {
      await execFileAsync('terraform', ['fmt', '-check', '-no-color', cwd]);
      fmtOk = true;
    } catch {
      // fmt --check exits non-zero when files need formatting
    }

    if (validateOk && fmtOk) {
      return { evalId: EVAL_ID, name: EVAL_NAME, score: 1.0, reasoning: 'terraform validate and fmt --check both pass.' };
    }
    if (validateOk && !fmtOk) {
      return { evalId: EVAL_ID, name: EVAL_NAME, score: 0.7, reasoning: 'terraform validate passes but fmt --check fails (formatting issues).' };
    }
    return { evalId: EVAL_ID, name: EVAL_NAME, score: 0.2, reasoning: `terraform validate failed: ${validateReason}` };
  },
};

export default terraformValidEval;

// Subprocess entry point: called by papi via `tsx <file>` with EvalContext JSON on stdin
const chunks: Buffer[] = [];
process.stdin.on('data', (c: Buffer) => chunks.push(c));
process.stdin.on('end', async () => {
  const ctx: EvalContext = JSON.parse(Buffer.concat(chunks).toString());
  try {
    const result = await terraformValidEval.evaluate(ctx);
    process.stdout.write(JSON.stringify(result));
  } catch (err) {
    process.stderr.write(String(err));
    process.exit(1);
  }
});
