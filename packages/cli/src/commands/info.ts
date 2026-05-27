import fs from 'fs/promises';
import path from 'path';
import matter from 'gray-matter';
import { findSkill } from '../lib/skill-manifest.js';
import { globalInstallDir, skillFilename } from '../lib/install-target.js';

export async function infoCommand(skillName: string): Promise<void> {
  const skill = await findSkill(skillName);
  if (!skill) {
    console.error(`Skill "${skillName}" not found.`);
    process.exit(1);
  }

  const raw = await fs.readFile(skill.skillMdPath, 'utf8');
  const { data, content } = matter(raw);

  let installed = false;
  try {
    await fs.access(path.join(globalInstallDir(), skillFilename(skillName)));
    installed = true;
  } catch {}

  console.log(`Name:        ${data.name}`);
  console.log(`Version:     ${data.version ?? 'n/a'}`);
  console.log(`Installed:   ${installed ? 'yes' : 'no'}`);
  console.log(`Description:\n  ${data.description ?? ''}`);
  console.log(`\n--- Content preview ---`);
  console.log(content.trim().slice(0, 400));
  if (content.trim().length > 400) console.log('...');
}
