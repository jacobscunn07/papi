import fs from 'fs/promises';
import path from 'path';
import { discoverSkills } from '../lib/skill-manifest.js';
import { globalInstallDir, skillFilename } from '../lib/install-target.js';

export async function listCommand(opts: { installed?: boolean }): Promise<void> {
  const skills = await discoverSkills();

  if (skills.length === 0) {
    console.log('No skills found.');
    return;
  }

  const installDir = globalInstallDir();
  const rows = await Promise.all(
    skills.map(async (s) => {
      let installed = false;
      try {
        await fs.access(path.join(installDir, skillFilename(s.name)));
        installed = true;
      } catch {}
      return { ...s, installed };
    })
  );

  const filtered = opts.installed ? rows.filter((r) => r.installed) : rows;

  for (const r of filtered) {
    const tag = r.installed ? '[installed]' : '';
    console.log(`${r.name.padEnd(24)} v${r.version.padEnd(8)} ${tag}`);
    if (r.description) {
      console.log(`  ${r.description.slice(0, 80)}`);
    }
  }
}
