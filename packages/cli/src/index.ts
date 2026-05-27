#!/usr/bin/env node
import { program } from 'commander';
import { installCommand } from './commands/install.js';
import { listCommand } from './commands/list.js';
import { infoCommand } from './commands/info.js';
import { uninstallCommand } from './commands/uninstall.js';

program
  .name('skills')
  .description('Install and manage Claude Code skills')
  .version('1.0.0');

program
  .command('install [skill-name]')
  .description('Install a skill into ~/.claude/commands/')
  .option('--all', 'Install all available skills')
  .option('--local', 'Install into project-local .claude/commands/')
  .action(installCommand);

program
  .command('list')
  .description('List available skills')
  .option('--installed', 'Show only installed skills')
  .action(listCommand);

program
  .command('info <skill-name>')
  .description('Show skill details')
  .action(infoCommand);

program
  .command('uninstall <skill-name>')
  .description('Remove an installed skill')
  .option('--local', 'Remove from project-local .claude/commands/')
  .action(uninstallCommand);

program.parse();
