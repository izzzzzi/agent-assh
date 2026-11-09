#!/usr/bin/env node
// skills/assh/ is the only hand-written copy of the agent instructions. Every
// other copy is generated from it, so they cannot drift:
//
//   node scripts/check-skill-copies.js --write   regenerate the copies
//   node scripts/check-skill-copies.js           fail if a copy is stale (CI)
const fs = require('fs');
const path = require('path');

const root = path.join(__dirname, '..');
const read = (relative) => fs.readFileSync(path.join(root, relative), 'utf8');

const REFERENCES = ['connect', 'session', 'transfer', 'server', 'security'];
const GENERATED = '<!-- Generated from skills/assh/ by scripts/check-skill-copies.js. Edit the skill, not this file. -->';

// The skill body without its frontmatter, split at the reference list: `core`
// is what an agent needs on every SSH task.
const body = read('skills/assh/SKILL.md').replace(/^---\n[\s\S]*?\n---\n+/, '');
const marker = '\n## References\n';
if (!body.includes(marker)) {
  throw new Error('skills/assh/SKILL.md must end with a "## References" section');
}
const core = body.slice(0, body.indexOf(marker)).trimEnd();

// Push every heading one level down, leaving `# comment` lines inside code
// fences alone.
function demoteHeadings(markdown) {
  let inFence = false;
  return markdown
    .split('\n')
    .map((line) => {
      if (line.startsWith('```')) {
        inFence = !inFence;
        return line;
      }
      return !inFence && /^#+ /.test(line) ? `#${line}` : line;
    })
    .join('\n');
}

const references = REFERENCES.map((name) =>
  demoteHeadings(read(`skills/assh/references/${name}.md`)).trim()
).join('\n\n');

const compact = `${core}\n\n## More detail\n\nFull guide: \`AGENT_INSTRUCTIONS.md\` in the agent-assh npm package. Flags: \`assh <command> --help\`.\n`;

const cursorFrontmatter = [
  '---',
  'description: Use assh for SSH access — bootstraps connections, manages tmux sessions, keeps output out of agent context.',
  'globs:',
  'alwaysApply: true',
  '---',
  '',
].join('\n');

// path -> expected content
const copies = {
  'AGENTS.md': `${GENERATED}\n\n${body.replaceAll('](references/', '](skills/assh/references/')}`,
  'SYSTEM_PROMPT_snippet.md': `${GENERATED}\n\n${compact}`,
  'AGENT_INSTRUCTIONS.md': `${GENERATED}\n\n${core}\n\n${references}\n`,
  '.clinerules/assh.md': `${GENERATED}\n\n${compact}`,
  '.cursor/rules/assh.mdc': `${cursorFrontmatter}\n${GENERATED}\n\n${compact}`,
  // Embedded into the binary and printed by `assh prompt`.
  'internal/cli/agent_prompt.md': compact,
};

const write = process.argv.includes('--write');
const stale = [];

for (const [relative, expected] of Object.entries(copies)) {
  const full = path.join(root, relative);
  const actual = fs.existsSync(full) ? fs.readFileSync(full, 'utf8') : null;
  if (actual === expected) {
    continue;
  }
  if (write) {
    fs.mkdirSync(path.dirname(full), { recursive: true });
    fs.writeFileSync(full, expected);
    console.log(`wrote ${relative}`);
  } else {
    stale.push(relative);
  }
}

if (stale.length > 0) {
  console.error(`Stale copies of skills/assh/:\n  ${stale.join('\n  ')}`);
  console.error('Run: node scripts/check-skill-copies.js --write');
  process.exit(1);
}

console.log('All skill copies match skills/assh/. OK');
