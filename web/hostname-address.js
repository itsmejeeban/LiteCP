(() => {
  "use strict";
  const cp = window.LiteCP;
  const card = document.createElement("article");
  card.id = "hostnameAddressPanel";
  card.className = "panel settings-card";
  card.innerHTML = `<div class="panel-head"><div><h2>Hostname address</h2><p>Set the hostname IP used by this server. Public DNS is managed at your DNS provider.</p></div></div>
    <form class="settings-body">
      <label class="field"><span>Saved hostname</span><input data-address-hostname readonly></label>
      <label class="field"><span>IP address</span><input name="ip" required maxlength="45" placeholder="IPv4 or IPv6 address" autocomplete="off"></label>
      <p data-address-status role="status" aria-live="polite"></p>
      <div class="form-actions"><button class="button button-primary" type="submit" disabled>Save address</button><button class="button button-secondary" type="button" data-address-refresh>Refresh</button></div>
    </form>`;
  document.querySelector('[data-settings-panel="hostname"]').append(card);
  const form = card.querySelector("form");
  const hostname = card.querySelector("[data-address-hostname]");
  const ip = form.elements.ip;
  const status = card.querySelector("[data-address-status]");
  const save = form.querySelector('[type="submit"]');
  const refresh = card.querySelector("[data-address-refresh]");
  let busy = false, dirty = false, loadedHostname = "";
  const endpoint = "/api/system/hostname/address";
  function setBusy(value) {
    busy = value;
    ip.disabled = refresh.disabled = value;
    save.disabled = value || !loadedHostname;
  }
  function render(data) {
    hostname.value = data.hostname;
    ip.value = data.addresses[0] || "";
    loadedHostname = data.hostname;
    dirty = false;
    status.textContent = data.addresses.length ? "Current local mapping: " + data.addresses.join(", ") : "No local hostname mapping is configured.";
  }
  async function load() {
    if (busy) return;
    setBusy(true);
    try {
      render(await cp.api(endpoint));
    } catch (error) {
      status.textContent = error.message;
    } finally {
      setBusy(false);
    }
  }
  ip.addEventListener("input", () => { dirty = true; });
  refresh.addEventListener("click", async () => {
    if (dirty && !await window.LiteCPDialogs.confirm("Discard the unsaved hostname address?")) return;
    await load();
  });
  form.addEventListener("submit", async event => {
    event.preventDefault();
    if (busy || !loadedHostname || !form.reportValidity()) return;
    setBusy(true);
    status.textContent = "Saving local hostname address…";
    try {
      render(await cp.api(endpoint, {method: "PUT", body: {hostname: loadedHostname, ip: ip.value.trim()}}));
      status.textContent = "Local hostname address saved.";
      cp.showToast("Hostname address saved", "The server-local hostname mapping was updated.");
    } catch (error) {
      status.textContent = error.message;
    } finally {
      setBusy(false);
    }
  });
  function activate() {
    if (location.hash === "#settings/hostname" && !dirty && cp.state()?.system?.hostname !== loadedHostname) load();
  }
  const previousRender = window.LiteCPFeatures.render;
  window.LiteCPFeatures.render = state => { previousRender?.(state); activate(); };
  const previousNavigate = window.LiteCPFeatures.navigate;
  window.LiteCPFeatures.navigate = route => { previousNavigate?.(route); activate(); };
  window.addEventListener("hashchange", activate);
  activate();
})();
