#!/usr/bin/env node
// Verify that AGENTS.md and all inline copies contain the core rules from skills/assh/SKILL.md
const fs = require('fs');
const path = require('path');

const root = path.join(__dirname, '..');

const INVARIANTS = [
  'dangerous_command_requires_confirmation',
  'assh connect',
  'password',
];

const skill = fs.readFileSync(path.join(root, 'skills/assh/SKILL.md'), 'utf8');
const agents = fs.readFileSync(path.join(root, 'AGENTS.md'), 'utf8');
const copies = [['AGENTS.md', agents]];

const cursorRule = path.join(root, '.cursor/rules/assh.mdc');
const clineRule = path.join(root, '.clinerules/assh.md');
const copilotInstructions = path.join(root, '.github/copilot-instructions.md');

for (const f of [cursorRule, clineRule, copilotInstructions]) {
  if (fs.existsSync(f)) copies.push([path.relative(root, f), fs.readFileSync(f, 'utf8')]);
}

let failed = false;

for (const [name, content] of copies) {
  for (const phrase of INVARIANTS) {
    if (!content.includes(phrase)) {
      console.error(`${name} is missing invariant: "${phrase}"`);
      failed = true;
    }
  }
}

const skillInvariants = [...INVARIANTS, 'assh connect', 'assh session'];
for (const phrase of skillInvariants) {
  if (!skill.includes(phrase)) {
    console.error(`skills/assh/SKILL.md is missing invariant: "${phrase}"`);
    failed = true;
  }
}

if (failed) {
  console.error('Skill copies drifted. Update them from skills/assh/SKILL.md or AGENTS.md.');
  process.exit(1);
}

console.log('All skill copies match canonical source. OK');
