import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawn, execFileSync } from 'node:child_process';
import { once } from 'node:events';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createInterface } from 'node:readline';
import { SmartCardClient } from '../dist/index.js';

test('native WebSocket interoperates with the Go server and keeps replies connection scoped', {timeout: 30_000}, async t => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'smartcard-sdk-agent-'));
  let child, exited, a, b;
  t.after(async () => {
    a?.destroy();
    b?.destroy();
    if (child) {
      child.stdin.end();
      const timer = setTimeout(() => child.kill(), 5_000);
      try { await exited; } finally { clearTimeout(timer); }
    }
    fs.rmSync(temp, {recursive: true, force: true});
  });
  const executable = path.join(temp, process.platform === 'win32' ? 'testagent.exe' : 'testagent');
  execFileSync('go', ['build', '-o', executable, './tests/testagent'], {stdio: 'inherit'});
  child = spawn(executable, [], {stdio: ['pipe', 'pipe', 'inherit']});
  exited = once(child, 'exit');
  const lines = createInterface({input: child.stdout});
  const [url] = await once(lines, 'line');
  assert.match(url, /^ws:\/\/127\.0\.0\.1:\d+\/ws$/);

  function event(client, name) {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => { off(); reject(new Error(`Timed out waiting for ${name}`)); }, 5_000);
      const off = client.on(name, payload => { clearTimeout(timer); off(); resolve(payload); });
    });
  }
  a = new SmartCardClient({url, reconnect: false});
  b = new SmartCardClient({url, reconnect: false});
  const statusA = event(a, 'status');
  await a.connect();
  assert.equal((await statusA).selected, 'synthetic-reader');
  const statusB = event(b, 'status');
  await b.connect();
  await statusB;
  // Wait for a command after B's automatic status sync, whose result might
  // still be in flight after the status broadcast.
  await b.getStatus();
  const resultsB = [];
  b.on('command-result', result => resultsB.push(result));
  const cardA = event(a, 'card');
  const cardB = event(b, 'card');
  const result = await a.readNow();
  assert.equal(result.status, 'completed');
  assert.equal((await cardA).personal.name.full_name, 'SYNTHETIC CARD');
  assert.equal((await cardB).reader, 'synthetic-reader');
  await assert.rejects(a.refreshReaders(), error => error.code === 'COMMAND_BUSY' && error.result.code === 'reader_busy');
  assert.deepEqual(resultsB, []);
});
