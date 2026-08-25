import type { Eval, EvalContext, EvalResult } from './types.js';
import { countMatchingLines, loadGoSource, runEval } from './utils.js';

const EVAL_ID = 'errors-handled';
const EVAL_NAME = 'Handle errors explicitly';

const result = (score: number, reasoning: string): EvalResult => ({
  evalId: EVAL_ID,
  name: EVAL_NAME,
  score,
  reasoning,
});

const ERR_CHECK_RE = /if +err +!= +nil/;
const PANIC_RE = /\bpanic\(/;
// Common stdlib calls that return an error and therefore demand a check.
const ERR_OP_RE =
  /os\.(Open|Create|ReadFile|WriteFile|Stat|Remove)|strconv\.(Atoi|Parse)|json\.(Marshal|Unmarshal)|http\.(Get|Post|Do|NewRequest)|\.Read\(|\.Write\(|\.Scan\(|\.Decode\(|\.Encode\(/;

const errorsHandledEval: Eval = {
  id: EVAL_ID,
  name: EVAL_NAME,

  async evaluate(ctx: EvalContext): Promise<EvalResult> {
    const loaded = loadGoSource(ctx, EVAL_ID, EVAL_NAME);
    if ('result' in loaded) return loaded.result;
    const src = loaded.src;

    const errChecks = countMatchingLines(src, ERR_CHECK_RE);
    const panics = countMatchingLines(src, PANIC_RE);
    const errOps = countMatchingLines(src, ERR_OP_RE);

    if (errOps === 0 && errChecks === 0) {
      return result(1.0, 'No error-returning operations to handle.');
    }
    if (errChecks >= 1 && panics === 0) {
      return result(1.0, "Errors are checked with 'if err != nil' and not swallowed by panic.");
    }
    if (errChecks >= 1 && panics >= 1) {
      return result(0.7, 'Errors are checked, but panic() is used where a returned error is preferable.');
    }
    if (errChecks === 0 && errOps >= 1) {
      return result(0.2, "Error-returning calls present but no 'if err != nil' checks found.");
    }
    return result(0.5, 'Error handling is unclear.');
  },
};

export default errorsHandledEval;

runEval((ctx) => errorsHandledEval.evaluate(ctx));
