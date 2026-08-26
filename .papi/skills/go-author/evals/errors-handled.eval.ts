import type { Eval, EvalContext, EvalResult } from './types.js';
import { lintGoSources, messagesFor, type EvalRule } from './eslint-runner.js';
import { loadGoSource, runEval } from './utils.js';

const EVAL_ID = 'errors-handled';
const EVAL_NAME = 'Handle errors explicitly';

const result = (score: number, reasoning: string): EvalResult => ({
  evalId: EVAL_ID,
  name: EVAL_NAME,
  score,
  reasoning,
});

// Common stdlib calls that return an error and therefore demand a check.
const ERR_OP_RE =
  /^(os\.(Open|Create|ReadFile|WriteFile|Stat|Remove)|strconv\.(Atoi|Parse\w*)|json\.(Marshal|Unmarshal)|http\.(Get|Post|Do|NewRequest))$/;
const ERR_METHOD_RE = /\.(Read|Write|Scan|Decode|Encode)$/;

/** `if err != nil` — matched structurally, so a comment mentioning it cannot count. */
const errCheckRule: EvalRule = {
  create(context) {
    return {
      if_statement(node: any) {
        const cond = node.childForFieldName('condition');
        if (!cond || cond.type !== 'binary_expression') return;
        if (cond.childForFieldName('operator')?.text !== '!=') return;
        if (cond.childForFieldName('right')?.text !== 'nil') return;
        const left = cond.childForFieldName('left')?.text ?? '';
        if (!/(^|\b)err\w*$/.test(left)) return;
        context.report({ node, message: `if ${left} != nil` });
      },
    };
  },
};

/** Calls whose stdlib signature returns an error. */
const errOpRule: EvalRule = {
  create(context) {
    return {
      call_expression(node: any) {
        const fn = node.childForFieldName('function')?.text ?? '';
        if (!ERR_OP_RE.test(fn) && !ERR_METHOD_RE.test(fn)) return;
        context.report({ node, message: fn });
      },
    };
  },
};

const panicRule: EvalRule = {
  create(context) {
    return {
      call_expression(node: any) {
        if (node.childForFieldName('function')?.text !== 'panic') return;
        context.report({ node, message: 'panic()' });
      },
    };
  },
};

/**
 * A blank identifier in the final position of `x, _ := f()` — the conventional
 * error slot. Restricted to the trailing position so `for _, v := range` and
 * `_, ok := m[k]` style lookups are not counted; Go's error position is a
 * convention, not something knowable without type information.
 */
const discardedErrRule: EvalRule = {
  create(context) {
    return {
      short_var_declaration(node: any) {
        const left = node.childForFieldName('left');
        const right = node.childForFieldName('right');
        if (!left || !right) return;
        const names = left.namedChildren.filter(Boolean);
        const values = right.namedChildren.filter(Boolean);
        if (names.length < 2 || values.length !== 1) return;
        if (values[0].type !== 'call_expression') return;
        if (names[names.length - 1].text !== '_') return;
        context.report({ node, message: node.text.trim().slice(0, 60) });
      },
    };
  },
};

const errorsHandledEval: Eval = {
  id: EVAL_ID,
  name: EVAL_NAME,

  async evaluate(ctx: EvalContext): Promise<EvalResult> {
    const loaded = loadGoSource(ctx, EVAL_ID, EVAL_NAME);
    if ('result' in loaded) return loaded.result;

    const messages = await lintGoSources(loaded.sources, {
      'err-check': errCheckRule,
      'err-op': errOpRule,
      panic: panicRule,
      'discarded-err': discardedErrRule,
    });

    const errChecks = messagesFor(messages, 'err-check').length;
    const panics = messagesFor(messages, 'panic').length;
    const discarded = messagesFor(messages, 'discarded-err');
    const errOps = messagesFor(messages, 'err-op').length + discarded.length;

    if (errOps === 0 && errChecks === 0) {
      return result(1.0, 'No error-returning operations to handle.');
    }
    if (errChecks >= 1 && panics === 0) {
      if (discarded.length > 0) {
        return result(0.7, `Errors are checked, but ${discarded.length} call(s) discard the error into \`_\` (e.g. ${discarded[0].message}).`);
      }
      return result(1.0, "Errors are checked with 'if err != nil' and not swallowed by panic.");
    }
    if (errChecks >= 1 && panics >= 1) {
      return result(0.7, 'Errors are checked, but panic() is used where a returned error is preferable.');
    }
    if (errChecks === 0 && errOps >= 1) {
      const how = discarded.length > 0
        ? `error-returning calls present and ${discarded.length} discard the error into \`_\` (e.g. ${discarded[0].message})`
        : "error-returning calls present but no 'if err != nil' checks found";
      return result(0.2, `${how}.`);
    }
    return result(0.5, 'Error handling is unclear.');
  },
};

export default errorsHandledEval;

runEval((ctx) => errorsHandledEval.evaluate(ctx));
