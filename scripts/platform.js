'use strict';

function target(platform = process.platform, arch = process.arch) {
  const osMap = {
    linux: 'linux',
    darwin: 'darwin',
    win32: 'windows',
  };
  const archMap = {
    x64: 'amd64',
    arm64: 'arm64',
  };

  const os = osMap[platform];
  if (!os) {
    throw new Error(`Unsupported platform: ${platform}`);
  }

  const mappedArch = archMap[arch];
  if (!mappedArch) {
    throw new Error(`Unsupported architecture: ${arch}`);
  }

  if (os === 'windows' && mappedArch === 'arm64') {
    throw new Error('Unsupported platform: windows/arm64');
  }

  return {
    os,
    arch: mappedArch,
    ext: os === 'windows' ? '.exe' : '',
    archiveExt: os === 'windows' ? '.zip' : '.tar.gz',
  };
}

module.exports = { target };
