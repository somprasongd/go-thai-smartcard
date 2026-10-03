import { createHash } from 'node:crypto';
import { gunzipSync } from 'node:zlib';

// Node 24 and 25 can gzip the same npm tar archive differently. Verify the
// registry's compressed bytes first, then compare every uncompressed tar byte.
export function verifyPublishedArchive(remote, integrity, local) {
  const actual = `sha512-${createHash('sha512').update(remote).digest('base64')}`;
  if (actual !== integrity) throw new Error('Downloaded package failed registry integrity verification');
  if (!gunzipSync(remote).equals(gunzipSync(local))) throw new Error('Version already exists with different contents');
}
