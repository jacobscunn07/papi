import type { Eval, EvalContext, EvalResult } from './types.js';
import { lintJson, memberPath, messagesFor, type EvalRule } from './eslint-runner.js';
import { blockAttribute, loadTerraformJson } from './terraform-json.js';
import { clampScore, judgeWithClaude, runEval } from './utils.js';

const EVAL_ID = 'naming-convention';
const EVAL_NAME = 'Resource naming convention';

const result = (score: number, reasoning: string): EvalResult => ({
  evalId: EVAL_ID,
  name: EVAL_NAME,
  score,
  reasoning,
});

// Matches names like: s3-payments-prod-us-east-1-receipts
// Pattern: <resource_type>-<project>-<environment>-<region>-<identifier>
// The region segment must look like us-east-1, eu-west-2, ap-southeast-1, etc.
const NAMING_PATTERN = /\b[a-z][a-z0-9]*-[a-z][a-z0-9]*-[a-z][a-z0-9]*-(?:us|eu|ap|ca|sa|me|af)-[a-z]+-\d+-[a-z][a-z0-9-]*/;

// Attributes that carry a user-chosen resource name. Terraform has no single
// convention, so this covers `name`, any `*_name`, and the common aliases.
const NAME_ATTR_RE = /^(name|.*_name|bucket|identifier|.*_identifier|prefix|.*_prefix)$/;

// Blocks whose bodies name real infrastructure.
const NAMED_BLOCKS = new Set(['resource', 'module', 'data']);

/** Reports name-carrying attributes whose string value satisfies `accept`. */
function nameAttributeRule(accept: (value: string) => boolean): EvalRule {
  return {
    create(context) {
      return {
        Member(node) {
          const attr = blockAttribute(memberPath(context, node));
          if (!attr || !NAMED_BLOCKS.has(attr.kind) || !NAME_ATTR_RE.test(attr.attr)) return;

          const value: unknown = node.value?.value;
          if (typeof value !== 'string' || !accept(value)) return;
          context.report({ node, message: `${attr.address}.${attr.attr} = "${value}"` });
        },
      };
    },
  };
}

const namingConventionEval: Eval = {
  id: EVAL_ID,
  name: EVAL_NAME,

  async evaluate(ctx: EvalContext): Promise<EvalResult> {
    if (!ctx.invoked || !ctx.qualityTranscript) {
      return result(0.0, 'Skipped — skill not invoked.');
    }

    const { doc, source } = await loadTerraformJson(ctx);

    if (source !== 'none') {
      const messages = await lintJson(doc, {
        conventional: nameAttributeRule((v) => NAMING_PATTERN.test(v)),
        'named-attribute': nameAttributeRule(() => true),
      });

      const conventional = messagesFor(messages, 'conventional');
      if (conventional.length > 0) {
        return result(1.0, `Terraform uses the naming convention (${conventional.map((m) => m.message).join('; ')}).`);
      }
      if (messagesFor(messages, 'named-attribute').length === 0) {
        return result(0.5, 'No resource names found in the Terraform — naming convention may not apply to this scenario.');
      }
      // Names exist but none match literally. They are usually interpolated
      // (`name = local.vpc_name`), and hcl2json cannot resolve the expression —
      // so hand the question to the judge, which can read the surrounding prose.
    }

    const parsed = await judgeWithClaude(`Does this Terraform response use or recommend the naming pattern:
  <resource_type>-<project>-<environment>-<region>-<identifier>
  e.g. "s3-payments-prod-us-east-1-receipts"

Score: 0.0=no pattern, 0.5=neutral/not applicable, 0.7=structured but different pattern, 1.0=this exact pattern

TASK: ${ctx.scenario.prompt}
RESPONSE: ${ctx.qualityTranscript.slice(0, 2000)}
JSON only: {"score": <0-1>, "reasoning": "<one sentence>"}`);
    return result(clampScore(parsed.score), parsed.reasoning);
  },
};

export default namingConventionEval;

runEval((ctx) => namingConventionEval.evaluate(ctx));
