import type { Eval, EvalContext, EvalResult } from './types.js';
import { joinCodeBlocks, judgeWithClaude } from './utils.js';

// Matches names like: s3-payments-prod-us-east-1-receipts
// Pattern: word-word-word-region-word  (at least 5 dash-separated segments)
// Region segment must look like us-east-1, eu-west-2, ap-southeast-1, etc.
const NAMING_PATTERN = /\b[a-z][a-z0-9]*-[a-z][a-z0-9]*-[a-z][a-z0-9]*-(?:us|eu|ap|ca|sa|me|af)-[a-z]+-\d+-[a-z][a-z0-9-]*/;

const namingConventionEval: Eval = {
  id: 'naming-convention',
  name: 'Resource naming convention',

  async evaluate(ctx: EvalContext): Promise<EvalResult> {
    if (!ctx.invoked || !ctx.qualityTranscript) {
      return { evalId: 'naming-convention', name: 'Resource naming convention', score: 0.0, reasoning: 'Skipped — skill not invoked.' };
    }

    const code = joinCodeBlocks(ctx.qualityTranscript);

    if (code.length > 0) {
      if (NAMING_PATTERN.test(code)) {
        const match = NAMING_PATTERN.exec(code)!;
        return { evalId: 'naming-convention', name: 'Resource naming convention', score: 1.0, reasoning: `Code uses the naming convention (found: "${match[0]}").` };
      }
      const hasStringNames = /"[a-z][a-z0-9_-]{3,}"/.test(code);
      if (!hasStringNames) {
        return { evalId: 'naming-convention', name: 'Resource naming convention', score: 0.5, reasoning: 'No resource name strings found in code — naming convention may not apply to this scenario.' };
      }
    }

    const parsed = await judgeWithClaude(`Does this Terraform response use or recommend the naming pattern:
  <resource_type>-<project>-<environment>-<region>-<identifier>
  e.g. "s3-payments-prod-us-east-1-receipts"

Score: 0.0=no pattern, 0.5=neutral/not applicable, 0.7=structured but different pattern, 1.0=this exact pattern

TASK: ${ctx.scenario.prompt}
RESPONSE: ${ctx.qualityTranscript.slice(0, 2000)}
JSON only: {"score": <0-1>, "reasoning": "<one sentence>"}`);
    return { evalId: 'naming-convention', name: 'Resource naming convention', score: Math.max(0, Math.min(1, parsed.score)), reasoning: parsed.reasoning };
  },
};

export default namingConventionEval;

// Subprocess entry point: called by papi via `tsx <file>` with EvalContext JSON on stdin
const chunks: Buffer[] = [];
process.stdin.on('data', (c: Buffer) => chunks.push(c));
process.stdin.on('end', async () => {
  const ctx: EvalContext = JSON.parse(Buffer.concat(chunks).toString());
  try {
    const result = await namingConventionEval.evaluate(ctx);
    process.stdout.write(JSON.stringify(result));
  } catch (err) {
    process.stderr.write(String(err));
    process.exit(1);
  }
});
