import { test } from 'node:test';
import assert from 'node:assert/strict';
import { gzipSync } from 'node:zlib';
import { createHash } from 'node:crypto';
import { verifyPublishedArchive } from '../scripts/archive.mjs';

const integrity = data => `sha512-${createHash('sha512').update(data).digest('base64')}`;

test('same archive with different gzip bytes is accepted only after registry integrity verification', () => {
  const tar = Buffer.from('synthetic archive contents '.repeat(200));
  const remote = gzipSync(tar, {level: 1});
  const local = gzipSync(tar, {level: 9});
  assert.equal(remote.equals(local), false);
  verifyPublishedArchive(remote, integrity(remote), local);
  assert.throws(() => verifyPublishedArchive(remote, integrity(local), local), /integrity verification/);
});

test('changed archive content is rejected even with a valid registry digest', () => {
  const remote = gzipSync(Buffer.from('original synthetic archive'));
  const local = gzipSync(Buffer.from('changed synthetic archive'));
  assert.throws(() => verifyPublishedArchive(remote, integrity(remote), local), /different contents/);
});
