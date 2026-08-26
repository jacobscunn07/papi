import { ESLint, type Linter } from 'eslint';
import { goPlugin } from './go-language.js';

// Namespace every eval-supplied rule is registered under. Must not collide with
// the `go` plugin key that the `go/go` language id points at.
const NS = 'papi';

/**
 * A rule as the language plugins define them. Rules receive tree-sitter nodes,
 * whose shape varies per grammar symbol, so evals take the loose type here and
 * narrow locally.
 */
export interface EvalRule {
  create(context: any): Record<string, (node: any) => void>;
}

/**
 * Lints Go source with the given rules, registered together and run in one pass.
 * Use `messagesFor` to pull out an individual rule's findings.
 */
export async function lintGo(
  source: string,
  rules: Record<string, EvalRule>,
): Promise<Linter.LintMessage[]> {
  const { plugins, language } = await goPlugin();

  const eslint = new ESLint({
    // There is deliberately no eslint.config.js in this repo — ESLint is used
    // here as a library, not as a project linter — so config lookup must be off.
    overrideConfigFile: true,
    overrideConfig: [
      {
        files: ['**/*.go'],
        plugins: { ...plugins, [NS]: { rules } } as any,
        language,
        rules: Object.fromEntries(Object.keys(rules).map((id) => [`${NS}/${id}`, 'error'])),
      },
    ],
  });

  const [result] = await eslint.lintText(source, { filePath: 'source.go' });
  return result?.messages ?? [];
}

/** Messages produced by one of the rules passed to lintGo. */
export function messagesFor(messages: Linter.LintMessage[], ruleId: string): Linter.LintMessage[] {
  return messages.filter((m) => m.ruleId === `${NS}/${ruleId}`);
}

/** Lints several Go files with the same rules, merging every file's messages. */
export async function lintGoSources(
  sources: string[],
  rules: Record<string, EvalRule>,
): Promise<Linter.LintMessage[]> {
  const all: Linter.LintMessage[] = [];
  for (const source of sources) {
    all.push(...(await lintGo(source, rules)));
  }
  return all;
}
