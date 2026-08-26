import type { Eval, EvalContext, EvalResult } from './types.js';
import { lintJson, memberPath, messagesFor, type EvalRule } from './eslint-runner.js';
import { blockAttribute, loadTerraformJson } from './terraform-json.js';
import { runEval } from './utils.js';

const EVAL_ID = 'no-count';
const EVAL_NAME = 'No count — use for_each';

const result = (score: number, reasoning: string): EvalResult => ({
  evalId: EVAL_ID,
  name: EVAL_NAME,
  score,
  reasoning,
});

// `count = var.create*` is the sanctioned conditional-creation idiom, not a
// violation. hcl2json renders HCL expressions as interpolation strings, so
// `count = var.create_web ? 1 : 0` arrives as "${var.create_web ? 1 : 0}".
const CREATE_VAR_RE = /\bvar\.create\w*/;

// Blocks that take meta-arguments. `variable` is excluded so a variable actually
// named "count" is not mistaken for the meta-argument.
const META_ARG_BLOCKS = new Set(['resource', 'data', 'module']);

function metaArgRule(attrName: string, describe: (address: string) => string): EvalRule {
  return {
    create(context) {
      return {
        Member(node) {
          const attr = blockAttribute(memberPath(context, node));
          if (!attr || attr.attr !== attrName || !META_ARG_BLOCKS.has(attr.kind)) return;

          const value: unknown = node.value?.value;
          if (attrName === 'count' && typeof value === 'string' && CREATE_VAR_RE.test(value)) {
            return; // conditional-creation idiom
          }
          context.report({ node, message: describe(attr.address) });
        },
      };
    },
  };
}

const noCountEval: Eval = {
  id: EVAL_ID,
  name: EVAL_NAME,

  async evaluate(ctx: EvalContext): Promise<EvalResult> {
    if (!ctx.invoked || !ctx.qualityTranscript) {
      return result(0.0, 'Skipped — skill not invoked.');
    }

    const { doc, source } = await loadTerraformJson(ctx);
    if (source === 'none') {
      return result(0.5, 'No parseable Terraform found — cannot determine for_each usage.');
    }

    const messages = await lintJson(doc, {
      'no-count': metaArgRule('count', (a) => `${a} uses \`count\``),
      'for-each': metaArgRule('for_each', (a) => `${a} uses \`for_each\``),
    });

    const violations = messagesFor(messages, 'no-count');
    if (violations.length > 0) {
      const where = violations.map((m) => m.message).join('; ');
      return result(0.1, `Terraform uses \`count\` instead of \`for_each\` (${where}).`);
    }

    if (messagesFor(messages, 'for-each').length > 0) {
      return result(1.0, 'Terraform uses `for_each` with no `count` violations.');
    }
    return result(0.6, 'Terraform present but no `for_each` usage detected.');
  },
};

export default noCountEval;

runEval((ctx) => noCountEval.evaluate(ctx));
