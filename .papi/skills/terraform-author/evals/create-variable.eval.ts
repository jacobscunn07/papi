import type { Eval, EvalContext, EvalResult } from './types.js';
import { lintJson, memberPath, messagesFor, type EvalRule } from './eslint-runner.js';
import { loadTerraformJson } from './terraform-json.js';
import { runEval } from './utils.js';

const EVAL_ID = 'create-variable';
const EVAL_NAME = 'create variable pattern';

const result = (score: number, reasoning: string): EvalResult => ({
  evalId: EVAL_ID,
  name: EVAL_NAME,
  score,
  reasoning,
});

const PER_RESOURCE_RE = /^create_\w+$/;

/** Reports `variable` blocks whose label matches. Path shape is variable.<name>. */
function variableNameRule(matches: (name: string) => boolean): EvalRule {
  return {
    create(context) {
      return {
        Member(node) {
          const path = memberPath(context, node);
          if (path.length !== 2 || path[0] !== 'variable' || !matches(path[1])) return;
          context.report({ node, message: `var.${path[1]}` });
        },
      };
    },
  };
}

const createVariableEval: Eval = {
  id: EVAL_ID,
  name: EVAL_NAME,

  async evaluate(ctx: EvalContext): Promise<EvalResult> {
    if (!ctx.invoked || !ctx.qualityTranscript) {
      return result(0.0, 'Skipped — skill not invoked.');
    }

    const { doc, source } = await loadTerraformJson(ctx);
    if (source === 'none') {
      return result(0.5, 'No parseable Terraform found — cannot determine pattern usage.');
    }

    const messages = await lintJson(doc, {
      'global-create': variableNameRule((name) => name === 'create'),
      'per-resource-create': variableNameRule((name) => PER_RESOURCE_RE.test(name)),
    });
    const hasGlobal = messagesFor(messages, 'global-create').length > 0;
    const perResource = messagesFor(messages, 'per-resource-create').map((m) => m.message);

    if (hasGlobal && perResource.length > 0) {
      return result(1.0, `Terraform declares both \`var.create\` and per-resource variables (${perResource.join(', ')}).`);
    }
    if (hasGlobal) {
      return result(0.6, 'Terraform declares `var.create` but no per-resource `create_<name>` variables.');
    }
    return result(
      0.5,
      'Terraform present but no create variable pattern detected — may not be applicable to this scenario.',
    );
  },
};

export default createVariableEval;

runEval((ctx) => createVariableEval.evaluate(ctx));
