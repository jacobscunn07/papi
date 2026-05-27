import fs from 'fs/promises';
import path from 'path';
import matter from 'gray-matter';

export interface SkillMeta {
  name: string;
  description: string;
  version: string;
  skillDir: string;
  skillMdPath: string;
}

/** Resolves the skills/ directory relative to this package's location */
function getSkillsRoot(): string {
  // packages/cli/dist/lib -> packages/cli -> packages -> repo root
  const repoRoot = path.resolve(new URL(import.meta.url).pathname, '../../../../..');
  return path.join(repoRoot, 'skills');
}

export async function discoverSkills(skillsRoot?: string): Promise<SkillMeta[]> {
  const root = skillsRoot ?? getSkillsRoot();
  let entries: string[];
  try {
    entries = await fs.readdir(root);
  } catch {
    return [];
  }

  const skills: SkillMeta[] = [];
  for (const entry of entries) {
    const skillDir = path.join(root, entry);
    const skillMdPath = path.join(skillDir, 'SKILL.md');
    try {
      const stat = await fs.stat(skillDir);
      if (!stat.isDirectory()) continue;
      const raw = await fs.readFile(skillMdPath, 'utf8');
      const { data } = matter(raw);
      if (!data.name) continue;
      skills.push({
        name: data.name as string,
        description: (data.description as string) ?? '',
        version: (data.version as string) ?? '0.0.0',
        skillDir,
        skillMdPath,
      });
    } catch {
      // skip directories without a valid SKILL.md
    }
  }
  return skills;
}

export async function findSkill(name: string, skillsRoot?: string): Promise<SkillMeta | null> {
  const all = await discoverSkills(skillsRoot);
  return all.find((s) => s.name === name) ?? null;
}
