import { execFile } from 'child_process';
import { promisify } from 'util';

const execFileAsync = promisify(execFile);

async function judgeWithClaude(prompt) {
  const { stdout } = await execFileAsync('claude', [
    '-p', prompt,
    '--model', 'claude-haiku-4-5-20251001',
    '--output-format', 'json',
    '--no-session-persistence',
  ], { maxBuffer: 1024 * 1024 });
  const outer = JSON.parse(stdout);
  let raw = outer.result.trim();
  if (raw.startsWith('```')) {
    raw = raw.replace(/^```(?:\w+)?\n?/, '').replace(/\n?```$/, '');
  }
  try {
    return JSON.parse(raw);
  } catch {
    const jsonMatch = raw.match(/\{[^{}]*"score"[^{}]*\}/);
    if (jsonMatch) return JSON.parse(jsonMatch[0]);
    return { score: 0.5, reasoning: 'LLM judge did not return valid JSON.' };
  }
}

const EVAL_ID = 'terraform-aws-modules';
const EVAL_NAME = 'Use terraform-aws-modules for AWS';

async function evaluate(ctx) {
  if (!ctx.invoked || !ctx.qualityTranscript) {
    return { evalId: EVAL_ID, name: EVAL_NAME, score: 0.0, reasoning: 'Skipped — skill not invoked.' };
  }

  if (ctx.qualityTranscript.includes('github.com/terraform-aws-modules')) {
    return { evalId: EVAL_ID, name: EVAL_NAME, score: 1.0, reasoning: 'Response references github.com/terraform-aws-modules.' };
  }

  const parsed = await judgeWithClaude(`First determine if the task involves creating or configuring AWS resources (VPC, EC2, S3, RDS, ECS, etc.).

If NOT an AWS resource creation task: {"score": 0.5, "reasoning": "Scenario is not about creating AWS resources — eval not applicable."}

If it IS an AWS task, score whether the response recommends modules from github.com/terraform-aws-modules:
- 0.1: Uses raw aws_* resources directly
- 0.4: Mentions modules but not terraform-aws-modules specifically
- 0.7: Recommends a community module but not terraform-aws-modules
- 1.0: Explicitly recommends github.com/terraform-aws-modules

TASK: ${ctx.scenario.prompt}

RESPONSE: ${ctx.qualityTranscript.slice(0, 2000)}

JSON only: {"score": <0-1>, "reasoning": "<one sentence>"}`);
  return { evalId: EVAL_ID, name: EVAL_NAME, score: Math.max(0, Math.min(1, parsed.score)), reasoning: parsed.reasoning };
}

const chunks = [];
process.stdin.on('data', (c) => chunks.push(c));
process.stdin.on('end', async () => {
  const ctx = JSON.parse(Buffer.concat(chunks).toString());
  try {
    const result = await evaluate(ctx);
    process.stdout.write(JSON.stringify(result));
  } catch (err) {
    process.stderr.write(String(err));
    process.exit(1);
  }
});
