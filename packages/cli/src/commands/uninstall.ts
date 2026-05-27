import fs from 'fs/promises';
import path from 'path';
import { globalInstallDir, localInstallDir, skillFilename } from '../lib/install-target.js';

export async function uninstallCommand(
  skillName: string,
  opts: { local?: boolean }
): Promise<void> {
  const targetDir = opts.local ? localInstallDir() : globalInstallDir();
  const dest = path.join(targetDir, skillFilename(skillName));
  try {
    await fs.unlink(dest);
    console.log(`Uninstalled ${skillName} (removed ${dest})`);
  } catch {
    console.error(`Skill "${skillName}" is not installed at ${dest}`);
    process.exit(1);
  }
}
