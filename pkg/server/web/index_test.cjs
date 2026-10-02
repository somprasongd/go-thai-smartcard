const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const html = fs.readFileSync(`${__dirname}/index.html`, 'utf8');
let script = html.match(/<script>\s*([\s\S]*?)<\/script>/)[1];
// Expose the closure's functions without changing the production page.
script = script.replace(/\}\)\(\);\s*$/, 'globalThis.page = {store, boot, cidValid, clearData, handleEvent, setConn, stalePolicy, copyText, applyStalePolicy, sendCommand, pendingCommands, setWS: value => {ws=value}}; })();');
function scenario(prefs,policy) {
  const clipboard=[];
  const storage=new Map();
  if(prefs) storage.set("smc.prefs",JSON.stringify(prefs));
  if(policy) storage.set("smc.stalePolicy",JSON.stringify(policy));
  const timers=new Map(), listeners={};let timerId=0;
  const elements = new Map();
  const make = () => ({hidden:false, src:'', style:{}, dataset:{}, childNodes:[], listeners:{},
    classList:{toggle(){},add(){},remove(){}},
    setAttribute(k,v){ this[k]=v; }, removeAttribute(k){ delete this[k]; },
    appendChild(child){this.childNodes.push(child);return child;},
    querySelectorAll(){return [];}, addEventListener(k,v){this.listeners[k]=v;}});
  const element = id => { if (!elements.has(id)) elements.set(id,make()); return elements.get(id); };
  const context = {
    document:{readyState:'loading', documentElement:make(), head:make(), body:make(),
      getElementById:element, createElement:make, createTextNode:()=>make(),
      querySelectorAll:()=>[], addEventListener(){}},
    localStorage:{getItem:key=>storage.get(key)||null,setItem:(key,value)=>storage.set(key,value)},
    window:{addEventListener:(name,fn)=>listeners[name]=fn,confirm:()=>true,location:{protocol:'http:',host:'localhost',search:''},fetch:()=>new Promise(()=>{})},
    fetch:()=>new Promise(()=>{}), navigator:{clipboard:{writeText:async text=>clipboard.push(text)}}, setTimeout(fn,ms){const id=++timerId;timers.set(id,{fn,ms});return id;},clearTimeout(id){timers.delete(id);},
    URLSearchParams, WebSocket:class {static OPEN=1}, console
  };
  vm.runInNewContext(script,context);
  return {page:context.page,element,window:context.window,timers,storage,listeners,clipboard};
}
let test=scenario({privacy:'kiosk',mask:false,maskphoto:false,parts:true,nameparts:true,json:true,autoclear:30,lang:'en',dataLang:'both'});
test.page.boot();
for (const [key,value] of Object.entries({mask:false,maskphoto:false,parts:true,nameparts:true,json:true,autoclear:30,lang:'en',dataLang:'both'})) {
  assert.equal(test.page.store.ui[key],value,`persisted ${key} changed on reload`);
}
test=scenario(); test.page.boot();
assert.equal(test.page.store.ui.mask,true);
assert.equal(test.page.store.ui.maskphoto,true);
test=scenario({lang:'both'});
assert.equal(test.page.store.ui.lang,'th');
assert.equal(test.page.store.ui.dataLang,'both');
for(let i=0;i<100;i++) {
  const prefix=String(100000000000+i);
  const sum=[...prefix].reduce((acc,d,j)=>acc+Number(d)*(13-j),0);
  const check=(11-sum%11)%10;
  assert.equal(test.page.cidValid(prefix+check),true);
  assert.equal(test.page.cidValid(prefix+(check+1)%10),false);
}
test.element('lightbox').hidden=false;
test.element('lightbox-img').src='data:image/jpeg;base64,c3ludGhldGlj';
test.page.clearData();
assert.equal(test.element('lightbox').hidden,true);
assert.equal(test.element('lightbox-img').src,undefined);
console.log('card page preferences, checksum and portrait clearing passed');

test.element('lightbox').hidden=false;
test.element('lightbox-img').src='data:image/jpeg;base64,c3ludGhldGlj';
test.page.handleEvent('smc-removed',{},'ws');
assert.equal(test.element('lightbox').hidden,true);
assert.equal(test.element('lightbox-img').src,undefined);

const commands=[];
test.page.setWS({readyState:1,send:raw=>commands.push(JSON.parse(raw))});
test.window.smcSocket={connected:true,emit(){throw new Error('duplicate command on socket.io')}};
assert.equal(test.page.sendCommand({action:'read-now'}),true);
assert.equal(commands.length,1);
const requestId=commands[0].request_id;
assert.ok(requestId);
assert.equal(test.page.pendingCommands.size,1);
test.page.handleEvent('smc-command-result',{request_id:requestId,status:'accepted'},'ws');
assert.equal(test.page.pendingCommands.size,1);
test.page.handleEvent('smc-command-result',{request_id:requestId,status:'completed'},'ws');
assert.equal(test.page.pendingCommands.size,0);

function read(t) {
 t.page.handleEvent('smc-data',{cid:'synthetic',reader:'test reader'},'ws');
}
// One surviving transport retains freshness; reconnect alone does not restore it.
test=scenario({privacy:'counter',lang:'en'}, {mode:'keep',seconds:30});
test.page.setConn('ws',true);test.page.setConn('sio',true);read(test);
test.page.setConn('ws',false);
assert.equal(test.page.store.stale,false);
test.element('lightbox').hidden=false;test.element('lightbox-img').src='synthetic';
test.page.setConn('sio',false);
assert.equal(test.element('lightbox').hidden,true);
assert.equal(test.element('lightbox-img').src,undefined);
assert.equal(test.page.store.stale,true);
assert.equal(test.element('stale-banner').hidden,false);
assert.match(test.element('stale-banner').textContent,/Connection lost.*Last read/);
test.page.setConn('ws',true);
assert.equal(test.page.store.stale,true);
assert.match(test.element('stale-banner').textContent,/waiting for a new read/);
test.page.handleEvent('smc-status',{state:'card-present',readers:[]},'ws');
assert.equal(test.page.store.stale,true);
read(test);assert.equal(test.page.store.stale,false);
assert.equal(test.element('stale-banner').hidden,true);
// Preset defaults and an explicit override.
for (const [privacy,clears] of [['kiosk',true],['counter',false],['dev',false]]) {
 const tr=scenario({privacy});tr.page.setConn('ws',true);read(tr);tr.page.setConn('ws',false);
 assert.equal(tr.page.store.data===null,clears);
}
test=scenario({privacy:'kiosk'}, {mode:'keep',seconds:30});read(test);test.page.setConn('ws',false);
assert.ok(test.page.store.data);
// Timer survives reconnection; a successful read cancels the old-data expiry.
test=scenario({privacy:'counter'}, {mode:'delay',seconds:30});read(test);test.page.setConn('ws',false);
let expiry=[...test.timers.values()].find(t=>t.ms>0&&t.ms<=30000);
assert.ok(expiry);test.page.setConn('ws',true);expiry.fn();assert.equal(test.page.store.data,null);
read(test);test.page.setConn('ws',false);assert.ok(test.timers.size);read(test);
assert.equal(test.timers.size,0);
// Settings saved in a second tab applies immediately; no card data is stored.
test=scenario({privacy:'counter'}, {mode:'keep',seconds:30});read(test);test.page.setConn('ws',false);
test.storage.set('smc.stalePolicy',JSON.stringify({mode:'clear',seconds:30}));
test.listeners.storage({key:'smc.stalePolicy'});assert.equal(test.page.store.data,null);
assert.equal(test.page.store.readAt,null);
// Invalid storage falls back safely to the preset.
test=scenario({privacy:'kiosk'}, {mode:'invalid',seconds:-1});
assert.equal(test.page.stalePolicy().mode,'clear');
// Copy cancellation never writes old data to the clipboard.
(async()=>{
 test=scenario({privacy:'counter'}, {mode:'keep',seconds:30});read(test);test.page.setConn('ws',false);
 test.window.isSecureContext=true;test.window.confirm=()=>false;
 await assert.rejects(test.page.copyText('synthetic'),e=>e.cancelled===true);
 assert.equal(test.clipboard.length,0);
 test.window.confirm=()=>true;await test.page.copyText('synthetic');
 assert.deepEqual(test.clipboard,['synthetic']);
 console.log('stale-card policy, multi-transport, reconnection, timers and copy checks passed');
})();
