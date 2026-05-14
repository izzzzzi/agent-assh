'use strict';

const crypto = require('node:crypto');
const fs = require('node:fs');
const https = require('node:https');
const os = require('node:os');
const path = require('node:path');
const { execFileSync } = require('node:child_process');
const pkg = require('../package.json');
const { target } = require('./platform');

const root = path.join(__dirname, '..');
const nativeDir = path.join(root, 'native');

function ensureNativeDir() {
  fs.mkdirSync(nativeDir, { recursive: true });
}

function binaryPath(info) {
  return path.join(nativeDir, `assh${info.ext}`);
}

function writeFakeExecutable(info) {
  ensureNativeDir();
  const body = process.platform === 'win32'
    ? '#!/usr/bin/env node\r\nconsole.log(`assh smoke ${process.argv.slice(2).join(" ")}`);\r\n'
    : '#!/bin/sh\necho "assh smoke $*"\n';
  fs.writeFileSync(binaryPath(info), body, { mode: 0o755 });
  fs.chmodSync(binaryPath(info), 0o755);
}

function download(url, destination) {
  return new Promise((resolve, reject) => {
    const request = https.get(url, (response) => {
      if ([301, 302, 303, 307, 308].includes(response.statusCode)) {
        response.resume();
        download(response.headers.location, destination).then(resolve, reject);
        return;
      }

      if (response.statusCode !== 200) {
        response.resume();
        reject(new Error(`Download failed (${response.statusCode}): ${url}`));
        return;
      }

      const file = fs.createWriteStream(destination);
      response.pipe(file);
      file.on('finish', () => file.close(resolve));
      file.on('error', reject);
    });

    request.on('error', reject);
  });
}

function checksumFor(checksums, archive) {
  for (const line of checksums.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed) {
      continue;
    }

    const match = trimmed.match(/^([a-fA-F0-9]{64})\s+\*?(.+)$/);
    if (match && path.basename(match[2]) === archive) {
      return match[1].toLowerCase();
    }
  }

  throw new Error(`No checksum found for ${archive}`);
}

function sha256(filePath) {
  const hash = crypto.createHash('sha256');
  hash.update(fs.readFileSync(filePath));
  return hash.digest('hex');
}

function verifyChecksum(archivePath, checksumsPath, archive) {
  const expected = checksumFor(fs.readFileSync(checksumsPath, 'utf8'), archive);
  const actual = sha256(archivePath);
  if (actual !== expected) {
    throw new Error(`Checksum mismatch for ${archive}: expected ${expected}, got ${actual}`);
  }
}

function powershellCommand() {
  for (const candidate of ['powershell.exe', 'powershell', 'pwsh']) {
    try {
      execFileSync(candidate, ['-NoProfile', '-Command', '$PSVersionTable.PSVersion.ToString()'], {
        stdio: 'ignore',
      });
      return candidate;
    } catch (_) {
      // Try the next PowerShell executable name.
    }
  }

  throw new Error('PowerShell is required to extract Windows zip archives');
}

function extractArchive(archivePath, destination, info) {
  fs.mkdirSync(destination, { recursive: true });

  if (info.archiveExt === '.zip') {
    const ps = powershellCommand();
    execFileSync(ps, [
      '-NoProfile',
      '-Command',
      'Expand-Archive -LiteralPath $args[0] -DestinationPath $args[1] -Force',
      archivePath,
      destination,
    ], { stdio: 'inherit' });
    return;
  }

  execFileSync('tar', ['-xzf', archivePath, '-C', destination], { stdio: 'inherit' });
}

function findExtractedBinary(directory, info) {
  const wanted = `assh${info.ext}`;
  const entries = fs.readdirSync(directory, { withFileTypes: true });

  for (const entry of entries) {
    const entryPath = path.join(directory, entry.name);
    if (entry.isFile() && entry.name === wanted) {
      return entryPath;
    }
    if (entry.isDirectory()) {
      const found = findExtractedBinary(entryPath, info);
      if (found) {
        return found;
      }
    }
  }

  return null;
}

async function main() {
  const info = target();

  if (process.env.AGENT_ASSH_SKIP_DOWNLOAD === '1') {
    writeFakeExecutable(info);
    return;
  }

  const version = `v${pkg.version}`;
  const archive = `assh_${pkg.version}_${info.os}_${info.arch}${info.archiveExt}`;
  const baseUrl = `https://github.com/agent-ssh/assh/releases/download/${version}`;
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-assh-'));
  const archivePath = path.join(tmp, archive);
  const checksumsPath = path.join(tmp, 'checksums.txt');
  const extractDir = path.join(tmp, 'extract');

  try {
    await download(`${baseUrl}/${archive}`, archivePath);
    await download(`${baseUrl}/checksums.txt`, checksumsPath);
    verifyChecksum(archivePath, checksumsPath, archive);
    extractArchive(archivePath, extractDir, info);

    const extracted = findExtractedBinary(extractDir, info);
    if (!extracted) {
      throw new Error(`Archive did not contain assh${info.ext}`);
    }

    ensureNativeDir();
    fs.copyFileSync(extracted, binaryPath(info));
    fs.chmodSync(binaryPath(info), 0o755);
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
}

main().catch((error) => {
  console.error(error.message);
  process.exit(1);
});
