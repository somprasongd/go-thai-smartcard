const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');

const html = fs.readFileSync(`${__dirname}/settings.html`, 'utf8');
const script = html.match(/<script>([\s\S]*?)<\/script>/)[1];
const config = {
  server: { listen: '127.0.0.1', port: 9898, transports: ['ws'], allowed_origins: ['*'] },
  card: { read_face_image: true, read_laser_id: true, read_nhso: false, reader: '' },
  logging: {mode:'file',max_size_mb:2,max_backups:0,max_age_days:0},
  tls: { enabled: false, port: 9899, cert_file: '', key_file: '' }
};
const settle = () => new Promise(resolve => setImmediate(resolve));

async function scenario(response, status = 200, alreadyRevealed = false, readerRefresh = false) {
  const elements = new Map();
  const element = id => {
    if (!elements.has(id)) elements.set(id, {
      value: '', checked: false, hidden: true, textContent: '', listeners: {},
      childNodes: [],
      addEventListener(type, callback) { this.listeners[type] = callback; },
      appendChild(child) { this.childNodes.push(child); },
      querySelectorAll: () => []
    });
    return elements.get(id);
  };
  const storage = new Map();
  const navigations = [];
  const context = {
    document: {
      getElementById: element,
      documentElement: { setAttribute: () => {}, classList: { toggle: () => {}, add: () => {}, remove: () => {} } },
      querySelectorAll: () => [],
      // loadReaders builds <option>s for the reader dropdown.
      createElement: tag => ({ tag, value: '', textContent: '' })
    }, URL,
    window: { location: { origin: 'http://127.0.0.1:9898', assign: url => navigations.push(url) }, confirm: () => true },
    localStorage: {getItem:key=>storage.get(key)||null,setItem:(key,value)=>storage.set(key,value),removeItem:key=>storage.delete(key)},
    navigator: { clipboard: { writeText: async () => {} } },
    fetch: async (_url, options) => _url === "/api/readers" ? { ok: true, json: async () => ({readers:["saved reader"]}) } : options ? {
      ok: status === 200, status, json: async () => response, text: async () => 'port is occupied'
    } : { ok: true, json: async () => ({ config: {...config, card: {...config.card, reader: readerRefresh ? "saved reader" : ""}}, version: 'old', token_set: false }) }
  };
  vm.runInNewContext(script, context);
  await settle();
  if (readerRefresh) {
    assert.equal(element('reader').value, 'saved reader');
    element('reader').value = '';
    await element('btn-reader-refresh').listeners.click();
    assert.equal(element('reader').value, '', 'refresh discarded all-readers selection');
  }
  if (alreadyRevealed) { element('token-reveal').hidden = false; element('token-value').textContent = 'previous-token'; }
  element('port').value = 9999;
  element('btn-save').listeners.click();
  await settle();
  return { element, navigations, storage };
}

(async () => {
  const policy = await scenario({version:'new'});
  assert.equal(policy.element('stale-mode').value,'auto');
  policy.element('stale-mode').value='delay';policy.element('stale-mode').listeners.change();
  assert.equal(policy.element('stale-delay-row').hidden,false);
  policy.element('stale-seconds').value=0;policy.element('btn-stale-save').listeners.click();
  assert.equal(policy.storage.has('smc.stalePolicy'),false);
  policy.element('stale-seconds').value=15;policy.element('btn-stale-save').listeners.click();
  assert.deepEqual(JSON.parse(policy.storage.get('smc.stalePolicy')),{mode:'delay',seconds:15});
  policy.element('stale-mode').value='keep';policy.element('stale-mode').listeners.change();
  assert.equal(policy.element('stale-delay-row').hidden,true);
  policy.element('btn-stale-save').listeners.click();
  assert.equal(JSON.parse(policy.storage.get('smc.stalePolicy')).mode,'keep');
  policy.element('stale-mode').value='auto';policy.element('btn-stale-save').listeners.click();
  assert.equal(policy.storage.has('smc.stalePolicy'),false);
  const moved = { version: 'new', endpoint_url: 'http://127.0.0.1:9999' };
  await scenario(moved, 200, false, true);
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
  // The token section follows exposure: hidden on loopback, shown beyond it.
  async function listenScenario(listen, setExpose, typeListen) {
    const elements = new Map();
    const element = id => {
      if (!elements.has(id)) elements.set(id, {
        value: '', checked: false, hidden: true, textContent: '', listeners: {},
        childNodes: [],
        addEventListener(type, callback) { this.listeners[type] = callback; },
        appendChild(child) { this.childNodes.push(child); },
        querySelectorAll: () => []
      });
      return elements.get(id);
    };
    const sent = [];
    const context = {
      document: {
        getElementById: element,
        documentElement: { setAttribute: () => {}, classList: { toggle: () => {}, add: () => {}, remove: () => {} } },
        querySelectorAll: () => [],
        createElement: tag => ({ tag, value: '', textContent: '' })
      }, URL,
      window: { location: { origin: 'http://127.0.0.1:9898', assign: () => {} }, confirm: () => true },
      navigator: { clipboard: { writeText: async () => {} } },
      fetch: async (_url, options) => options
        ? { ok: true, status: 200, json: async () => { assert.deepEqual(JSON.parse(options.body).config.logging,config.logging); sent.push(JSON.parse(options.body).config.server.listen); return { version: 'new' }; } }
        : { ok: true, json: async () => ({ config: { ...config, server: { ...config.server, listen } }, version: 'old', token_set: false }) }
    };
    vm.runInNewContext(script, context);
    await settle();
    const rows = {
      expose: !element('expose-row').hidden,
      text: !element('listen-row').hidden,
      token: !element('token-section').hidden
    };
    let tokenAfterExpose = null;
    if (setExpose !== undefined) {
      element('expose').checked = setExpose;
      element('expose').listeners.change();
      tokenAfterExpose = !element('token-section').hidden;
    }
    let tokenAfterType = null;
    if (typeListen !== undefined) {
      element('listen').value = typeListen;
      element('listen').listeners.input();
      tokenAfterType = !element('token-section').hidden;
    }
    element('btn-save').listeners.click();
    await settle();
    return { rows, tokenAfterExpose, tokenAfterType, listen: sent[0] };
  }

  let probe = await listenScenario('127.0.0.1');
  assert.deepEqual(probe.rows, { expose: true, text: false, token: false });
  assert.equal(probe.listen, '127.0.0.1');
  probe = await listenScenario('127.0.0.1', true);
  assert.equal(probe.listen, '0.0.0.0');
  assert.equal(probe.tokenAfterExpose, true);
  probe = await listenScenario('0.0.0.0');
  assert.deepEqual(probe.rows, { expose: true, text: false, token: true });
  assert.equal(probe.listen, '0.0.0.0');
  probe = await listenScenario('0.0.0.0', false);
  assert.equal(probe.listen, '127.0.0.1');
  assert.equal(probe.tokenAfterExpose, false);
  probe = await listenScenario('192.168.1.10');
  assert.deepEqual(probe.rows, { expose: false, text: true, token: true });
  assert.equal(probe.listen, '192.168.1.10');
  probe = await listenScenario('192.168.1.10', undefined, 'localhost');
  assert.equal(probe.tokenAfterType, false);
  probe = await listenScenario('localhost', undefined, '0.0.0.0');
  assert.equal(probe.tokenAfterType, true);
})().catch(error => { console.error(error); process.exitCode = 1; });
