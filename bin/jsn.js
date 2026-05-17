#!/usr/bin/env node
/**
 * JSN CLI binary wrapper
 *
 * This script finds the downloaded platform-specific binary and spawns it,
 * forwarding all arguments, stdin, stdout, and stderr.
 */

const { spawn } = require('child_process');
const path = require('path');
const fs = require('fs');
const os = require('os');

function getBinaryPath() {
  const binaryDir = path.join(__dirname, '..', 'binary');
  const binaryName = os.platform() === 'win32' ? 'jsn.exe' : 'jsn';
  const binaryPath = path.join(binaryDir, binaryName);

  if (fs.existsSync(binaryPath)) {
    return binaryPath;
  }

  // Fallback: check if running from source/build
  const devPath = path.join(__dirname, '..', 'jsn');
  if (fs.existsSync(devPath)) {
    return devPath;
  }

  // Another fallback for dev
  const devPath2 = path.join(__dirname, '..', 'jsn.exe');
  if (fs.existsSync(devPath2)) {
    return devPath2;
  }

  console.error('Error: JSN binary not found.');
  console.error('');
  console.error('The binary should have been downloaded during npm install.');
  console.error('Try reinstalling the package:');
  console.error('  npm uninstall -g @jacebenson/jsn');
  console.error('  npm install -g @jacebenson/jsn');
  console.error('');
  console.error('Or download manually from:');
  console.error('  https://github.com/jacebenson/jsn/releases');
  process.exit(1);
}

const binaryPath = getBinaryPath();
const child = spawn(binaryPath, process.argv.slice(2), {
  stdio: 'inherit',
  windowsHide: true,
});

child.on('exit', (code) => {
  process.exit(code ?? 0);
});

child.on('error', (err) => {
  console.error(`Failed to start JSN: ${err.message}`);
  process.exit(1);
});
