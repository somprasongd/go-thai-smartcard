import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { execFileSync } from 'node:child_process';

// A bootstrap release is published interactively before npm can trust CI.
// A tag rerun may skip it only when the registry has the identical artifact.
const pkg = JSON.parse(fs.readFileSync('package.json', 'utf8'));
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'smartcard-sdk-publish-'));
const npmCli = process.env.npm_execpath;
if (!npmCli) throw new Error('Run this script through npm run publish:release');
const npm = (args) => execFileSync(process.execPath, [npmCli, ...args], {encoding: 'utf8'});
try {
  const artifact = JSON.parse(npm(['pack', '--json', '--pack-destination', temp]))[0];
  const response = await fetch(`https://registry.npmjs.org/${encodeURIComponent(pkg.name)}/${pkg.version}`);
  if (response.ok) {
    const existing = await response.json();
    if (existing.dist?.integrity !== artifact.integrity) throw new Error('Version already exists with different contents');
    console.log(`${pkg.name}@${pkg.version} already published with identical contents`);
  } else if (response.status === 404) {
    execFileSync(process.execPath, [npmCli, 'publish', path.join(temp, artifact.filename), '--access', 'public'], {stdio: 'inherit'});
  } else {
    throw new Error(`Cannot check npm registry: HTTP ${response.status}`);
  }
} finally { fs.rmSync(temp, {recursive: true, force: true}); }
