import { ESLint, type Linter } from 'eslint';
import json from '@eslint/json';
import markdown from '@eslint/markdown';

// Namespace every eval-supplied rule is registered under. Arbitrary, but it must
// not collide with the `json`/`markdown` plugin keys the `language` ids point at.
const NS = 'papi';

/**
 * A rule as the language plugins define them. The two plugins export precise
 * types (`JSONRuleDefinition`, `MarkdownRuleDefinition`), but they are generic
 * over visitor keys that differ per language, so evals hand us the loose shape
 * and keep their own node handling typed locally.
 */
export interface EvalRule {
  create(context: any): Record<string, (node: any) => void>;
}

async function lintWith(
  text: string,
  filePath: string,
  files: string,
  language: string,
  plugins: Record<string, unknown>,
  rules: Record<string, EvalRule>,
): Promise<Linter.LintMessage[]> {
  const eslint = new ESLint({
    // There is deliberately no eslint.config.js in this repo — ESLint is used
    // here as a library, not as a project linter — so config lookup must be off.
    overrideConfigFile: true,
    overrideConfig: [
      {
        files: [files],
        plugins: { ...plugins, [NS]: { rules } } as any,
        language,
        rules: Object.fromEntries(Object.keys(rules).map((id) => [`${NS}/${id}`, 'error'])),
      },
    ],
  });
  const [result] = await eslint.lintText(text, { filePath });
  return result?.messages ?? [];
}

/**
 * Lints an hcl2json document. The document is serialized and parsed back by
 * @eslint/json so rules see a real AST; reported positions refer to that JSON,
 * not to the original .tf (hcl2json does not carry source spans).
 *
 * Rules are registered together and run in one pass; use `messagesFor` to pull
 * out the findings of an individual rule.
 */
export function lintJson(
  doc: unknown,
  rules: Record<string, EvalRule>,
): Promise<Linter.LintMessage[]> {
  return lintWith(
    JSON.stringify(doc, null, 2),
    'terraform.json',
    '**/*.json',
    'json/json',
    { json },
    rules,
  );
}

/** Lints markdown. Uses GFM — tables do not exist in commonmark. */
export function lintMarkdown(
  md: string,
  rules: Record<string, EvalRule>,
): Promise<Linter.LintMessage[]> {
  return lintWith(md, 'SKILL.md', '**/*.md', 'markdown/gfm', { markdown }, rules);
}

/** Messages produced by one of the rules passed to lintJson / lintMarkdown. */
export function messagesFor(messages: Linter.LintMessage[], ruleId: string): Linter.LintMessage[] {
  return messages.filter((m) => m.ruleId === `${NS}/${ruleId}`);
}

/**
 * Dotted path of a Momoa `Member` node within the document, e.g.
 * `resource.aws_s3_bucket.this.count`. Array nesting is transparent, so for
 * hcl2json's `{resource: {<type>: {<name>: [body]}}}` shape this is exactly the
 * Terraform address of the attribute.
 */
export function memberPath(context: any, node: any): string[] {
  const ancestors: any[] = context.sourceCode.getAncestors(node);
  const names = ancestors.filter((a) => a.type === 'Member').map((a) => String(a.name.value));
  return [...names, String(node.name.value)];
}
