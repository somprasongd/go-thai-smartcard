const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const html = fs.readFileSync(`${__dirname}/index.html`, 'utf8');
let script = html.match(/<script>\s*([\s\S]*?)<\/script>/)[1];
// Expose the closure's functions without changing the production page.
script = script.replace(/\}\)\(\);\s*$/, 'globalThis.page = {store, boot, cidValid, clearData, handleEvent}; })();');
function scenario(prefs) {
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
    localStorage:{getItem:()=>prefs ? JSON.stringify(prefs) : null,setItem(){}},
    window:{location:{protocol:'http:',host:'localhost',search:''},fetch:()=>new Promise(()=>{})},
    fetch:()=>new Promise(()=>{}), navigator:{}, setTimeout(){},clearTimeout(){},
    URLSearchParams, WebSocket:class {}, console
  };
  vm.runInNewContext(script,context);
  return {page:context.page,element};
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
