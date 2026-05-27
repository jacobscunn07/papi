import fs from 'fs/promises';
import path from 'path';
import { discoverSkills, findSkill } from '../lib/skill-manifest.js';
import { globalInstallDir, localInstallDir, skillFilename } from '../lib/install-target.js';

export async function installCommand(
  skillName: string | undefined,
  opts: { all?: boolean; local?: boolean }
): Promise<void> {
  const targetDir = opts.local ? localInstallDir() : globalInstallDir();
  await fs.mkdir(targetDir, { recursive: true });

  if (opts.all) {
    const skills = await discoverSkills();
    if (skills.length === 0) {
      console.error('No skills found.');
      process.exit(1);
    }
    for (const skill of skills) {
      await installOne(skill.skillMdPath, skill.name, targetDir);
    }
    return;
  }

  if (!skillName) {
    console.error('Provide a skill name or use --all');
    process.exit(1);
  }

  const skill = await findSkill(skillName);
  if (!skill) {
    console.error(`Skill "${skillName}" not found.`);
    process.exit(1);
  }
  await installOne(skill.skillMdPath, skill.name, targetDir);
}

async function installOne(srcPath: string, name: string, targetDir: string): Promise<void> {
  const dest = path.join(targetDir, skillFilename(name));
  await fs.copyFile(srcPath, dest);
  console.log(`Installed ${name} -> ${dest}`);
}
