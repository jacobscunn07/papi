import { mkdirSync } from 'fs';

const cacheDir = '/tmp/terraform-plugin-cache';
mkdirSync(cacheDir, { recursive: true });

// KEY=VALUE lines on stdout are injected as env vars into later phases.
console.log(`TF_PLUGIN_CACHE_DIR=${cacheDir}`);
