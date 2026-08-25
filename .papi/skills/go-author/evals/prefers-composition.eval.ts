import type { Eval, EvalContext, EvalResult } from './types.js';
import { countMatchingLines, loadGoSource, runEval } from './utils.js';

const EVAL_ID = 'prefers-composition';
const EVAL_NAME = 'Prefer composition over inheritance';

const result = (score: number, reasoning: string): EvalResult => ({
  evalId: EVAL_ID,
  name: EVAL_NAME,
  score,
  reasoning,
});

const INTERFACE_RE = /interface *\{/;
const STRUCT_RE = /struct *\{/;
// Embedded fields: a line that is just a (possibly pointer/qualified) type name with
// no field name — the Go composition idiom (e.g. "io.Reader", "sync.Mutex", "*Base").
const EMBED_RE = /^[ \t]+\*?[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?[ \t]*$/;

const prefersCompositionEval: Eval = {
  id: EVAL_ID,
  name: EVAL_NAME,

  async evaluate(ctx: EvalContext): Promise<EvalResult> {
    const loaded = loadGoSource(ctx, EVAL_ID, EVAL_NAME);
    if ('result' in loaded) return loaded.result;
    const src = loaded.src;

    const interfaces = countMatchingLines(src, INTERFACE_RE);
    const structs = countMatchingLines(src, STRUCT_RE);
    const embeds = countMatchingLines(src, EMBED_RE);

    if (structs === 0 && interfaces === 0) {
      return result(1.0, 'No type definitions to evaluate for composition.');
    }
    if (interfaces >= 1 || embeds >= 1) {
      return result(1.0, 'Uses interfaces and/or struct embedding (composition over inheritance).');
    }
    if (structs >= 1) {
      return result(0.6, 'Concrete structs only — favor small interfaces or embedding for composition.');
    }
    return result(0.5, 'Type design is unclear.');
  },
};

export default prefersCompositionEval;

runEval((ctx) => prefersCompositionEval.evaluate(ctx));
