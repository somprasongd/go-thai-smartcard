const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');

const html = fs.readFileSync(`${__dirname}/settings.html`, 'utf8');
const script = html.match(/<script>([\s\S]*?)<\/script>/)[1];
const config = {
  server: { listen: '127.0.0.1', port: 9898, transports: ['ws'], allowed_origins: ['*'] },
  card: { read_face_image: true, read_laser_id: true, read_nhso: false, reader: '' },
  tls: { enabled: false, port: 9899, cert_file: '', key_file: '' }
};
const settle = () => new Promise(resolve => setImmediate(resolve));

async function scenario(response, status = 200, alreadyRevealed = false) {
  const elements = new Map();
  const element = id => {
    if (!elements.has(id)) elements.set(id, {
      value: '', checked: false, hidden: true, textContent: '', listeners: {},
      addEventListener(type, callback) { this.listeners[type] = callback; }
    });
    return elements.get(id);
  };
  const navigations = [];
  const context = {
    document: { getElementById: element }, URL,
    window: { location: { origin: 'http://127.0.0.1:9898', assign: url => navigations.push(url) }, confirm: () => true },
    navigator: { clipboard: { writeText: async () => {} } },
    fetch: async (_url, options) => options ? {
      ok: status === 200, status, json: async () => response, text: async () => 'port is occupied'
    } : { ok: true, json: async () => ({ config, version: 'old', token_set: false }) }
  };
  vm.runInNewContext(script, context);
  await settle();
  if (alreadyRevealed) { element('token-reveal').hidden = false; element('token-value').textContent = 'previous-token'; }
  element('port').value = 9999;
  element('btn-save').listeners.click();
  await settle();
  return { element, navigations };
}

(async () => {
  const moved = { version: 'new', endpoint_url: 'http://127.0.0.1:9999' };
  let result = await scenario(moved);
  assert.deepEqual(result.navigations, ['http://127.0.0.1:9999/settings']);
  result = await scenario({ ...moved, token: 'shown-once' });
  assert.deepEqual(result.navigations, []);
  assert.equal(result.element('token-value').textContent, 'shown-once');
  assert.equal(result.element('endpoint-moved').hidden, false);
  assert.equal(result.element('endpoint-link').href, 'http://127.0.0.1:9999/settings');
  result = await scenario(moved, 200, true);
  assert.deepEqual(result.navigations, []);
  assert.equal(result.element('token-value').textContent, 'previous-token');
  result = await scenario(moved, 500);
  assert.deepEqual(result.navigations, []);
  assert.equal(result.element('save-status').className, 'error');
  result = await scenario({ version: 'new', endpoint_url: 'http://127.0.0.1:9898' });
  assert.deepEqual(result.navigations, []);

  // The two standard listens render the Ollama-style expose checkbox and the
  // checkbox maps back onto those values; a custom bind keeps the text field.
  async function listenScenario(listen, setExpose) {
    const elements = new Map();
    const element = id => {
      if (!elements.has(id)) elements.set(id, {
        value: '', checked: false, hidden: true, textContent: '', listeners: {},
        addEventListener(type, callback) { this.listeners[type] = callback; }
      });
      return elements.get(id);
    };
    const sent = [];
    const context = {
      document: { getElementById: element }, URL,
      window: { location: { origin: 'http://127.0.0.1:9898', assign: () => {} }, confirm: () => true },
      navigator: { clipboard: { writeText: async () => {} } },
      fetch: async (_url, options) => options
        ? { ok: true, status: 200, json: async () => { sent.push(JSON.parse(options.body).config.server.listen); return { version: 'new' }; } }
        : { ok: true, json: async () => ({ config: { ...config, server: { ...config.server, listen } }, version: 'old', token_set: false }) }
    };
    vm.runInNewContext(script, context);
    await settle();
    const rows = { expose: !element('expose-row').hidden, text: !element('listen-row').hidden };
    if (setExpose !== undefined) element('expose').checked = setExpose;
    element('btn-save').listeners.click();
    await settle();
    return { rows, listen: sent[0] };
  }

  let probe = await listenScenario('127.0.0.1');
  assert.deepEqual(probe.rows, { expose: true, text: false });
  assert.equal(probe.listen, '127.0.0.1');
  probe = await listenScenario('127.0.0.1', true);
  assert.equal(probe.listen, '0.0.0.0');
  probe = await listenScenario('0.0.0.0');
  assert.deepEqual(probe.rows, { expose: true, text: false });
  assert.equal(probe.listen, '0.0.0.0');
  probe = await listenScenario('0.0.0.0', false);
  assert.equal(probe.listen, '127.0.0.1');
  probe = await listenScenario('192.168.1.10');
  assert.deepEqual(probe.rows, { expose: false, text: true });
  assert.equal(probe.listen, '192.168.1.10');
})().catch(error => { console.error(error); process.exitCode = 1; });
