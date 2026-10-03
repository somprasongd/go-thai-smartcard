import assert from 'node:assert/strict';
import { test } from 'node:test';
import { setTimeout as delay } from 'node:timers/promises';
import { SmartCardClient } from '../dist/index.js';

class Socket extends EventTarget {
  readyState = 0;
  sent = [];
  open() { this.readyState = 1; this.dispatchEvent(new Event('open')); }
  send(text) { this.sent.push(JSON.parse(text)); }
  close() { this.readyState = 3; this.dispatchEvent(new Event('close')); }
  receive(event, payload) {
    this.dispatchEvent(new MessageEvent('message', { data: JSON.stringify({event, payload}) }));
  }
  result(command, status, code) {
    this.receive('smc-command-result', {...command, status, code});
  }
}

function setup(t, options = {}) {
  const sockets = [];
  const client = new SmartCardClient({
    url: 'ws://localhost:9898/ws', reconnect: false,
    webSocketFactory(url) { const socket = new Socket(); socket.url = url; sockets.push(socket); return socket; },
    ...options,
  });
  t.after(() => client.destroy());
  return {client, sockets};
}

async function connected(t, options) {
  const fixture = setup(t, options);
  const promise = fixture.client.connect();
  fixture.sockets[0].open();
  await promise;
  // Complete the SDK's automatic status synchronization.
  fixture.sockets[0].result(fixture.sockets[0].sent[0], 'completed');
  return {...fixture, socket: fixture.sockets[0]};
}

test('accepted is intermediate; concurrent terminal results match request IDs', async t => {
  const {client, socket} = await connected(t);
  const first = client.readNow();
  const second = client.refreshReaders();
  const [a, b] = socket.sent.slice(1);
  assert.notEqual(a.request_id, b.request_id);
  assert.ok(a.request_id.length <= 128);
  let settled = false;
  first.then(() => { settled = true; });
  socket.result(a, 'accepted');
  await Promise.resolve();
  assert.equal(settled, false);
  socket.result(b, 'completed');
  assert.equal((await second).action, 'refresh-readers');
  socket.result(a, 'completed');
  assert.equal((await first).request_id, a.request_id);
});

test('busy and failed preserve machine readable agent codes', async t => {
  const {client, socket} = await connected(t);
  for (const [status, code, errorCode] of [
    ['busy', 'reader_busy', 'COMMAND_BUSY'], ['failed', 'operation_failed', 'COMMAND_FAILED'],
  ]) {
    const promise = client.readNow();
    const rejected = assert.rejects(promise, error => error.code === errorCode && error.result.code === code && error.outcome === status);
    socket.result(socket.sent.at(-1), status, code);
    await rejected;
  }
});

test('timeout is unknown; late success does not retry or settle another command', async t => {
  const {client, socket} = await connected(t, {commandTimeoutMs: 15});
  const promise = client.readNow();
  const command = socket.sent.at(-1);
  await assert.rejects(promise, error => error.code === 'COMMAND_TIMEOUT' && error.outcome === 'unknown');
  socket.result(command, 'completed');
  assert.equal(socket.sent.length, 2);
  const next = client.readNow();
  socket.result(socket.sent.at(-1), 'completed');
  await next;
});

test('connection loss settles work and reconnect synchronizes only status', async t => {
  const {client, socket, sockets} = await connected(t, {
    reconnect: true, reconnectMinDelayMs: 10, reconnectMaxDelayMs: 10,
  });
  const promise = client.readNow();
  const rejected = assert.rejects(promise, error => error.code === 'CONNECTION_LOST' && error.outcome === 'unknown');
  socket.close();
  await rejected;
  assert.equal(client.state, 'reconnecting');
  for (let i = 0; sockets.length < 2 && i < 50; i++) await delay(5);
  assert.equal(sockets.length, 2);
  sockets[1].open();
  assert.deepEqual(sockets[1].sent.map(command => command.action), ['get-status']);
  // Frames from an old connection cannot resolve new work or deliver old card data.
  let cards = 0;
  client.on('card', () => { cards++; });
  socket.receive('smc-data', {personal: null});
  assert.equal(cards, 0);
  sockets[1].result(sockets[1].sent[0], 'completed');
});

test('close/destroy cancel retries and timers; connect after destroy rejects', async t => {
  const {client, socket, sockets} = await connected(t, {
    reconnect: true, reconnectMinDelayMs: 10, reconnectMaxDelayMs: 10,
  });
  socket.close();
  client.destroy();
  await delay(30);
  assert.equal(sockets.length, 1);
  assert.equal(client.state, 'disconnected');
  await assert.rejects(client.connect(), {code: 'DESTROYED'});
  await assert.rejects(client.readNow(), {code: 'DESTROYED'});
});

test('handshake deadline rejects and closes the socket; repeated connect shares promise', async t => {
  const {client, sockets} = setup(t, {connectTimeoutMs: 10});
  const promise = client.connect();
  assert.equal(client.connect(), promise);
  await assert.rejects(promise, {code: 'CONNECT_TIMEOUT'});
  assert.equal(sockets[0].readyState, 3);
  assert.equal(client.state, 'disconnected');
});

test('pending limit, send failure and disconnect never leave a hanging promise', async t => {
  const {client, socket} = await connected(t, {maxPendingCommands: 1});
  const first = client.readNow();
  await assert.rejects(client.readNow(), {code: 'PENDING_LIMIT', outcome: 'not-sent'});
  const rejected = assert.rejects(first, {code: 'CONNECTION_CLOSED', outcome: 'unknown'});
  client.close();
  await rejected;
  await assert.rejects(client.readNow(), {code: 'NOT_CONNECTED'});
  const reconnect = client.connect();
  // The factory has made another socket.
  client.close();
  await assert.rejects(reconnect, {code: 'CONNECTION_CLOSED'});
  const other = await connected(t);
  other.socket.send = () => { throw new Error('synthetic send failure'); };
  await assert.rejects(other.client.readNow(), {code: 'SEND_FAILED'});
});

test('bad input is reported without leaking token or raw message; listener errors do not interrupt settlement', async t => {
  const {client, socket} = await connected(t, {token: 'synthetic-token'});
  const errors = [];
  client.on('error', error => errors.push(error));
  const off = client.on('card', () => { throw new Error('synthetic personal data'); });
  socket.receive('smc-data', {personal: null});
  off();
  socket.dispatchEvent(new MessageEvent('message', {data: 'synthetic-token'}));
  const promise = client.readNow();
  socket.result({...socket.sent.at(-1), action: 'get-status'}, 'completed');
  socket.result(socket.sent.at(-1), 'completed');
  await promise;
  assert.deepEqual(errors.map(error => error.code), ['LISTENER_ERROR', 'PROTOCOL_ERROR', 'PROTOCOL_ERROR']);
  assert.ok(errors.every(error => !String(error).includes('synthetic')));
  assert.equal(new URL(socket.url).searchParams.get('token'), 'synthetic-token');
});

test('card events remain independent of completed commands and are never cached', async t => {
  const {client, socket} = await connected(t);
  const cards = [];
  client.on('card', data => cards.push(data));
  const promise = client.readNow();
  socket.receive('smc-data', {personal: null, reader: 'synthetic-reader'});
  socket.result(socket.sent.at(-1), 'completed');
  assert.equal((await promise).status, 'completed');
  assert.equal(cards.length, 1);
  let replayed = false;
  client.on('card', () => { replayed = true; });
  assert.equal(replayed, false);
});

test('Go nil reader slices normalize to an empty list for no-reader status', async t => {
  const {client, socket} = await connected(t);
  let status;
  client.on('status', value => { status = value; });
  socket.receive('smc-status', {readers: null, selected: '', state: 'waiting', health: 'no-reader'});
  assert.deepEqual(status.readers, []);
  assert.equal(status.health, 'no-reader');
});

test('destroy from a connection listener stops opening or retrying sockets', async t => {
  const {client, sockets} = setup(t, {reconnect: true});
  client.on('connection', () => client.destroy());
  await assert.rejects(client.connect(), {code: 'CONNECTION_CLOSED'});
  assert.equal(sockets.length, 0);
});
