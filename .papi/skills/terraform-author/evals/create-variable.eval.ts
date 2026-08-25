import type { Eval, EvalContext, EvalResult } from './types.js';
import { joinCodeBlocks } from './utils.js';

const EVAL_ID = 'create-variable';
const EVAL_NAME = 'create variable pattern';

const result = (score: number, reasoning: string): EvalResult => ({
  evalId: EVAL_ID,
  name: EVAL_NAME,
  score,
  reasoning,
});

const GLOBAL_RE = /variable\s+"create"\s*\{|var\.create\b/;
const PER_RESOURCE_RE = /variable\s+"create_\w+"\s*\{|var\.create_\w+/;

const createVariableEval: Eval = {
  id: EVAL_ID,
  name: EVAL_NAME,

  async evaluate(ctx: EvalContext): Promise<EvalResult> {
    if (!ctx.invoked || !ctx.qualityTranscript) {
      return result(0.0, 'Skipped — skill not invoked.');
    }

    const code = joinCodeBlocks(ctx.qualityTranscript);
    if (!code) {
      return result(0.5, 'No code blocks found — cannot determine pattern usage.');
    }

    const hasGlobal = GLOBAL_RE.test(code);
    const hasPerResource = PER_RESOURCE_RE.test(code);

    if (hasGlobal && hasPerResource) {
      return result(1.0, 'Code uses both global `create` and per-resource `create_<name>` variables.');
    }
    if (hasGlobal) {
      return result(0.6, 'Code uses global `var.create` but missing per-resource `create_<name>` variables.');
    }
    return result(
      0.5,
      'Code present but no create variable pattern detected — may not be applicable to this scenario.',
    );
  },
};

export default createVariableEval;

// Subprocess entry point: called by papi via `tsx <file>` with EvalContext JSON on stdin
const chunks: Buffer[] = [];
process.stdin.on('data', (c: Buffer) => chunks.push(c));
process.stdin.on('end', async () => {
  try {
    const ctx: EvalContext = JSON.parse(Buffer.concat(chunks).toString());
    process.stdout.write(JSON.stringify(await createVariableEval.evaluate(ctx)));
  } catch (err) {
    process.stderr.write(String(err));
    process.exit(1);
  }
});
