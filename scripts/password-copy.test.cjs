const test = require('node:test'), assert = require('node:assert/strict'), fs = require('node:fs'), vm = require('node:vm');
function setup(writeText = async () => {}) {
  const notifications = [];
  class Element {
    constructor(tag) { this.tag = tag; this.children = []; this.attributes = {}; this.handlers = {}; this.dataset={};this.events=[];this.value = ''; this.type = 'password'; this.className = ''; this.classList = { add: name => {this.className+=' '+name;},contains: name => this.className.split(' ').includes(name) }; }
    setAttribute(name, value) { this.attributes[name] = value; }
    addEventListener(name, callback) { this.handlers[name] = callback; }
    append(...items) { for (const item of items) { if (item.parentElement) item.parentElement.children = item.parentElement.children.filter(child => child !== item); item.parentElement = this; this.children.push(item); } }
    prepend(item){item.parentElement=this;this.children.unshift(item);}
    insertAdjacentElement(position, item) { assert.ok(['beforebegin','afterend'].includes(position)); const parent = this.parentElement; parent.children.splice(parent.children.indexOf(this)+(position==='afterend'?1:0), 0, item); item.parentElement = parent; }
    dispatchEvent(event){this.events.push(event.type);}
    closest(selector){return this.parentElement;}
    focus() { this.focused = true; }
    select() { this.selected = true; }
  }
  const window = { LiteCP: { showToast: (...message) => notifications.push(message) } };
  const document={ createElement: tag => new Element(tag) };
  vm.runInNewContext(fs.readFileSync('web/password-copy.js', 'utf8'), {window, document, navigator: {clipboard: {writeText}}});
  const input = new Element('input'), parent = new Element('label'); parent.append(input);
  return {helper: window.LiteCPPasswordCopy, input, parent, notifications,document};
}
const click = button => button.handlers.click({preventDefault() {}});
test('copy icon reads the current generated password without showing or submitting it', async () => {
  const copied = [], ui = setup(async value => copied.push(value));
  ui.helper.enhance(ui.input);
  const wrapper = ui.parent.children[0], button = wrapper.children[2];
  assert.equal(wrapper.className, 'password-copy-field'); assert.equal(button.type, 'button'); assert.equal(button.attributes['aria-label'], 'Copy password');
  ui.input.value = 'First-generated-secret'; await click(button);
  ui.input.value = 'Next-generated-secret'; await click(button);
  assert.deepEqual(copied, ['First-generated-secret', 'Next-generated-secret']);
  assert.equal(ui.input.type, 'password'); assert.deepEqual(Object.keys(button.attributes), ['aria-label']);
  assert.ok(ui.notifications.every(message => !message.join(' ').includes('secret')));
  ui.helper.enhance(ui.input); assert.equal(ui.parent.children.length, 1); assert.equal(wrapper.children.length, 3);
});
test('empty values do not write to the clipboard and rejection offers manual copy', async () => {
  let writes = 0; const ui = setup(async () => { writes++; throw Error('Clipboard rejected'); });
  ui.helper.enhance(ui.input); const button = ui.parent.children[0].children[2];
  await click(button); assert.equal(writes, 0);
  ui.input.value = 'Generated-private-value'; await click(button);
  assert.equal(writes, 1); assert.equal(ui.input.focused, true); assert.equal(ui.input.selected, true); assert.equal(ui.input.type, 'password');
  assert.equal(ui.notifications.at(-1)[0], 'Copy unavailable');
});
test('one-time credential copy reads only its password and does not depend on input visibility', async () => {
  const copied = [], ui = setup(async value => copied.push(value));
  const button = ui.helper.button(() => 'One-time-database-password'); await click(button);
  assert.deepEqual(copied, ['One-time-database-password']);
});
test('all generated password entry points use the shared copy control', () => {
  const features = fs.readFileSync('web/features.js','utf8'), ftp = fs.readFileSync('web/website-ftp.js','utf8'), app = fs.readFileSync('web/app.js','utf8'), index = fs.readFileSync('web/index.html','utf8');
  assert.match(features, /window\.LiteCPPasswordCopy\.enhance\(input\)/);
  assert.match(ftp, /LiteCPPasswordCopy\.enhance\(dialog\.querySelector\('input\[name="password"\]'\)\)/);
  assert.match(app, /\/password\|secret\/i\.test\(rows\[index\]\[0\]\)/);assert.match(app,/input\.readOnly=true/);assert.match(app,/LiteCPPasswordCopy\.enhance\(input\)/);
  assert.ok(index.indexOf('password-copy.js') < index.indexOf('src="app.js'));
});
test('eye icon appears before copy and reveals/hides without changing or submitting the password',async()=>{
  const copied=[],ui=setup(async value=>copied.push(value));ui.input.value='Existing-private-value';ui.helper.enhance(ui.input);
  const wrapper=ui.parent.children[0],eye=wrapper.children[1],copy=wrapper.children[2];
  assert.equal(eye.type,'button');assert.equal(eye.attributes['aria-label'],'Show password');assert.equal(eye.attributes['aria-pressed'],'false');assert.equal(eye.textContent,undefined);
  await click(eye);assert.equal(ui.input.type,'text');assert.equal(eye.attributes['aria-label'],'Hide password');assert.equal(eye.attributes['aria-pressed'],'true');
  await click(copy);assert.deepEqual(copied,['Existing-private-value']);
  await click(eye);assert.equal(ui.input.type,'password');assert.equal(ui.input.value,'Existing-private-value');assert.deepEqual(ui.input.events,[]);
});
test('current/confirmation password fields get an eye without a copy or generator control',()=>{
  const ui=setup();ui.helper.enhance(ui.input,{copy:false});const wrapper=ui.parent.children[0];
  assert.ok(wrapper.classList.contains('password-eye-only'));assert.equal(wrapper.children.length,2);assert.equal(wrapper.children[1].attributes['aria-label'],'Show password');
});
test('Generate is a secondary button and still fills password confirmation',async()=>{
  const ui=setup();ui.input.name='next';ui.input.form={elements:{confirm:{value:''}}};
  const source=fs.readFileSync('web/features.js','utf8'),start=source.indexOf('  function enhancePasswords('),end=source.indexOf('  function askPassword(',start);
  vm.runInNewContext(source.slice(start,end)+'\nenhancePasswords();',{document:ui.document,window:{LiteCPPasswordCopy:ui.helper},$$:()=>[ui.input],generatePassword:()=> 'Generated-fixture-password',Event:class{constructor(type){this.type=type;}}});
  const wrapper=ui.parent.children[0],actions=wrapper.children.at(-1),generate=actions.children[0];assert.equal(generate.tag,'button');assert.equal(generate.type,'button');assert.equal(generate.className,'button button-secondary');assert.equal(generate.textContent,'Generate');
  await click(generate);assert.equal(ui.input.value,'Generated-fixture-password');assert.equal(ui.input.form.elements.confirm.value,ui.input.value);assert.deepEqual(ui.input.events,['input']);
  assert.equal(actions.children.length,1);
});
