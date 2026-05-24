#!/usr/bin/env node
import { execSync } from 'child_process';
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';

const __dirname = dirname(fileURLToPath(import.meta.url));
const pkg = JSON.parse(readFileSync(join(__dirname, '..', 'package.json'), 'utf8'));

const type = process.argv[2];
if (!type || !['patch', 'minor', 'major'].includes(type)) {
  console.error('Usage: npm run release -- <patch|minor|major>');
  process.exit(1);
}

let branch;
try {
  branch = execSync('git rev-parse --abbrev-ref HEAD', { encoding: 'utf8' }).trim();
} catch {
  console.error('Error: Not in a git repository');
  process.exit(1);
}

if (branch !== 'main' && branch !== 'nodejs') {
  console.error(`Error: Releases can only be made from 'main' or 'nodejs' branch (currently on '${branch}')`);
  process.exit(1);
}

const target = branch === 'main' ? 'go' : 'node';
const prefix = target === 'go' ? 'go-v' : 'node-v';

console.log('');
console.log(`🚀 Releasing ${target.toUpperCase()} version from '${branch}'`);
console.log(`   Bump: ${type}`);
console.log('');

if (target === 'node') {
  // Run tests
  console.log('⏳ Running tests...');
  try {
    execSync('npm test', { stdio: 'inherit' });
  } catch {
    console.error('');
    console.error('❌ Tests failed. Release aborted.');
    process.exit(1);
  }

  // Bump version with correct tag prefix
  console.log('');
  console.log(`⏳ Bumping version (${type})...`);
  execSync(`npm version ${type} --tag-version-prefix=${prefix}`, { stdio: 'inherit' });

  // Push commits and tags
  console.log('');
  console.log('⏳ Pushing to origin...');
  execSync('git push && git push --tags', { stdio: 'inherit' });

  // Show final state
  const newPkg = JSON.parse(readFileSync(join(__dirname, '..', 'package.json'), 'utf8'));
  console.log('');
  console.log('✅ Node.js release complete!');
  console.log(`   Version: ${newPkg.version}`);
  console.log(`   Tag:     ${prefix}${newPkg.version}`);
  console.log('');
} else {
  // Go release: find latest go-v tag, increment, create new tag
  let latestTag;
  try {
    latestTag = execSync('git tag --list "go-v*" --sort=-v:refname', { encoding: 'utf8' }).trim().split('\n')[0];
  } catch {
    latestTag = '';
  }

  let newVersion;
  if (!latestTag) {
    newVersion = '1.0.0';
  } else {
    const currentVersion = latestTag.replace('go-v', '');
    const parts = currentVersion.split('.').map(Number);
    if (type === 'major') {
      parts[0] += 1;
      parts[1] = 0;
      parts[2] = 0;
    } else if (type === 'minor') {
      parts[1] += 1;
      parts[2] = 0;
    } else {
      parts[2] += 1;
    }
    newVersion = parts.join('.');
  }

  const newTag = `go-v${newVersion}`;

  console.log(`   Latest tag: ${latestTag || '(none)'}`);
  console.log(`   New tag:    ${newTag}`);
  console.log('');
  console.log(`⏳ Creating tag ${newTag}...`);
  execSync(`git tag ${newTag}`, { stdio: 'inherit' });

  console.log('');
  console.log('⏳ Pushing tag to origin...');
  execSync('git push origin --tags', { stdio: 'inherit' });

  console.log('');
  console.log('✅ Go release complete!');
  console.log(`   Tag: ${newTag}`);
  console.log('');
}
