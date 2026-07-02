import type { Eval, EvalContext, EvalResult } from './types.js';
import { judgeWithClaude } from './utils.js';

// Matches ?ref= followed by a hex SHA (7–40 chars)
const SHA_REF = /\?ref=([0-9a-f]{7,40})\b/i;
// Bad patterns: tag (v1.2.3), branch names, registry URLs
const TAG_REF = /\?ref=(v?\d+\.\d+[\.\d]*)/i;
const BRANCH_REF = /\?ref=(main|master|develop|HEAD|release[\w/-]*)/i;
const REGISTRY_SOURCE = /source\s*=\s*"[^"]*registry\.terraform\.io/;

const gitShaSourceEval: Eval = {
  id: 'git-sha-source',
  name: 'Module source: git SHA only',

  async evaluate(ctx: EvalContext): Promise<EvalResult> {
    if (!ctx.invoked || !ctx.qualityTranscript) {
      return { evalId: 'git-sha-source', name: 'Module source: git SHA only', score: 0.0, reasoning: 'Skipped — skill not invoked.' };
    }

    const t = ctx.qualityTranscript;

    if (REGISTRY_SOURCE.test(t)) {
      return { evalId: 'git-sha-source', name: 'Module source: git SHA only', score: 0.1, reasoning: 'Response uses registry.terraform.io as module source — should use git SHA instead.' };
    }
    if (TAG_REF.test(t)) {
      return { evalId: 'git-sha-source', name: 'Module source: git SHA only', score: 0.1, reasoning: `Response uses a version tag ref (${TAG_REF.exec(t)![1]}) — should use a git SHA.` };
    }
    if (BRANCH_REF.test(t)) {
      return { evalId: 'git-sha-source', name: 'Module source: git SHA only', score: 0.1, reasoning: `Response uses a branch ref (${BRANCH_REF.exec(t)![1]}) — should use a git SHA.` };
    }
    if (SHA_REF.test(t)) {
      return { evalId: 'git-sha-source', name: 'Module source: git SHA only', score: 1.0, reasoning: `Response uses a git SHA ref (${SHA_REF.exec(t)![1]}).` };
    }

    const parsed = await judgeWithClaude(`Does this response recommend using a git commit SHA (not a tag or branch name) as the version reference for Terraform module sources? If the scenario doesn't involve module sources, score 0.5 (neutral).

Score guide:
- 0.0: Recommends tags, branches, or registry
- 0.5: Doesn't mention module versioning
- 0.8: Recommends pinning to a specific commit/SHA
- 1.0: Explicitly recommends git SHA and explains why tags/branches are unsafe

TASK: ${ctx.scenario.prompt}

RESPONSE: ${t.slice(0, 2000)}

JSON only: {"score": <0-1>, "reasoning": "<one sentence>"}`);
    return { evalId: 'git-sha-source', name: 'Module source: git SHA only', score: Math.max(0, Math.min(1, parsed.score)), reasoning: parsed.reasoning };
  },
};

export default gitShaSourceEval;

// Subprocess entry point: called by papi via `tsx <file>` with EvalContext JSON on stdin
const chunks: Buffer[] = [];
process.stdin.on('data', (c: Buffer) => chunks.push(c));
process.stdin.on('end', async () => {
  const ctx: EvalContext = JSON.parse(Buffer.concat(chunks).toString());
  try {
    const result = await gitShaSourceEval.evaluate(ctx);
    process.stdout.write(JSON.stringify(result));
  } catch (err) {
    process.stderr.write(String(err));
    process.exit(1);
  }
});
