(() => {
  "use strict";
  const cp = window.LiteCP;
  const section = document.querySelector('[data-settings-panel="hostname"]');
  const card = document.createElement("article");
  card.className = "panel settings-card";
  card.innerHTML = `
    <div class="panel-head"><div><h2>Hostname SSL</h2><p>Install a trusted Let's Encrypt certificate for the LiteCP panel.</p></div></div>
    <div class="settings-body">
      <p>Save a public hostname, point its DNS records to this server, and allow inbound HTTP on port 80.</p>
      <p role="status" data-hostname-ssl-status>Checking certificate…</p>
      <p><a data-hostname-ssl-url hidden>Open secure panel</a></p>
      <div class="form-actions">
        <button type="button" class="button button-primary" data-hostname-ssl-install disabled>Install hostname SSL</button>
        <button type="button" class="button button-secondary" data-hostname-ssl-refresh>Refresh status</button>
      </div>
    </div>`;
  section.append(card);

  const status = card.querySelector("[data-hostname-ssl-status]");
  const link = card.querySelector("[data-hostname-ssl-url]");
  const install = card.querySelector("[data-hostname-ssl-install]");
  const refresh = card.querySelector("[data-hostname-ssl-refresh]");
  const endpoint = "/api/system/hostname/ssl";
  let current = null;
  let loading = false;
  let installing = false;
  let checkedHostname = "";

  async function load() {
    if (loading || installing) return;
    loading = true;
    const hostname = cp.state()?.system?.hostname;
    install.disabled = refresh.disabled = true;
    link.hidden = true;
    status.textContent = "Checking certificate…";
    try {
      current = await cp.api(endpoint);
      checkedHostname = current.hostname;
      if (!current.validHostname) {
        status.textContent = "Save a public server hostname to install SSL.";
      } else if (current.trusted) {
        status.textContent = `Trusted certificate for ${current.hostname}. Expires ${cp.formatDateTime(current.expiresAt)}.`;
        link.href = current.url;
        link.hidden = false;
      } else {
        status.textContent = `No trusted panel certificate found for ${current.hostname}.`;
      }
    } catch (error) {
      current = null;
      status.textContent = error.message;
    } finally {
      loading = false;
      refresh.disabled = false;
      install.disabled = !current?.validHostname;
      if (hostname !== cp.state()?.system?.hostname) load();
    }
  }

  install.addEventListener("click", async () => {
    if (installing || loading || !current?.validHostname) return;
    const hostname = current.hostname;
    if (!await window.LiteCPDialogs.confirm(`Request and install a trusted panel certificate for ${hostname}?`, {title:"Install hostname SSL", confirmText:"Install SSL"})) return;
    if (installing || loading || hostname !== current?.hostname) return;
    installing = true;
    install.disabled = refresh.disabled = true;
    status.textContent = `Requesting and verifying SSL for ${hostname}…`;
    try {
      await cp.api(endpoint, {method:"POST", body:{hostname}});
      installing = false;
      await load();
      cp.showToast("Hostname SSL installed", `The panel is serving a trusted certificate for ${hostname}.`);
    } catch (error) {
      status.textContent = error.message;
    } finally {
      installing = false;
      install.disabled = !current?.validHostname;
      refresh.disabled = false;
    }
  });

  refresh.addEventListener("click", load);
  function activate() {
    if (location.hash === "#settings/hostname" && cp.state()?.system?.hostname !== checkedHostname) load();
  }
  const previousRender = window.LiteCPFeatures.render;
  window.LiteCPFeatures.render = state => { previousRender(state); activate(); };
  const previousNavigate = window.LiteCPFeatures.navigate;
  window.LiteCPFeatures.navigate = page => { previousNavigate?.(page); activate(); };
  window.addEventListener("hashchange", activate);
  activate();
})();
