import type { Eval, EvalContext, EvalResult } from './types.js';
import { countMatchingLines, loadGoSource, runEval } from './utils.js';

const EVAL_ID = 'uses-context';
const EVAL_NAME = 'Use context.Context for cancellation and deadlines';

const result = (score: number, reasoning: string): EvalResult => ({
  evalId: EVAL_ID,
  name: EVAL_NAME,
  score,
  reasoning,
});

const CTX_TYPE_RE = /context\.Context/;
const CTX_CTOR_RE = /context\.(WithCancel|WithTimeout|WithDeadline|Background|TODO)/;
const CTX_DONE_RE = /\.Done\(\)/;
// Does the code do long-running, blocking, or concurrent work that should be cancellable?
const CANCELLABLE_RE =
  /go +func|net\/http|http\.(Get|Post|Do|Client|NewRequest)|time\.(Sleep|After|NewTimer|NewTicker)|sync\.WaitGroup|select *\{|<-/;

const usesContextEval: Eval = {
  id: EVAL_ID,
  name: EVAL_NAME,

  async evaluate(ctx: EvalContext): Promise<EvalResult> {
    const loaded = loadGoSource(ctx, EVAL_ID, EVAL_NAME);
    if ('result' in loaded) return loaded.result;
    const src = loaded.src;

    const ctxType = countMatchingLines(src, CTX_TYPE_RE);
    const ctxCtor = countMatchingLines(src, CTX_CTOR_RE);
    const ctxDone = countMatchingLines(src, CTX_DONE_RE);
    const cancellable = countMatchingLines(src, CANCELLABLE_RE);

    if (cancellable === 0) {
      return result(1.0, 'No long-running or cancellable work that requires a context.');
    }
    if (ctxType >= 1 && (ctxCtor >= 1 || ctxDone >= 1)) {
      return result(1.0, 'Uses context.Context and honors cancellation/deadlines.');
    }
    if (ctxType >= 1) {
      return result(0.7, 'Accepts context.Context but does not clearly honor Done()/timeouts.');
    }
    return result(0.2, 'Cancellable work present but no context.Context is used.');
  },
};

export default usesContextEval;

runEval((ctx) => usesContextEval.evaluate(ctx));
