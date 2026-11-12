const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");

function fixture(settings = {}) {
  class Element {
    constructor() {
      this.children = [];
      this.nodes = new Map();
      this.handlers = {};
      this.hidden = false;
      this.classList = {contains: () => false};
    }
    set innerHTML(value) {
      this.html = value;
      this.inputs = [...value.matchAll(/<input\b([^>]+)>/g)].map(match => {
        const attributes = match[1];
        return {
          name: attributes.match(/name="([^"]+)"/)?.[1],
          type: attributes.match(/type="([^"]+)"/)?.[1],
          value: attributes.match(/value="([^"]*)"/)?.[1] || "",
          checked: /\bchecked\b/.test(attributes),
          focus() {}
        };
      });
    }
    get innerHTML() { return this.html; }
    querySelector(selector) {
      if (selector === "input") return this.inputs[0];
      if (!this.nodes.has(selector)) this.nodes.set(selector, new Element());
      return this.nodes.get(selector);
    }
    querySelectorAll(selector) {
      if (selector === ".alerts-recipient") return this.querySelector("[data-alert-recipients]").children;
      if (selector.startsWith("input[name")) return this.inputs.filter(input => input.name.startsWith("event-"));
      return [];
    }
    append(child) { child.parent = this; this.children.push(child); }
    addEventListener(name, callback) { this.handlers[name] = callback; }
    reportValidity() { return true; }
  }
  const panel = new Element();
  const form = panel.querySelector("form");
  const requests = [];
  const response = {
    settings: {emailEnabled: true, emailTo: "old@example.test", emailFrom: "alerts@example.test", ...settings},
    categories: [
      {id: "backup", label: "Backup failed", description: "Backup creation and upload failures."},
      {id: "resource", label: "Resource limit reached", description: "Website resource violations."}
    ]
  };
  class FormData {
    constructor(target) {
      this.inputs = [...target.inputs, ...target.querySelector("[data-alert-recipients]").children.flatMap(row => row.inputs)];
    }
    getAll(name) { return this.inputs.filter(input => input.name === name).map(input => input.value); }
    get(name) { return this.getAll(name)[0]; }
    has(name) { return this.inputs.some(input => input.name === name && input.checked); }
  }
  const state = {accessRole: ""};
  const window = {
    LiteCP: {
      state: () => state,
      escapeHTML: String,
      api: async (path, options) => { requests.push({path, options}); return response; }
    },
    LiteCPFeatures: {navigate() {}},
    addEventListener() {}
  };
  vm.runInNewContext(fs.readFileSync("web/alerts-settings.js", "utf8"), {
    document: {createElement: tag => tag === "article" ? panel : new Element(), querySelector: () => new Element()},
    window, location: {hash: "#settings/alerts"}, MutationObserver: class {observe() {}},
    FormData, Set
  });
  const input = name => form.inputs.find(value => value.name === name);
  const click = attribute => form.handlers.click({target: {closest: selector => selector === attribute ? {} : null}});
  return {panel, form, requests, input, click, settle: () => new Promise(setImmediate), submit: () => form.handlers.submit({preventDefault() {}})};
}

test("Alerts loads automatically and carries a legacy recipient into the new form", async () => {
  const ui = fixture();
  await ui.settle();
  assert.equal(ui.requests[0].path, "/api/operations?alerts=1");
  assert.equal(ui.form.hidden, false);
  assert.equal(ui.form.querySelector("[data-alert-recipients]").children[0].inputs[0].value, "old@example.test");
  assert.equal(ui.input("event-backup").checked, true);
});

test("Alerts saves multiple recipient addresses and an explicit disabled event", async () => {
  const ui = fixture({emailRecipients: ["first@example.test"], alertRules: {resource: false}});
  await ui.settle();
  ui.click("[data-alert-add]");
  ui.form.querySelector("[data-alert-recipients]").children[1].inputs[0].value = "second@example.test";
  ui.input("event-backup").checked = false;
  ui.submit();
  await ui.settle();
  const body = JSON.parse(JSON.stringify(ui.requests[1].options.body));
  assert.equal(body.action, "alert-settings");
  assert.deepEqual(body.settings.emailRecipients, ["first@example.test", "second@example.test"]);
  assert.deepEqual(body.settings.alertRules, {backup: false, resource: false});
  assert.equal(body.settings.emailEnabled, true);
  assert.match(ui.panel.querySelector("[data-alert-status]").textContent, /saved/);
});

test("Alerts prevents email delivery with missing addresses and permits disabling delivery", async () => {
  const ui = fixture({emailTo: "", emailFrom: ""});
  await ui.settle();
  ui.submit();
  await ui.settle();
  assert.equal(ui.requests.length, 1);
  assert.match(ui.panel.querySelector("[data-alert-status]").textContent, /sender and at least one/);
  ui.input("emailEnabled").checked = false;
  ui.submit();
  await ui.settle();
  assert.equal(ui.requests[1].options.body.settings.emailEnabled, false);
});

test("Test email uses draft recipients without saving notification rules", async () => {
  const ui = fixture();
  await ui.settle();
  ui.click("[data-alert-test]");
  await ui.settle();
  assert.equal(ui.requests[1].options.body.action, "test-email");
  assert.deepEqual(Array.from(ui.requests[1].options.body.settings.emailRecipients), ["old@example.test"]);
  assert.match(ui.panel.querySelector("[data-alert-status]").textContent, /accepted by the mail service/);
});
