#!/usr/bin/env node
/**
 * JSN npm postinstall script
 *
 * Downloads the correct platform-specific binary from GitHub releases
 * and places it in the `binary/` directory for the wrapper to find.
 */

const https = require('https');
const fs = require('fs');
const path = require('path');
const os = require('os');
const { execSync } = require('child_process');

const REPO = 'jacebenson/jsn';
const BINARY_DIR = path.join(__dirname, '..', 'binary');

const PLATFORM_MAP = {
  darwin: 'darwin',
  linux: 'linux',
  win32: 'windows',
};

const ARCH_MAP = {
  x64: 'amd64',
  arm64: 'arm64',
};

function detectPlatform() {
  const platform = os.platform();
  const arch = os.arch();

  const mappedPlatform = PLATFORM_MAP[platform];
  const mappedArch = ARCH_MAP[arch];

  if (!mappedPlatform) {
    throw new Error(
      `Unsupported platform: ${platform}. ` +
        `Supported platforms: ${Object.keys(PLATFORM_MAP).join(', ')}`
    );
  }

  if (!mappedArch) {
    throw new Error(
      `Unsupported architecture: ${arch}. ` +
        `Supported architectures: ${Object.keys(ARCH_MAP).join(', ')}`
    );
  }

  // Windows arm64 can run amd64 binaries, so fall back
  if (mappedPlatform === 'windows' && mappedArch === 'arm64') {
    console.log(
      'Warning: Windows arm64 build not available, using amd64 binary.'
    );
    return { platform: 'windows', arch: 'amd64', fallback: true };
  }

  return { platform: mappedPlatform, arch: mappedArch };
}

function getVersion() {
  // npm sets this during install
  if (process.env.npm_package_version) {
    return process.env.npm_package_version;
  }

  // Fallback: read package.json
  const pkgPath = path.join(__dirname, '..', 'package.json');
  const pkg = JSON.parse(fs.readFileSync(pkgPath, 'utf8'));
  return pkg.version;
}

function downloadFile(url, dest) {
  return new Promise((resolve, reject) => {
    const file = fs.createWriteStream(dest);
    https
      .get(url, { timeout: 30000 }, (response) => {
        if (response.statusCode === 301 || response.statusCode === 302) {
          // Follow redirect
          return downloadFile(response.headers.location, dest)
            .then(resolve)
            .catch(reject);
        }

        if (response.statusCode !== 200) {
          reject(
            new Error(
              `Download failed: HTTP ${response.statusCode} for ${url}`
            )
          );
          return;
        }

        response.pipe(file);
        file.on('finish', () => {
          file.close(resolve);
        });
      })
      .on('error', (err) => {
        fs.unlink(dest, () => {});
        reject(err);
      })
      .on('timeout', () => {
        fs.unlink(dest, () => {});
        reject(new Error('Download timeout'));
      });
  });
}

function extractArchive(archivePath, destDir) {
  const ext = path.extname(archivePath);
  const isWindows = os.platform() === 'win32';

  if (archivePath.endsWith('.zip')) {
    // Use PowerShell on Windows, unzip elsewhere
    if (isWindows) {
      execSync(
        `powershell -Command "Expand-Archive -Path '${archivePath}' -DestinationPath '${destDir}' -Force"`,
        { stdio: 'inherit' }
      );
    } else {
      execSync(`unzip -q -o "${archivePath}" -d "${destDir}"`, {
        stdio: 'inherit',
      });
    }
  } else if (archivePath.endsWith('.tar.gz')) {
    execSync(`tar -xzf "${archivePath}" -C "${destDir}"`, {
      stdio: 'inherit',
    });
  } else {
    throw new Error(`Unknown archive format: ${archivePath}`);
  }
}

function findBinary(extractDir) {
  const binaryName = os.platform() === 'win32' ? 'jsn.exe' : 'jsn';

  // The archive contains just the binary directly
  const directPath = path.join(extractDir, binaryName);
  if (fs.existsSync(directPath)) {
    return directPath;
  }

  // Sometimes archives have nested directories
  const entries = fs.readdirSync(extractDir);
  for (const entry of entries) {
    const entryPath = path.join(extractDir, entry);
    const stat = fs.statSync(entryPath);
    if (stat.isDirectory()) {
      const nestedPath = path.join(entryPath, binaryName);
      if (fs.existsSync(nestedPath)) {
        return nestedPath;
      }
    }
  }

  throw new Error(
    `Could not find ${binaryName} in extracted archive. Contents: ${entries.join(', ')}`
  );
}

async function main() {
  // Skip if running in development (from git repo)
  if (fs.existsSync(path.join(__dirname, '..', 'go.mod'))) {
    console.log('Development environment detected. Skipping binary download.');
    console.log('Build the binary manually: go build ./cmd/jsn');
    return;
  }

  const { platform, arch } = detectPlatform();
  const version = getVersion();

  // Development placeholder version
  if (version === '0.0.0') {
    console.log('Warning: version is 0.0.0, skipping binary download.');
    console.log('This is expected when installing from source.');
    return;
  }

  const ext = platform === 'windows' ? 'zip' : 'tar.gz';
  const filename = `jsn_v${version}_${platform}_${arch}.${ext}`;
  const url = `https://github.com/${REPO}/releases/download/v${version}/${filename}`;

  console.log(`Downloading JSN v${version} for ${platform}/${arch}...`);
  console.log(`  ${url}`);

  // Ensure binary directory exists
  if (!fs.existsSync(BINARY_DIR)) {
    fs.mkdirSync(BINARY_DIR, { recursive: true });
  }

  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'jsn-install-'));
  const archivePath = path.join(tmpDir, filename);

  try {
    await downloadFile(url, archivePath);
    console.log('Extracting...');
    extractArchive(archivePath, tmpDir);

    const extractedBinary = findBinary(tmpDir);
    const binaryName = platform === 'windows' ? 'jsn.exe' : 'jsn';
    const finalPath = path.join(BINARY_DIR, binaryName);

    // Move binary to final location
    fs.copyFileSync(extractedBinary, finalPath);

    // Make executable on Unix
    if (platform !== 'windows') {
      fs.chmodSync(finalPath, 0o755);
    }

    console.log(`Installed JSN binary to ${finalPath}`);
  } catch (err) {
    console.error(`Error: ${err.message}`);
    console.error('');
    console.error('Failed to download the JSN binary.');
    console.error('You can still use the CLI if you have a binary installed manually.');
    console.error('Download from: https://github.com/jacebenson/jsn/releases');

    // Don't fail the npm install — the user can fix this later
    process.exit(0);
  } finally {
    // Cleanup temp directory
    try {
      fs.rmSync(tmpDir, { recursive: true, force: true });
    } catch {
      // Ignore cleanup errors
    }
  }
}

main().catch((err) => {
  console.error(`Unexpected error: ${err.message}`);
  process.exit(0); // Don't fail npm install
});
