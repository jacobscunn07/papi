import type { Eval, EvalContext, EvalResult } from './types.js';
import { lintJson, memberPath, messagesFor, type EvalRule } from './eslint-runner.js';
import { blockAttribute, loadTerraformJson } from './terraform-json.js';
import { clampScore, judgeWithClaude, runEval } from './utils.js';

const EVAL_ID = 'git-sha-source';
const EVAL_NAME = 'Module source: git SHA only';

const result = (score: number, reasoning: string): EvalResult => ({
  evalId: EVAL_ID,
  name: EVAL_NAME,
  score,
  reasoning,
});

// Matches ?ref= followed by a hex SHA (7–40 chars)
const SHA_REF = /\?ref=([0-9a-f]{7,40})\b/i;
// Bad patterns: tag (v1.2.3), branch names, registry sources
const TAG_REF = /\?ref=(v?\d+\.\d+[.\d]*)/i;
const BRANCH_REF = /\?ref=(main|master|develop|HEAD|release[\w/-]*)/i;
const REGISTRY_SOURCE = /registry\.terraform\.io/;

/**
 * Reports `module` block `source` values matching a pattern. Unlike the previous
 * transcript regex, this only ever sees real module sources — prose explaining
 * which refs to avoid can no longer trigger a violation.
 */
function moduleSourceRule(pattern: RegExp, describe: (address: string, value: string, match: string) => string): EvalRule {
  return {
    create(context) {
      return {
        Member(node) {
          const attr = blockAttribute(memberPath(context, node));
          if (!attr || attr.kind !== 'module' || attr.attr !== 'source') return;

          const value: unknown = node.value?.value;
          if (typeof value !== 'string') return;
          const match = pattern.exec(value);
          if (!match) return;
          context.report({ node, message: describe(attr.address, value, match[1] ?? match[0]) });
        },
      };
    },
  };
}

const gitShaSourceEval: Eval = {
  id: EVAL_ID,
  name: EVAL_NAME,

  async evaluate(ctx: EvalContext): Promise<EvalResult> {
    if (!ctx.invoked || !ctx.qualityTranscript) {
      return result(0.0, 'Skipped — skill not invoked.');
    }

    const { doc, source } = await loadTerraformJson(ctx);

    if (source !== 'none') {
      const messages = await lintJson(doc, {
        registry: moduleSourceRule(REGISTRY_SOURCE, (a) => `${a} sources from registry.terraform.io`),
        tag: moduleSourceRule(TAG_REF, (a, _v, m) => `${a} pins the version tag ${m}`),
        branch: moduleSourceRule(BRANCH_REF, (a, _v, m) => `${a} pins the branch ${m}`),
        sha: moduleSourceRule(SHA_REF, (a, _v, m) => `${a} pins the git SHA ${m}`),
      });

      const bad = [
        ...messagesFor(messages, 'registry'),
        ...messagesFor(messages, 'tag'),
        ...messagesFor(messages, 'branch'),
      ];
      if (bad.length > 0) {
        return result(0.1, `Module sources are not pinned to a git SHA: ${bad.map((m) => m.message).join('; ')}.`);
      }

      const shas = messagesFor(messages, 'sha');
      if (shas.length > 0) {
        return result(1.0, `Module sources pin git SHAs: ${shas.map((m) => m.message).join('; ')}.`);
      }
      // Module blocks exist but none carry a recognizable source — fall through
      // to the judge rather than guessing.
    }

    const parsed = await judgeWithClaude(`Does this response recommend using a git commit SHA (not a tag or branch name) as the version reference for Terraform module sources? If the scenario doesn't involve module sources, score 0.5 (neutral).

Score guide:
- 0.0: Recommends tags, branches, or registry
- 0.5: Doesn't mention module versioning
- 0.8: Recommends pinning to a specific commit/SHA
- 1.0: Explicitly recommends git SHA and explains why tags/branches are unsafe

TASK: ${ctx.scenario.prompt}

RESPONSE: ${ctx.qualityTranscript.slice(0, 2000)}

JSON only: {"score": <0-1>, "reasoning": "<one sentence>"}`);
    return result(clampScore(parsed.score), parsed.reasoning);
  },
};

export default gitShaSourceEval;

runEval((ctx) => gitShaSourceEval.evaluate(ctx));
