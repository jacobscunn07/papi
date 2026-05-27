import { execFile } from 'child_process';
import { promisify } from 'util';

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

/** Extracts the contents of all fenced code blocks from a markdown string. */
export function extractCodeBlocks(text: string): string[] {
  const blocks: string[] = [];
  const re = /```[\w]*\n([\s\S]*?)```/g;
  let match: RegExpExecArray | null;
  while ((match = re.exec(text)) !== null) {
    blocks.push(match[1]);
  }
  return blocks;
}

/** Joins all code blocks into a single string for easier pattern matching. */
export function joinCodeBlocks(text: string): string {
  return extractCodeBlocks(text).join('\n');
}
