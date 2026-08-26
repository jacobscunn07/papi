import type { Eval, EvalContext, EvalResult } from './types.js';
import { lintJson, memberPath, messagesFor, type EvalRule } from './eslint-runner.js';
import { loadTerraformJson } from './terraform-json.js';
import { runEval } from './utils.js';

const EVAL_ID = 'uses-modules';
const EVAL_NAME = 'Prefer modules over raw resources';

const result = (score: number, reasoning: string): EvalResult => ({
  evalId: EVAL_ID,
  name: EVAL_NAME,
  score,
  reasoning,
});

/** Reports each `resource "aws_*"` block — the path shape is resource.<type>.<name>. */
const rawAwsResourceRule: EvalRule = {
  create(context) {
    return {
      Member(node) {
        const path = memberPath(context, node);
        if (path.length !== 3 || path[0] !== 'resource' || !path[1].startsWith('aws_')) return;
        context.report({ node, message: `resource.${path[1]}.${path[2]}` });
      },
    };
  },
};

/** Reports each `module` block — the path shape is module.<name>. */
const moduleBlockRule: EvalRule = {
  create(context) {
    return {
      Member(node) {
        const path = memberPath(context, node);
        if (path.length !== 2 || path[0] !== 'module') return;
        context.report({ node, message: `module.${path[1]}` });
      },
    };
  },
};

const usesModulesEval: Eval = {
  id: EVAL_ID,
  name: EVAL_NAME,

  async evaluate(ctx: EvalContext): Promise<EvalResult> {
    if (!ctx.invoked || !ctx.qualityTranscript) {
      return result(0.0, 'Skipped — skill not invoked.');
    }

    const { doc, source } = await loadTerraformJson(ctx);
    if (source === 'none') {
      return result(0.5, 'No parseable Terraform found — cannot determine module usage.');
    }

    const messages = await lintJson(doc, {
      'raw-aws-resource': rawAwsResourceRule,
      'module-block': moduleBlockRule,
    });
    const raw = messagesFor(messages, 'raw-aws-resource').map((m) => m.message);
    const modules = messagesFor(messages, 'module-block').map((m) => m.message);

    if (modules.length > 0 && raw.length === 0) {
      return result(1.0, `Terraform uses module blocks with no raw aws_* resources (${modules.join(', ')}).`);
    }
    if (modules.length === 0 && raw.length > 0) {
      return result(0.1, `Terraform uses raw aws_* resources instead of modules (${raw.join(', ')}).`);
    }
    if (modules.length > 0 && raw.length > 0) {
      return result(0.4, `Terraform mixes module blocks (${modules.join(', ')}) and raw aws_* resources (${raw.join(', ')}).`);
    }
    return result(0.5, 'Terraform present but no module or aws_* resource blocks detected.');
  },
};

export default usesModulesEval;

runEval((ctx) => usesModulesEval.evaluate(ctx));
