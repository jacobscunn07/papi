import { createRequire } from 'module';
import { TextSourceCodeBase, VisitNodeStep } from '@eslint/plugin-kit';
import { Language as TSLanguage, Parser, type Node as TSNode, type Tree } from 'web-tree-sitter';

/**
 * A minimal ESLint language for Go, so Go evals can be written as ESLint rules
 * that visit real syntax nodes instead of running regexes over source text.
 *
 * Go is parsed by tree-sitter (`tree-sitter-go` ships a prebuilt .wasm, so this
 * needs no Go toolchain). The shape below mirrors @eslint/json's JSONLanguage /
 * JSONSourceCode, which are the reference implementations of this interface.
 */

/** tree-sitter's Point is 0-based; ESLint reports lines from 1 and columns from 1. */
function toPosition(point: { row: number; column: number }) {
  return { line: point.row + 1, column: point.column + 1 };
}

class GoSourceCode extends TextSourceCodeBase {
  #steps: VisitNodeStep[] | undefined;

  constructor({ text, ast }: { text: string; ast: TSNode }) {
    super({ text, ast });
  }

  getLoc(node: TSNode) {
    return { start: toPosition(node.startPosition), end: toPosition(node.endPosition) };
  }

  getRange(node: TSNode): [number, number] {
    return [node.startIndex, node.endIndex];
  }

  /**
   * web-tree-sitter hands out a fresh Node wrapper on each access, so an
   * identity-keyed parent map (what @eslint/json builds) would not work here.
   * tree-sitter exposes the parent link directly, and `getAncestors()` in the
   * base class is defined purely in terms of this method.
   */
  getParent(node: TSNode): TSNode | undefined {
    return node.parent ?? undefined;
  }

  /** Depth-first enter/exit steps, cached — the tree does not mutate. */
  traverse(): Iterable<VisitNodeStep> {
    if (this.#steps) return this.#steps.values();

    const steps: VisitNodeStep[] = (this.#steps = []);
    const visit = (node: TSNode): void => {
      const parent = node.parent ?? null;
      steps.push(new VisitNodeStep({ target: node, phase: 1, args: [node, parent] }));
      for (const child of node.namedChildren) {
        if (child) visit(child);
      }
      steps.push(new VisitNodeStep({ target: node, phase: 2, args: [node, parent] }));
    };
    visit(this.ast as TSNode);
    return steps;
  }
}

class GoLanguage {
  fileType = 'text' as const;
  lineStart = 1 as const;
  columnStart = 1 as const;
  /** tree-sitter nodes expose their grammar symbol as `type`, which is what rules key off. */
  nodeTypeKey = 'type' as const;

  #parser: Parser;

  constructor(parser: Parser) {
    this.#parser = parser;
  }

  validateLanguageOptions(): void {
    // No options are supported.
  }

  parse(file: { body: string }) {
    const tree: Tree | null = this.#parser.parse(file.body);
    if (!tree) {
      return { ok: false as const, errors: [new Error('tree-sitter failed to parse the Go source.')] };
    }
    // A tree containing ERROR nodes is still worth linting, and `gofmt-clean`
    // already owns the "does this parse at all" question — so syntax errors are
    // deliberately not turned into fatal ESLint problems here.
    return { ok: true as const, ast: tree.rootNode };
  }

  createSourceCode(file: { body: string }, parseResult: { ast: TSNode }) {
    return new GoSourceCode({ text: file.body, ast: parseResult.ast });
  }
}

// Parser setup is async and instantiates a WASM module, but ESLint's
// Language.parse() is synchronous — so the parser is prepared once, up front,
// and the ready instance is handed to GoLanguage.
let parserPromise: Promise<Parser> | undefined;

function getParser(): Promise<Parser> {
  parserPromise ??= (async () => {
    await Parser.init();
    const require = createRequire(__filename);
    const go = await TSLanguage.load(require.resolve('tree-sitter-go/tree-sitter-go.wasm'));
    const parser = new Parser();
    parser.setLanguage(go);
    return parser;
  })();
  return parserPromise;
}

/** Builds the plugin/language pair for a lint run. Awaits parser readiness. */
export async function goPlugin(): Promise<{ plugins: Record<string, unknown>; language: string }> {
  const parser = await getParser();
  return {
    plugins: { go: { languages: { go: new GoLanguage(parser) } } },
    language: 'go/go',
  };
}
