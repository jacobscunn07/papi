import os from 'os';
import path from 'path';

/** Returns ~/.claude/commands/ */
export function globalInstallDir(): string {
  return path.join(os.homedir(), '.claude', 'commands');
}

/** Returns <cwd>/.claude/commands/ */
export function localInstallDir(cwd = process.cwd()): string {
  return path.join(cwd, '.claude', 'commands');
}

/** Filename a skill gets when installed: <name>.md */
export function skillFilename(skillName: string): string {
  return `${skillName}.md`;
}
