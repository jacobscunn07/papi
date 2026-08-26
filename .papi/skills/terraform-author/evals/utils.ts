import { execFile } from 'child_process';
import { promisify } from 'util';
import type { EvalContext, EvalResult } from './types.js';

const execFileAsync = promisify(execFile);

/** Calls claude CLI as an LLM judge. Prompt must request JSON {score, reasoning}. */
export async function judgeWithClaude(prompt: string): Promise<{ score: number; reasoning: string }> {
  const { stdout } = await execFileAsync('claude', [
    '-p', prompt,
    '--model', 'claude-haiku-4-5-20251001',
    '--output-format', 'json',
    '--no-session-persistence',
  ], { maxBuffer: 1024 * 1024 });
  const outer = JSON.parse(stdout) as { result: string };
  let raw = outer.result.trim();
  if (raw.startsWith('```')) {
    raw = raw.replace(/^```(?:\w+)?\n?/, '').replace(/\n?```$/, '');
  }
  try {
    return JSON.parse(raw) as { score: number; reasoning: string };
  } catch {
    const jsonMatch = raw.match(/\{[^{}]*"score"[^{}]*\}/);
    if (jsonMatch) {
      return JSON.parse(jsonMatch[0]) as { score: number; reasoning: string };
    }
    return { score: 0.5, reasoning: 'LLM judge did not return valid JSON.' };
  }
}

/** A fenced markdown code block with its info string (```hcl -> lang "hcl"). */
export interface TaggedCodeBlock {
  lang: string;
  code: string;
}

/** Extracts all fenced code blocks along with their language tag. */
export function extractTaggedCodeBlocks(text: string): TaggedCodeBlock[] {
  const blocks: TaggedCodeBlock[] = [];
  const re = /```([\w-]*)\n([\s\S]*?)```/g;
  let match: RegExpExecArray | null;
  while ((match = re.exec(text)) !== null) {
    blocks.push({ lang: match[1].toLowerCase(), code: match[2] });
  }
  return blocks;
}

/** Clamps an LLM-supplied score into the 0..1 range evals must report. */
export function clampScore(score: number): number {
  return Math.max(0, Math.min(1, Number.isFinite(score) ? score : 0.5));
}

/** Subprocess entry point: reads EvalContext JSON from stdin, writes EvalResult to stdout. */
export function runEval(evaluate: (ctx: EvalContext) => Promise<EvalResult>): void {
  const chunks: Buffer[] = [];
  process.stdin.on('data', (c: Buffer) => chunks.push(c));
  process.stdin.on('end', async () => {
    try {
      const ctx: EvalContext = JSON.parse(Buffer.concat(chunks).toString());
      process.stdout.write(JSON.stringify(await evaluate(ctx)));
    } catch (err) {
      process.stderr.write(String(err));
      process.exit(1);
    }
  });
}
