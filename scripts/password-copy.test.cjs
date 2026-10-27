const test = require('node:test'), assert = require('node:assert/strict'), fs = require('node:fs'), vm = require('node:vm');
function setup(writeText = async () => {}) {
  const notifications = [];
  class Element {
    constructor(tag) { this.tag = tag; this.children = []; this.attributes = {}; this.handlers = {}; this.value = ''; this.type = 'password'; this.className = ''; this.classList = { contains: name => this.className.split(' ').includes(name) }; }
    setAttribute(name, value) { this.attributes[name] = value; }
    addEventListener(name, callback) { this.handlers[name] = callback; }
    append(...items) { for (const item of items) { if (item.parentElement) item.parentElement.children = item.parentElement.children.filter(child => child !== item); item.parentElement = this; this.children.push(item); } }
    insertAdjacentElement(position, item) { assert.equal(position, 'beforebegin'); const parent = this.parentElement; parent.children.splice(parent.children.indexOf(this), 0, item); item.parentElement = parent; }
    focus() { this.focused = true; }
    select() { this.selected = true; }
  }
  const window = { LiteCP: { showToast: (...message) => notifications.push(message) } };
  vm.runInNewContext(fs.readFileSync('web/password-copy.js', 'utf8'), {window, document: { createElement: tag => new Element(tag) }, navigator: {clipboard: {writeText}}});
  const input = new Element('input'), parent = new Element('label'); parent.append(input);
  return {helper: window.LiteCPPasswordCopy, input, parent, notifications};
}
const click = button => button.handlers.click({preventDefault() {}});
test('copy icon reads the current generated password without showing or submitting it', async () => {
  const copied = [], ui = setup(async value => copied.push(value));
  ui.helper.enhance(ui.input);
  const wrapper = ui.parent.children[0], button = wrapper.children[1];
  assert.equal(wrapper.className, 'password-copy-field'); assert.equal(button.type, 'button'); assert.equal(button.attributes['aria-label'], 'Copy password');
  ui.input.value = 'First-generated-secret'; await click(button);
  ui.input.value = 'Next-generated-secret'; await click(button);
  assert.deepEqual(copied, ['First-generated-secret', 'Next-generated-secret']);
  assert.equal(ui.input.type, 'password'); assert.deepEqual(Object.keys(button.attributes), ['aria-label']);
  assert.ok(ui.notifications.every(message => !message.join(' ').includes('secret')));
  ui.helper.enhance(ui.input); assert.equal(ui.parent.children.length, 1); assert.equal(wrapper.children.length, 2);
});
test('empty values do not write to the clipboard and rejection offers manual copy', async () => {
  let writes = 0; const ui = setup(async () => { writes++; throw Error('Clipboard rejected'); });
  ui.helper.enhance(ui.input); const button = ui.parent.children[0].children[1];
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
  assert.match(app, /\/password\/i\.test\(rows\[index\]\[0\]\)/);
  assert.ok(index.indexOf('password-copy.js') < index.indexOf('src="app.js'));
});
