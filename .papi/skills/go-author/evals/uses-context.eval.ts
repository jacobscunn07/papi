import type { Eval, EvalContext, EvalResult } from './types.js';
import { lintGoSources, messagesFor, type EvalRule } from './eslint-runner.js';
import { loadGoSource, runEval } from './utils.js';

const EVAL_ID = 'uses-context';
const EVAL_NAME = 'Use context.Context for cancellation and deadlines';

const result = (score: number, reasoning: string): EvalResult => ({
  evalId: EVAL_ID,
  name: EVAL_NAME,
  score,
  reasoning,
});

const CTX_CTOR_RE = /^context\.(WithCancel|WithTimeout|WithDeadline|WithValue|Background|TODO)$/;
// Calls that block, sleep, or perform I/O worth making cancellable.
const CANCELLABLE_CALL_RE =
  /^(time\.(Sleep|After|NewTimer|NewTicker)|http\.(Get|Post|Do|NewRequest)|.*\.Do)$/;
// Statement types that introduce concurrency or blocking.
const CANCELLABLE_NODES = ['go_statement', 'select_statement', 'send_statement'];

/** A parameter or field typed `context.Context`. */
const ctxTypeRule: EvalRule = {
  create(context) {
    return {
      qualified_type(node: any) {
        const pkg = node.childForFieldName('package')?.text;
        const name = node.childForFieldName('name')?.text;
        if (pkg !== 'context' || name !== 'Context') return;
        context.report({ node, message: 'context.Context' });
      },
    };
  },
};

/** `context.WithTimeout(...)` and friends. */
const ctxCtorRule: EvalRule = {
  create(context) {
    return {
      call_expression(node: any) {
        const fn = node.childForFieldName('function')?.text ?? '';
        if (!CTX_CTOR_RE.test(fn)) return;
        context.report({ node, message: fn });
      },
    };
  },
};

/** `<-ctx.Done()` or any `.Done()` call. */
const ctxDoneRule: EvalRule = {
  create(context) {
    return {
      call_expression(node: any) {
        const fn = node.childForFieldName('function')?.text ?? '';
        if (!/\.Done$/.test(fn)) return;
        context.report({ node, message: fn });
      },
    };
  },
};

/** Work that should be cancellable: goroutines, selects, channel ops, blocking calls. */
const cancellableRule: EvalRule = {
  create(context) {
    const report = (node: any, what: string) => context.report({ node, message: what });
    const visitors: Record<string, (node: any) => void> = {
      call_expression(node: any) {
        const fn = node.childForFieldName('function')?.text ?? '';
        if (!CANCELLABLE_CALL_RE.test(fn)) return;
        report(node, fn);
      },
      unary_expression(node: any) {
        // `<-ch` receive
        if (node.childForFieldName('operator')?.text !== '<-') return;
        report(node, 'channel receive');
      },
    };
    for (const type of CANCELLABLE_NODES) {
      visitors[type] = (node: any) => report(node, type.replace('_', ' '));
    }
    return visitors;
  },
};

const usesContextEval: Eval = {
  id: EVAL_ID,
  name: EVAL_NAME,

  async evaluate(ctx: EvalContext): Promise<EvalResult> {
    const loaded = loadGoSource(ctx, EVAL_ID, EVAL_NAME);
    if ('result' in loaded) return loaded.result;

    const messages = await lintGoSources(loaded.sources, {
      'ctx-type': ctxTypeRule,
      'ctx-ctor': ctxCtorRule,
      'ctx-done': ctxDoneRule,
      cancellable: cancellableRule,
    });

    const ctxType = messagesFor(messages, 'ctx-type').length;
    const ctxCtor = messagesFor(messages, 'ctx-ctor').length;
    const ctxDone = messagesFor(messages, 'ctx-done').length;
    const cancellable = messagesFor(messages, 'cancellable');

    if (cancellable.length === 0) {
      return result(1.0, 'No long-running or cancellable work that requires a context.');
    }
    if (ctxType >= 1 && (ctxCtor >= 1 || ctxDone >= 1)) {
      return result(1.0, 'Uses context.Context and honors cancellation/deadlines.');
    }
    if (ctxType >= 1) {
      return result(0.7, 'Accepts context.Context but does not clearly honor Done()/timeouts.');
    }
    return result(
      0.2,
      `Cancellable work present (${[...new Set(cancellable.map((m) => m.message))].slice(0, 3).join(', ')}) but no context.Context is used.`,
    );
  },
};

export default usesContextEval;

runEval((ctx) => usesContextEval.evaluate(ctx));
