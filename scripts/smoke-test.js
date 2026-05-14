'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const { target } = require('./platform');

const root = path.join(__dirname, '..');
const nativeDir = path.join(root, 'native');

assert.deepEqual(target('linux', 'x64'), {
  os: 'linux',
  arch: 'amd64',
  ext: '',
  archiveExt: '.tar.gz',
});
assert.deepEqual(target('darwin', 'arm64'), {
  os: 'darwin',
  arch: 'arm64',
  ext: '',
  archiveExt: '.tar.gz',
});
assert.deepEqual(target('win32', 'x64'), {
  os: 'windows',
  arch: 'amd64',
  ext: '.exe',
  archiveExt: '.zip',
});
assert.throws(() => target('win32', 'arm64'), /windows\/arm64/);
assert.throws(() => target('freebsd', 'x64'), /Unsupported platform/);
assert.throws(() => target('linux', 'ia32'), /Unsupported architecture/);

const info = target();
const binaryPath = path.join(nativeDir, `assh${info.ext}`);
const fake = process.platform === 'win32'
  ? '#!/usr/bin/env node\r\nconsole.log(`assh smoke ${process.argv.slice(2).join(" ")}`);\r\n'
  : '#!/bin/sh\necho "assh smoke $*"\n';

const nativeExisted = fs.existsSync(nativeDir);
const binaryExisted = fs.existsSync(binaryPath);
const originalBinary = binaryExisted ? fs.readFileSync(binaryPath) : null;
const originalMode = binaryExisted ? fs.statSync(binaryPath).mode & 0o777 : null;

try {
  fs.mkdirSync(nativeDir, { recursive: true });
  fs.writeFileSync(binaryPath, fake, { mode: 0o755 });
  fs.chmodSync(binaryPath, 0o755);

  const result = spawnSync(process.execPath, [path.join(root, 'bin', 'assh.js'), 'arg-one'], {
    cwd: root,
    encoding: 'utf8',
  });

  assert.equal(result.status, 0, result.stderr || result.stdout);
  assert.match(result.stdout, /assh smoke/);

  console.log('smoke ok');
} finally {
  if (binaryExisted) {
    fs.writeFileSync(binaryPath, originalBinary, { mode: originalMode });
    fs.chmodSync(binaryPath, originalMode);
  } else {
    fs.rmSync(binaryPath, { force: true });
  }

  if (!nativeExisted && fs.existsSync(nativeDir) && fs.readdirSync(nativeDir).length === 0) {
    fs.rmdirSync(nativeDir);
  }
}
