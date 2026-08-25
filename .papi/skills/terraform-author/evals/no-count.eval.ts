import type { Eval, EvalContext, EvalResult } from './types.js';
import { extractCodeBlocks } from './utils.js';

const EVAL_ID = 'no-count';
const EVAL_NAME = 'No count — use for_each';

const result = (score: number, reasoning: string): EvalResult => ({
  evalId: EVAL_ID,
  name: EVAL_NAME,
  score,
  reasoning,
});

const COMMENT_RE = /#[^\n]*/g;
// `count = var.create*` is the sanctioned conditional-creation idiom, not a violation.
const CREATE_COUNT_RE = /count\s*=\s*var\.create\w*/g;
const COUNT_RE = /\bcount\s*=/;
const FOR_EACH_RE = /\bfor_each\s*=/;

function isCountViolation(code: string): boolean {
  return COUNT_RE.test(code.replace(CREATE_COUNT_RE, ''));
}

const noCountEval: Eval = {
  id: EVAL_ID,
  name: EVAL_NAME,

  async evaluate(ctx: EvalContext): Promise<EvalResult> {
    if (!ctx.invoked || !ctx.qualityTranscript) {
      return result(0.0, 'Skipped — skill not invoked.');
    }

    const blocks = extractCodeBlocks(ctx.qualityTranscript);
    let hasForEach = false;

    for (const block of blocks) {
      const stripped = block.replace(COMMENT_RE, '');
      if (isCountViolation(stripped)) {
        return result(0.1, 'Response contains `count =` in a code example. Use `for_each` instead.');
      }
      if (FOR_EACH_RE.test(stripped)) {
        hasForEach = true;
      }
    }

    if (blocks.length > 0 && hasForEach) {
      return result(1.0, 'Code uses `for_each` with no `count =` violations.');
    }
    if (blocks.length > 0) {
      return result(0.6, 'Code blocks present but no `for_each` usage detected.');
    }
    return result(0.5, 'No code blocks found — cannot determine for_each usage.');
  },
};

export default noCountEval;

// Subprocess entry point: called by papi via `tsx <file>` with EvalContext JSON on stdin
const chunks: Buffer[] = [];
process.stdin.on('data', (c: Buffer) => chunks.push(c));
process.stdin.on('end', async () => {
  try {
    const ctx: EvalContext = JSON.parse(Buffer.concat(chunks).toString());
    process.stdout.write(JSON.stringify(await noCountEval.evaluate(ctx)));
  } catch (err) {
    process.stderr.write(String(err));
    process.exit(1);
  }
});
