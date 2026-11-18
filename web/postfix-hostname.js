(() => {
  "use strict";
  const cp = window.LiteCP;
  const section = document.querySelector('[data-settings-panel="hostname"]');
  if (!section) return;
  const card = document.createElement("article");
  card.id = "postfixHostnamePanel";
  card.className = "panel settings-card";
  card.innerHTML = `<div class="panel-head"><div><h2>Postfix hostname</h2><p>Manage the hostname Postfix uses for email. Update public DNS and reverse DNS separately.</p></div></div>
    <form class="settings-body">
      <label class="field"><span>Mail hostname</span><input name="hostname" required maxlength="254" placeholder="mail.example.com" autocomplete="off"></label>
      <p role="status"></p>
      <div class="form-actions">
        <button type="submit" class="button button-primary" disabled>Save hostname</button>
        <button type="button" class="button button-secondary">Refresh</button>
      </div>
    </form>`;
  section.append(card);
  const form = card.querySelector("form");
  const input = form.elements.hostname;
  const status = form.querySelector('[role="status"]');
  const save = form.querySelector('[type="submit"]');
  const refresh = form.querySelector('[type="button"]');
  const endpoint = "/api/system/hostname/postfix";
  let current = "", busy = false, active = false;

  async function request(change) {
    if (busy) return;
    busy = input.disabled = save.disabled = refresh.disabled = true;
    status.textContent = change ? "Saving Postfix hostname…" : "Reading Postfix hostname…";
    try {
      const data = await cp.api(endpoint, change
        ? {method: "PUT", body: {current, hostname: input.value.trim()}}
        : undefined);
      current = input.value = data.hostname;
      status.textContent = change
        ? `Saved. Confirm that your mail certificate covers ${current}.`
        : `Current Postfix hostname: ${current}`;
      if (change) section.querySelector("[data-hostname-ssl-refresh]")?.click();
    } catch (error) {
      status.textContent = error.message;
    } finally {
      busy = input.disabled = refresh.disabled = false;
      save.disabled = !current;
    }
  }
  form.addEventListener("submit", event => {
    event.preventDefault();
    if (current && form.reportValidity()) request(true);
  });
  refresh.addEventListener("click", () => request(false));
  function activate() {
    const next = location.hash === "#settings/hostname" && !!cp.state();
    if (next && !active) request(false);
    active = next;
  }
  const navigate = window.LiteCPFeatures.navigate;
  window.LiteCPFeatures.navigate = route => { navigate?.(route); activate(); };
  const render = window.LiteCPFeatures.render;
  window.LiteCPFeatures.render = state => { render?.(state); activate(); };
  window.addEventListener("hashchange", activate);
  activate();
})();
