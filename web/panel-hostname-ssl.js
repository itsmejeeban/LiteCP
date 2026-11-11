(() => {
  "use strict";
  const cp = window.LiteCP;
  const section = document.querySelector('[data-settings-panel="hostname"]');
  const card = document.createElement("article");
  card.className = "panel settings-card";
  card.innerHTML = `
    <div class="panel-head"><div><h2>Hostname SSL</h2><p>Manage trusted certificates for the panel, FTPS and mail services.</p></div></div>
    <div class="settings-body">
      <p>Save a public hostname, point its DNS records to this server, and allow inbound HTTP on port 80.</p>
      <p role="status" data-hostname-ssl-status>Checking certificate…</p>
      <p><a data-hostname-ssl-url hidden>Open secure panel</a></p>
      <div class="form-actions">
        <button type="button" class="button button-primary" data-hostname-ssl-install disabled>Install hostname SSL</button>
        <button type="button" class="button button-primary" data-hostname-ssl-install-all disabled>Install All Services SSL</button>
        <button type="button" class="button button-secondary" data-hostname-ssl-refresh>Refresh status</button>
      </div>
      <p role="status" data-hostname-ssl-result hidden></p>
      <div class="table-scroll" data-hostname-ssl-services hidden>
        <table class="data-table" aria-label="Service SSL status">
          <thead><tr><th scope="col">Service</th><th scope="col">Endpoint</th><th scope="col">SSL status</th><th scope="col">Expires</th></tr></thead>
          <tbody data-hostname-ssl-service-rows></tbody>
        </table>
      </div>
    </div>`;
  section.append(card);

  const status = card.querySelector("[data-hostname-ssl-status]");
  const link = card.querySelector("[data-hostname-ssl-url]");
  const install = card.querySelector("[data-hostname-ssl-install]");
  const installAll = card.querySelector("[data-hostname-ssl-install-all]");
  const refresh = card.querySelector("[data-hostname-ssl-refresh]");
  const result = card.querySelector("[data-hostname-ssl-result]");
  const serviceTable = card.querySelector("[data-hostname-ssl-services]");
  const serviceRows = card.querySelector("[data-hostname-ssl-service-rows]");
  const endpoint = "/api/system/hostname/ssl";
  let current = null;
  let loading = false;
  let installing = false;
  let checkedHostname = "";

  function setButtons() {
    const busy = loading || installing;
    refresh.disabled = busy;
    install.disabled = installAll.disabled = busy || !current?.validHostname;
  }

  function renderServices(services) {
    const labels = {trusted: "Trusted", untrusted: "Needs a trusted certificate", unavailable: "Unavailable", unknown: "Not checked"};
    const escape = value => cp.escapeHTML(String(value ?? ""));
    serviceRows.innerHTML = services.map(service => {
      const label = labels[service.status] || labels.unknown;
      const expires = service.expiresAt ? cp.formatDateTime(service.expiresAt) : "—";
      const detail = service.detail ? `<br><small>${escape(service.detail)}</small>` : "";
      return `<tr><th scope="row">${escape(service.name)}</th><td>${escape(service.endpoint)}</td><td><span class="badge${service.status === "trusted" ? " success" : ""}">${label}</span>${detail}</td><td>${escape(expires)}</td></tr>`;
    }).join("");
    serviceTable.hidden = !services.length;
  }

  async function load() {
    if (loading || installing) return;
    loading = true;
    const hostname = cp.state()?.system?.hostname;
    setButtons();
    status.textContent = "Checking certificate…";
    try {
      current = await cp.api(endpoint);
      checkedHostname = current.hostname;
      link.hidden = true;
      renderServices(current.services || []);
      if (!current.validHostname) {
        status.textContent = "Save a public server hostname to install SSL.";
      } else if (current.trusted) {
        status.textContent = `SSL installed and active for ${current.hostname}. Expires ${cp.formatDateTime(current.expiresAt)}.`;
        link.href = current.url;
        link.hidden = false;
      } else if (current.configured) {
        status.textContent = `SSL installed for ${current.hostname}. The local HTTPS listener could not be verified.`;
        link.href = current.url;
        link.hidden = false;
      } else {
        status.textContent = `No trusted panel certificate found for ${current.hostname}.`;
      }
      return true;
    } catch (error) {
      if (current?.hostname !== hostname) { current = null; link.hidden = true; }
      renderServices([]);
      status.textContent = current?.trusted
        ? `SSL was active for ${current.hostname} at the last check. Status refresh failed.`
        : "Could not check hostname SSL status. Use Refresh status to try again.";
    } finally {
      loading = false;
      setButtons();
      if (hostname !== cp.state()?.system?.hostname) load();
    }
  }

  async function installCertificate(allServices) {
    if (installing || loading || !current?.validHostname) return;
    const hostname = current.hostname;
    const title = allServices ? "Install All Services SSL" : "Install hostname SSL";
    const prompt = allServices
      ? `Check certificates for ${hostname} and install trusted SSL on supported services that need it? Updating a service can briefly interrupt new connections.`
      : `Request and install a trusted panel certificate for ${hostname}?`;
    if (!await window.LiteCPDialogs.confirm(prompt, {title, confirmText: "Install SSL"})) return;
    if (installing || loading || hostname !== current?.hostname) return;
    installing = true;
    setButtons();
    result.hidden = true;
    status.textContent = allServices ? `Checking and installing service certificates for ${hostname}…` : `Requesting and verifying SSL for ${hostname}…`;
    try {
      const body = allServices ? {hostname, allServices: true} : {hostname};
      const response = await cp.api(endpoint, {method: "POST", body});
      installing = false;
      const verified = await load();
      const warnings = response?.warnings || [];
      if (warnings.length) {
        result.textContent = warnings.join(" ");
        result.hidden = false;
      }
      if (allServices) {
        const services = current?.services || [];
        if (verified && !warnings.length && services.some(service => service.status === "trusted")
            && services.every(service => service.status === "trusted" || service.status === "unavailable")) {
          cp.showToast("Service SSL verified", `Available services are serving trusted certificates for ${hostname}.`);
        } else if (verified && !warnings.length) {
          result.textContent = "Some certificates could not be verified. Review the service statuses below.";
          result.hidden = false;
        }
      } else if (verified && current?.trusted) {
        cp.showToast("Hostname SSL installed", `The panel is serving a trusted certificate for ${hostname}.`);
      }
    } catch (error) {
      status.textContent = error.message;
    } finally {
      installing = false;
      setButtons();
    }
  }

  install.addEventListener("click", () => installCertificate(false));
  installAll.addEventListener("click", () => installCertificate(true));
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
