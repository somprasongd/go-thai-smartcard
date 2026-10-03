import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { execFileSync } from 'node:child_process';

// Install the actual tarball outside the checkout: a directory install can hide
// missing exports or missing dist files behind a symlink to the source tree.
const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'smartcard-sdk-pack-'));
const npmCli = process.env.npm_execpath;
if (!npmCli) throw new Error('Run this script through npm run check');
const run = (args, cwd) => execFileSync(process.execPath, [npmCli, ...args], {
  cwd, encoding: 'utf8', stdio: ['ignore', 'pipe', 'inherit'],
});
try {
  const packed = JSON.parse(run(['pack', '--json', '--pack-destination', temp], process.cwd()))[0];
  const paths = new Set(packed.files.map(file => file.path));
  for (const required of ['dist/index.js', 'dist/index.d.ts', 'dist/types.js', 'dist/types.d.ts', 'LICENSE', 'NOTICE']) {
    if (!paths.has(required)) throw new Error(`Missing package file: ${required}`);
  }
  for (const file of packed.files) {
    if (!file.path.startsWith('dist/') && !['package.json', 'README.md', 'CHANGELOG.md', 'LICENSE', 'NOTICE'].includes(file.path)) {
      throw new Error(`Unexpected package file: ${file.path}`);
    }
  }
  fs.writeFileSync(path.join(temp, 'package.json'), '{"private":true,"type":"module"}');
  run(['install', '--ignore-scripts', '--no-audit', '--no-fund', path.join(temp, packed.filename)], temp);
  const smoke = `import { SmartCardClient, SmartCardClientError } from '@somprasongd/thai-smartcard-client';
const client = new SmartCardClient({url: 'ws://127.0.0.1:9898/ws'});
if (client.state !== 'disconnected' || typeof SmartCardClientError !== 'function') throw new Error('Invalid exports');
client.destroy();`;
  fs.writeFileSync(path.join(temp, 'smoke.mjs'), smoke);
  execFileSync(process.execPath, [path.join(temp, 'smoke.mjs')], {cwd: temp, stdio: 'inherit'});
  fs.copyFileSync(path.resolve('../../examples/web-client/consumer.ts'), path.join(temp, 'consumer.ts'));
  execFileSync(process.execPath, [path.resolve('node_modules/typescript/bin/tsc'),
    '--noEmit', '--strict', '--target', 'ES2022', '--module', 'NodeNext', 'consumer.ts'], {
    cwd: temp, stdio: 'inherit',
  });
  console.log('Packed package installs, imports and typechecks successfully');
} finally {
  fs.rmSync(temp, {recursive: true, force: true});
}
