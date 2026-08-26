import { existsSync, readdirSync, readFileSync, writeFileSync } from 'fs';
import { join } from 'path';
import { convertFiles, parse } from '@cdktf/hcl2json';
import type { EvalContext } from './types.js';
import { extractTaggedCodeBlocks } from './utils.js';

/** Where the Terraform under evaluation was recovered from. */
export type TerraformSource = 'workdir' | 'transcript' | 'none';

export interface TerraformDoc {
  /** hcl2json's rendering: { resource: { <type>: { <name>: [body] } }, module: { <name>: [body] }, ... } */
  doc: Record<string, unknown>;
  source: TerraformSource;
}

// Fence info strings that hold Terraform. `json` is excluded on purpose: a JSON
// block in a transcript is almost never Terraform's .tf.json form.
const TF_FENCES = new Set(['hcl', 'terraform', 'tf']);

// hcl2json instantiates a Go WASM module on first use, and papi runs every eval
// in its own subprocess — so the Terraform evals would each pay that cost. The
// conversion is cached in the scenario's work dir (unique per iteration and
// scenario, see loop.go's `scenarioDir`) so it happens once.
//
// The name must NOT end in `.tf.json`: Terraform reads those as configuration,
// and the post-quality hook runs `terraform init` in this same directory.
const CACHE_FILE = '.papi-terraform.json';

function isPlainObject(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

/** Merges two hcl2json docs the way Terraform merges several .tf files in a directory. */
function mergeDocs(
  a: Record<string, unknown>,
  b: Record<string, unknown>,
): Record<string, unknown> {
  const out: Record<string, unknown> = { ...a };
  for (const [key, bv] of Object.entries(b)) {
    const av = out[key];
    if (Array.isArray(av) && Array.isArray(bv)) {
      out[key] = [...av, ...bv];
    } else if (isPlainObject(av) && isPlainObject(bv)) {
      out[key] = mergeDocs(av, bv);
    } else {
      out[key] = bv;
    }
  }
  return out;
}

function hasTerraformFiles(dir: string): boolean {
  try {
    return readdirSync(dir).some((f) => f.endsWith('.tf') || f.endsWith('.tf.json'));
  } catch {
    return false;
  }
}

/** Converts the .tf files Claude wrote to disk. Null when there are none, or none parse. */
async function fromWorkDir(workDir: string): Promise<Record<string, unknown> | null> {
  // Checked up front so hcl2json does not log "No '.tf' files found" to stderr
  // on every scenario where Claude answered with prose only.
  if (!workDir || !existsSync(workDir) || !hasTerraformFiles(workDir)) return null;
  try {
    const doc = await convertFiles(workDir);
    return doc && Object.keys(doc).length > 0 ? (doc as Record<string, unknown>) : null;
  } catch {
    // convertFiles concatenates every .tf before parsing, so one syntax error
    // sinks the whole directory. Fall through to the transcript.
    return null;
  }
}

/** Parses fenced Terraform blocks out of the transcript. Null when none parse. */
async function fromTranscript(transcript: string): Promise<Record<string, unknown> | null> {
  const blocks = extractTaggedCodeBlocks(transcript).filter((b) => TF_FENCES.has(b.lang));
  let merged: Record<string, unknown> = {};
  let parsedAny = false;

  for (const [i, block] of blocks.entries()) {
    try {
      const doc = await parse(`block-${i}.tf`, block.code);
      if (doc && Object.keys(doc).length > 0) {
        merged = mergeDocs(merged, doc as Record<string, unknown>);
        parsedAny = true;
      }
    } catch {
      // Illustrative fragments ("count = 1" on its own) are not valid HCL by
      // themselves. Skip the block rather than discarding the whole transcript.
    }
  }
  return parsedAny ? merged : null;
}

/**
 * Resolves the Terraform document to evaluate, preferring what Claude actually
 * wrote to disk over what it pasted into the transcript.
 */
export async function loadTerraformJson(ctx: EvalContext): Promise<TerraformDoc> {
  const cachePath = ctx.workDir ? join(ctx.workDir, CACHE_FILE) : '';

  if (cachePath && existsSync(cachePath)) {
    try {
      return JSON.parse(readFileSync(cachePath, 'utf8')) as TerraformDoc;
    } catch {
      // Corrupt cache: fall through and rebuild it.
    }
  }

  let resolved: TerraformDoc = { doc: {}, source: 'none' };
  const onDisk = await fromWorkDir(ctx.workDir);
  if (onDisk) {
    resolved = { doc: onDisk, source: 'workdir' };
  } else {
    const inTranscript = await fromTranscript(ctx.qualityTranscript ?? '');
    if (inTranscript) resolved = { doc: inTranscript, source: 'transcript' };
  }

  if (cachePath) {
    try {
      writeFileSync(cachePath, JSON.stringify(resolved));
    } catch {
      // Caching is an optimization; a read-only work dir is not an eval failure.
    }
  }
  return resolved;
}

/** A block-body attribute located in an hcl2json document. */
export interface BlockAttribute {
  /** Top-level block kind: resource, data, module, variable, output, ... */
  kind: string;
  /** Resource/data type (e.g. aws_s3_bucket). Empty for single-label blocks. */
  type: string;
  /** Block label (e.g. `this` in resource "aws_s3_bucket" "this"). */
  name: string;
  /** Attribute name within the block body (e.g. count, source). */
  attr: string;
  /** Terraform address of the containing block, for use in messages. */
  address: string;
}

// Blocks hcl2json renders with two labels (kind.type.name) rather than one (kind.name).
const TWO_LABEL_BLOCKS = new Set(['resource', 'data']);

/**
 * Interprets a `memberPath` as an attribute of a Terraform block body, or returns
 * null when the path points somewhere else (a nested object, a top-level stray
 * from a partial snippet, a block label rather than an attribute).
 */
export function blockAttribute(path: string[]): BlockAttribute | null {
  const [kind] = path;
  if (!kind) return null;

  if (TWO_LABEL_BLOCKS.has(kind)) {
    if (path.length !== 4) return null;
    const [, type, name, attr] = path;
    return { kind, type, name, attr, address: `${kind}.${type}.${name}` };
  }

  if (path.length !== 3) return null;
  const [, name, attr] = path;
  return { kind, type: '', name, attr, address: `${kind}.${name}` };
}
