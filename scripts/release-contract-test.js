'use strict';

const fs = require('node:fs');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const pkg = require('../package.json');
const { target } = require('./platform');

const root = path.join(__dirname, '..');
const releaseRemote = 'https://github.com/izzzzzi/agent-assh.git';

function expectedArchives(version) {
  return [
    ['linux', 'x64'],
    ['linux', 'arm64'],
    ['darwin', 'x64'],
    ['darwin', 'arm64'],
    ['win32', 'x64'],
  ].map(([platform, arch]) => {
    const info = target(platform, arch);
    return `assh_${version}_${info.os}_${info.arch}${info.archiveExt}`;
  });
}

function verifyArtifactFiles(files, version = pkg.version) {
  for (const archive of expectedArchives(version)) {
    if (!files.includes(archive)) {
      throw new Error(`missing snapshot archive matching installer expectation: ${archive}`);
    }
  }

  if (!files.includes('checksums.txt')) {
    throw new Error('missing checksums.txt');
  }
}

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    cwd: root,
    stdio: options.stdio || 'pipe',
    encoding: 'utf8',
  });

  if (result.error) {
    throw result.error;
  }

  if (result.status !== 0) {
    const output = [result.stdout, result.stderr].filter(Boolean).join('\n').trim();
    throw new Error(`${command} ${args.join(' ')} failed${output ? `:\n${output}` : ''}`);
  }

  return (result.stdout || '').trim();
}

function git(args, options = {}) {
  return run('git', args, options);
}

function hasGitRef(ref) {
  const result = spawnSync('git', ['rev-parse', '--quiet', '--verify', ref], {
    cwd: root,
    stdio: 'ignore',
  });
  return result.status === 0;
}

function ensureReleaseGitState(version = pkg.version) {
  const tag = `v${version}`;
  let addedRemote = false;
  let addedTag = false;
  const cleanup = () => {
    if (addedTag) {
      git(['tag', '-d', tag]);
    }
    if (addedRemote) {
      git(['remote', 'remove', 'origin']);
    }
  };

  try {
    const remote = spawnSync('git', ['remote', 'get-url', 'origin'], {
      cwd: root,
      stdio: 'ignore',
    });
    if (remote.status !== 0) {
      git(['remote', 'add', 'origin', releaseRemote]);
      addedRemote = true;
    }

    if (!hasGitRef(`refs/tags/${tag}`)) {
      git(['tag', tag, 'HEAD']);
      addedTag = true;
    }

    return cleanup;
  } catch (error) {
    try {
      cleanup();
    } catch (cleanupError) {
      error.message = `${error.message}\ncleanup failed: ${cleanupError.message}`;
    }
    throw error;
  }
}

function goreleaserArgs() {
  if (spawnSync('goreleaser', ['--version'], { stdio: 'ignore' }).status === 0) {
    return ['goreleaser', ['release', '--snapshot', '--clean']];
  }

  return ['go', [
    'run',
    'github.com/goreleaser/goreleaser/v2@latest',
    'release',
    '--snapshot',
    '--clean',
  ]];
}

function artifactFiles(directory = path.join(root, 'dist')) {
  const files = [];

  function walk(current) {
    for (const entry of fs.readdirSync(current, { withFileTypes: true })) {
      const full = path.join(current, entry.name);
      if (entry.isDirectory()) {
        walk(full);
      } else {
        files.push(path.basename(full));
      }
    }
  }

  walk(directory);
  return files;
}

function main() {
  const cleanup = ensureReleaseGitState();

  try {
    const [command, args] = goreleaserArgs();
    run(command, args, { stdio: 'inherit' });
    verifyArtifactFiles(artifactFiles());
    console.log('release artifact contract ok');
  } finally {
    cleanup();
  }
}

if (require.main === module) {
  try {
    main();
  } catch (error) {
    console.error(error.message);
    process.exit(1);
  }
}

module.exports = {
  expectedArchives,
  verifyArtifactFiles,
};
