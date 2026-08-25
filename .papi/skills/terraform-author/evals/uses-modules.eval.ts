import type { Eval, EvalContext, EvalResult } from './types.js';

const EVAL_ID = 'uses-modules';
const EVAL_NAME = 'Prefer modules over raw resources';

const result = (score: number, reasoning: string): EvalResult => ({
  evalId: EVAL_ID,
  name: EVAL_NAME,
  score,
  reasoning,
});

// Matched against the whole transcript, not just fenced code blocks.
const MODULE_RE = /module\s+"/;
const RAW_RESOURCE_RE = /resource\s+"aws_/;

const usesModulesEval: Eval = {
  id: EVAL_ID,
  name: EVAL_NAME,

  async evaluate(ctx: EvalContext): Promise<EvalResult> {
    if (!ctx.invoked || !ctx.qualityTranscript) {
      return result(0.0, 'Skipped — skill not invoked.');
    }

    const hasModule = MODULE_RE.test(ctx.qualityTranscript);
    const hasRaw = RAW_RESOURCE_RE.test(ctx.qualityTranscript);

    if (hasModule && !hasRaw) {
      return result(1.0, 'Code uses module blocks with no raw aws_* resources.');
    }
    if (!hasModule && hasRaw) {
      return result(0.1, 'Code uses raw resource "aws_*" blocks instead of modules.');
    }
    if (hasModule && hasRaw) {
      return result(0.4, 'Code mixes module blocks and raw aws_* resources.');
    }
    return result(0.5, 'Code present but no module or aws_* resource pattern detected.');
  },
};

export default usesModulesEval;

// Subprocess entry point: called by papi via `tsx <file>` with EvalContext JSON on stdin
const chunks: Buffer[] = [];
process.stdin.on('data', (c: Buffer) => chunks.push(c));
process.stdin.on('end', async () => {
  try {
    const ctx: EvalContext = JSON.parse(Buffer.concat(chunks).toString());
    process.stdout.write(JSON.stringify(await usesModulesEval.evaluate(ctx)));
  } catch (err) {
    process.stderr.write(String(err));
    process.exit(1);
  }
});
