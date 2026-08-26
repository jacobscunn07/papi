import type { Eval, EvalContext, EvalResult } from './types.js';
import { lintGoSources, messagesFor, type EvalRule } from './eslint-runner.js';
import { loadGoSource, runEval } from './utils.js';

const EVAL_ID = 'prefers-composition';
const EVAL_NAME = 'Prefer composition over inheritance';

const result = (score: number, reasoning: string): EvalResult => ({
  evalId: EVAL_ID,
  name: EVAL_NAME,
  score,
  reasoning,
});

/** Reports nodes of a single grammar type. */
function nodeTypeRule(type: string, describe: (node: any) => string): EvalRule {
  return {
    create(context) {
      return {
        [type](node: any) {
          context.report({ node, message: describe(node) });
        },
      };
    },
  };
}

/**
 * An embedded field is exactly a `field_declaration` carrying no `name` field —
 * the grammar marks `name` optional and `type` required. This replaces a line
 * shape heuristic that matched any indented bare identifier, and so counted the
 * members of a `const (... iota ...)` block as struct embedding.
 */
const embeddedFieldRule: EvalRule = {
  create(context) {
    return {
      field_declaration(node: any) {
        if (node.childForFieldName('name')) return;
        context.report({ node, message: node.text.trim().slice(0, 60) });
      },
    };
  },
};

const prefersCompositionEval: Eval = {
  id: EVAL_ID,
  name: EVAL_NAME,

  async evaluate(ctx: EvalContext): Promise<EvalResult> {
    const loaded = loadGoSource(ctx, EVAL_ID, EVAL_NAME);
    if ('result' in loaded) return loaded.result;

    const messages = await lintGoSources(loaded.sources, {
      embed: embeddedFieldRule,
      iface: nodeTypeRule('interface_type', () => 'interface'),
      struct: nodeTypeRule('struct_type', () => 'struct'),
    });

    const embeds = messagesFor(messages, 'embed');
    const interfaces = messagesFor(messages, 'iface').length;
    const structs = messagesFor(messages, 'struct').length;

    if (structs === 0 && interfaces === 0) {
      return result(1.0, 'No type definitions to evaluate for composition.');
    }
    if (interfaces >= 1 || embeds.length >= 1) {
      const how = [
        interfaces >= 1 ? `${interfaces} interface(s)` : '',
        embeds.length >= 1 ? `embedded field(s): ${embeds.map((m) => m.message).join(', ')}` : '',
      ].filter(Boolean).join(' and ');
      return result(1.0, `Uses ${how} (composition over inheritance).`);
    }
    if (structs >= 1) {
      return result(0.6, `${structs} concrete struct(s) with no embedding or interfaces — favor small interfaces or embedding for composition.`);
    }
    return result(0.5, 'Type design is unclear.');
  },
};

export default prefersCompositionEval;

runEval((ctx) => prefersCompositionEval.evaluate(ctx));
