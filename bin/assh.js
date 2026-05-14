#!/usr/bin/env node
'use strict';

const path = require('node:path');
const fs = require('node:fs');
const { spawnSync } = require('node:child_process');

const ext = process.platform === 'win32' ? '.exe' : '';
const binary = path.join(__dirname, '..', 'native', `assh${ext}`);
const args = process.argv.slice(2);
let command = binary;
let commandArgs = args;

if (!fs.existsSync(binary)) {
  console.error(`assh binary not found at ${binary}; reinstall agent-assh or rerun postinstall`);
  process.exit(1);
}

if (process.platform === 'win32') {
  const header = fs.readFileSync(binary).subarray(0, 2);
  if (header.toString('ascii') !== 'MZ') {
    command = process.execPath;
    commandArgs = [binary, ...args];
  }
}

const result = spawnSync(command, commandArgs, {
  stdio: 'inherit',
});

if (result.error) {
  console.error(result.error.message);
  process.exit(1);
}

if (result.signal) {
  process.kill(process.pid, result.signal);
}

process.exit(result.status === null ? 1 : result.status);
