import { execFileSync } from 'child_process';
import { mkdirSync } from 'fs';

const cacheDir = '/tmp/terraform-plugin-cache';
mkdirSync(cacheDir, { recursive: true });

// Warm the provider cache for the terraform-valid eval. Best-effort: a work dir
// with no valid Terraform is expected, so failures are logged, not fatal.
try {
  const out = execFileSync('terraform', [`-chdir=${process.env.WORK_DIR}`, 'init', '-backend=false', '-no-color'], {
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
    env: { ...process.env, TF_PLUGIN_CACHE_DIR: cacheDir, TF_INPUT: '0' },
  });
  process.stdout.write(out);
} catch (err) {
  const e = err as { stdout?: string; stderr?: string };
  process.stdout.write(`terraform init failed: ${(e.stderr || e.stdout || String(err)).trim()}\n`);
}
