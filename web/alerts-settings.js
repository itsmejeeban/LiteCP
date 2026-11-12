(() => {
  "use strict";
  const cp = window.LiteCP;
  const esc = cp.escapeHTML;
  const app = document.querySelector("#appView");
  const panel = document.createElement("article");
  panel.id = "alertsSettingsPanel";
  panel.className = "panel settings-card";
  panel.innerHTML = `<div class="panel-head"><div><h2>Administrator alerts</h2><p>Choose which failures send an email to your administrators.</p></div><button type="button" class="button button-secondary" data-alert-refresh>Refresh</button></div>
    <div class="settings-body"><p data-alert-status role="status" aria-live="polite"></p><form data-alert-settings hidden></form></div>`;
  document.querySelector('[data-page-view="settings"] .settings-grid').append(panel);
  const form = panel.querySelector("form");
  const status = panel.querySelector("[data-alert-status]");
  let busy = false;
  let dirty = false;
  let wasVisible = false;

  function switchField(name, label, description, checked) {
    return `<label class="alerts-rule"><span class="alerts-rule-copy"><span>${esc(label)}</span>${description ? `<small>${esc(description)}</small>` : ""}</span><input type="checkbox" role="switch" name="${esc(name)}" ${checked ? "checked" : ""}><span class="alerts-switch" aria-hidden="true"></span></label>`;
  }
  function recipientField(value = "") {
    const row = document.createElement("div");
    row.className = "alerts-recipient";
    row.innerHTML = `<input type="email" name="recipient" aria-label="Alert recipient email" autocomplete="email" placeholder="admin@example.com" maxlength="254" value="${esc(value)}"><button type="button" class="button button-secondary" data-alert-remove>Remove</button>`;
    return row;
  }
  function recipientControls() {
    const rows = form.querySelectorAll(".alerts-recipient");
    rows.forEach(row => { row.querySelector("[data-alert-remove]").disabled = rows.length === 1; });
    form.querySelector("[data-alert-add]").disabled = rows.length >= 20;
  }
  function setBusy(value) {
    busy = value;
    panel.querySelectorAll("button, input").forEach(control => { control.disabled = value; });
    if (!value && !form.hidden) recipientControls();
  }
  function render(data) {
    const settings = data.settings;
    form.innerHTML = `<div class="alerts-delivery">
      ${switchField("emailEnabled", "Email notifications", "", settings.emailEnabled)}
      <div class="alerts-addresses"><fieldset><legend>Recipient email addresses</legend><div data-alert-recipients></div><button type="button" class="button button-secondary" data-alert-add>Add email</button><small>Up to 20 email addresses.</small></fieldset>
      <label class="field"><span>Sender email address</span><input type="email" name="emailFrom" value="${esc(settings.emailFrom || "")}" placeholder="alerts@example.com" maxlength="254"><small>Use an address on a mail domain configured in LiteCP.</small></label></div></div>
      <fieldset class="alerts-events"><legend>Notify me about</legend><div class="alerts-event-list">${data.categories.map(category => switchField("event-" + category.id, category.label, category.description, settings.alertRules?.[category.id] !== false)).join("")}</div></fieldset>
      <div class="alerts-actions"><button type="submit" class="button button-primary">Save alerts</button><button type="button" class="button button-secondary" data-alert-test>Send test email</button><a href="#operations" class="button button-secondary">View operations</a></div>`;
    const recipients = settings.emailRecipients?.length ? settings.emailRecipients : [settings.emailTo || ""];
    recipients.forEach(value => form.querySelector("[data-alert-recipients]").append(recipientField(value)));
    form.hidden = false;
    recipientControls();
  }
  function readSettings(forTest = false) {
    const fields = new FormData(form);
    const recipients = [...new Set(fields.getAll("recipient").map(value => String(value).trim()).filter(Boolean))];
    const emailFrom = String(fields.get("emailFrom") || "").trim();
    const emailEnabled = fields.has("emailEnabled");
    if ((emailEnabled || forTest) && (!recipients.length || !emailFrom)) {
      throw new Error("Enter a sender and at least one recipient email address.");
    }
    const alertRules = {};
    form.querySelectorAll('input[name^="event-"]').forEach(input => {
      alertRules[input.name.slice(6)] = input.checked;
    });
    return {emailEnabled, emailRecipients: recipients, emailFrom, alertRules};
  }
  async function load() {
    if (busy) return;
    if (dirty) { status.textContent = "Save your changes before refreshing."; return; }
    setBusy(true);
    status.textContent = "Loading alert settings…";
    try {
      const data = await cp.api("/api/operations?alerts=1");
      render(data);
      status.textContent = "";
    } catch (error) {
      status.textContent = error.message;
    } finally {
      setBusy(false);
    }
  }
  async function save(test = false) {
    if (busy || !form.reportValidity()) return;
    let settings;
    try { settings = readSettings(test); }
    catch (error) { status.textContent = error.message; return; }
    setBusy(true);
    status.textContent = test ? "Sending test email…" : "Saving alerts…";
    try {
      await cp.api("/api/operations", {method: "POST", body: {action: test ? "test-email" : "alert-settings", settings}});
      if (!test) dirty = false;
      status.textContent = test ? "Test email accepted by the mail service. Check the recipient inboxes." : "Alert settings saved.";
    } catch (error) {
      status.textContent = error.message;
    } finally {
      setBusy(false);
    }
  }
  form.addEventListener("input", () => { dirty = true; });
  form.addEventListener("submit", event => { event.preventDefault(); save(); });
  panel.querySelector("[data-alert-refresh]").addEventListener("click", load);
  form.addEventListener("click", event => {
    if (event.target.closest("[data-alert-add]")) {
      const row = recipientField();
      form.querySelector("[data-alert-recipients]").append(row);
      dirty = true;
      recipientControls();
      row.querySelector("input").focus();
    }
    if (event.target.closest("[data-alert-remove]")) {
      event.target.closest(".alerts-recipient").remove();
      dirty = true;
      recipientControls();
    }
    if (event.target.closest("[data-alert-test]")) save(true);
  });
  function activate() {
    const visible = location.hash === "#settings/alerts" && !app.classList.contains("hidden") && !cp.state()?.accessRole;
    if (visible && !wasVisible && !dirty) load();
    wasVisible = visible;
  }
  const navigate = window.LiteCPFeatures.navigate;
  window.LiteCPFeatures.navigate = id => { navigate?.(id); activate(); };
  window.addEventListener("hashchange", activate);
  new MutationObserver(activate).observe(app, {attributes: true, attributeFilter: ["class"]});
  activate();
})();
