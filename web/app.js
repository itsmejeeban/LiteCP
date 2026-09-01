(() => {
  "use strict";

  const $ = (selector, root = document) => root.querySelector(selector);
  const $$ = (selector, root = document) => [...root.querySelectorAll(selector)];
  const params = new URLSearchParams(location.search);
  const demoMode = location.protocol === "file:" || (["localhost", "127.0.0.1"].includes(location.hostname) && params.has("demo"));
  let currentState = null;
  let credentialText = "";
  let logTimer = null;

  const icons = {
    globe: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c3 3.5 3 14 0 18M12 3c-3 3.5-3 14 0 18"/></svg>',
    shield: '<svg viewBox="0 0 24 24"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10Z"/><path d="m9 12 2 2 4-5"/></svg>',
    clock: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></svg>',
    refresh: '<svg viewBox="0 0 24 24"><path d="M20 7v5a8 8 0 1 1-2.3-5.7L20 8"/><path d="M20 3v5h-5"/></svg>',
    trash: '<svg viewBox="0 0 24 24"><path d="M4 7h16M9 7V4h6v3M7 7l1 14h8l1-14M10 11v6M14 11v6"/></svg>',
    empty: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path d="M8 12h8M12 8v8"/></svg>'
  };

  const serviceMeta = {
    nginx: { symbol: "NX", description: "Handles secure web traffic and static assets." },
    php: { symbol: "PHP", description: "Runs the WHMCS application through FastCGI." },
    database: { symbol: "DB", description: "Stores clients, invoices and application data." },
    cron: { symbol: "CR", description: "Executes scheduled billing and automation tasks." }
  };

  const demoState = {
    version: "0.1.0",
    username: "admin",
    settings: { email: "admin@hostingtom.com", timezone: "Asia/Kolkata", backupRetention: 14 },
    sites: [
      { id: "site_demo_1", domain: "billing.hostingtom.com", root: "/var/www/litecp/billing.hostingtom.com/public", phpVersion: "8.3", ssl: true, cron: true, database: "cp_billing", dbUser: "cp_whmcs", createdAt: new Date(Date.now() - 32 * 86400000).toISOString() },
      { id: "site_demo_2", domain: "clients.northstar.test", root: "/var/www/litecp/clients.northstar.test/public", phpVersion: "8.3", ssl: false, cron: true, database: "cp_clients", dbUser: "cp_clients", createdAt: new Date(Date.now() - 3 * 86400000).toISOString() }
    ],
    backups: [
      { id: "backup_1", siteId: "site_demo_1", domain: "billing.hostingtom.com", path: "/var/backups/litecp/billing.hostingtom.com-20260831-020000.tar.gz", size: 193715264, status: "complete", createdAt: new Date(Date.now() - 7 * 3600000).toISOString() },
      { id: "backup_2", siteId: "site_demo_2", domain: "clients.northstar.test", path: "/var/backups/litecp/clients.northstar.test-20260830-020000.tar.gz", size: 128427910, status: "complete", createdAt: new Date(Date.now() - 31 * 3600000).toISOString() }
    ],
    activities: [
      { id: "a1", title: "Backup completed", detail: "billing.hostingtom.com files and database saved", kind: "success", createdAt: new Date(Date.now() - 7 * 3600000).toISOString() },
      { id: "a2", title: "SSL enabled", detail: "billing.hostingtom.com now redirects to HTTPS", kind: "success", createdAt: new Date(Date.now() - 2 * 86400000).toISOString() },
      { id: "a3", title: "Website added", detail: "clients.northstar.test is ready for WHMCS files", kind: "success", createdAt: new Date(Date.now() - 3 * 86400000).toISOString() }
    ],
    system: {
      os: "Ubuntu 24.04 LTS", osId: "ubuntu", hostname: "whmcs-prod-01", ipAddress: "203.0.113.42", uptimeSeconds: 1240920, cpuPercent: 12.4, loadAverage: 0.18, demo: true,
      memory: { used: 887095296, total: 2147483648, percent: 41.3 },
      disk: { used: 19327352832, total: 85899345920, percent: 22.5 },
      services: [
        { key: "nginx", label: "Nginx", unit: "nginx", state: "running" },
        { key: "php", label: "PHP 8.3 FPM", unit: "php8.3-fpm", state: "running" },
        { key: "database", label: "MariaDB", unit: "mariadb", state: "running" },
        { key: "cron", label: "WHMCS Scheduler", unit: "cron", state: "running" }
      ]
    }
  };

  async function api(path, options = {}) {
    if (demoMode) return mockAPI(path, options);
    const config = { credentials: "same-origin", ...options };
    if (config.body && typeof config.body !== "string") {
      config.headers = { "Content-Type": "application/json", ...(config.headers || {}) };
      config.body = JSON.stringify(config.body);
    }
    const response = await fetch(path, config);
    let payload = {};
    try { payload = await response.json(); } catch { /* use status fallback */ }
    if (!response.ok) {
      const error = new Error(payload.error || `Request failed (${response.status})`);
      error.status = response.status;
      throw error;
    }
    return payload;
  }

  async function mockAPI(path, options) {
    await new Promise(resolve => setTimeout(resolve, options.method && options.method !== "GET" ? 420 : 100));
    if (path === "/api/state") return structuredClone(demoState);
    if (path === "/api/login") return { ok: true };
    if (path.startsWith("/api/logs")) return { target: "demo", lines: [
      "2026/09/01 10:18:02 [notice] LiteCP preview log stream connected",
      "2026/09/01 10:17:55 [info] PHP-FPM pool www is healthy (2 idle workers)",
      "2026/09/01 10:17:48 [info] GET /clientarea.php 200 42ms",
      "2026/09/01 10:15:01 [info] WHMCS automation cron completed successfully"
    ] };
    if (path === "/api/sites" && options.method === "POST") {
      const input = options.body;
      const site = { id: `demo_${Date.now()}`, domain: input.domain, root: `/var/www/litecp/${input.domain}/public`, phpVersion: "8.3", ssl: false, cron: !!input.enableCron, database: input.createDatabase ? "cp_new_whmcs" : "", dbUser: input.createDatabase ? "cp_new_whmcs" : "", createdAt: new Date().toISOString() };
      demoState.sites.push(site);
      return { site, database: input.createDatabase ? { name: "cp_new_whmcs", username: "cp_new_whmcs", password: "uG7J6Jb8z2YdZPBJqx9YdX9n", host: "localhost" } : null };
    }
    if (path === "/api/backups" && options.method === "POST") {
      const site = demoState.sites.find(item => item.id === options.body.siteId);
      const backup = { id: `backup_${Date.now()}`, siteId: site.id, domain: site.domain, path: `/var/backups/litecp/${site.domain}-preview.tar.gz`, size: 184812032, status: "complete", createdAt: new Date().toISOString() };
      demoState.backups.unshift(backup);
      return backup;
    }
    if (path === "/api/sites/ssl") {
      const site = demoState.sites.find(item => item.id === options.body.id);
      if (site) site.ssl = true;
      return { ok: true };
    }
    if (path === "/api/sites/cron") {
      const site = demoState.sites.find(item => item.id === options.body.id);
      if (site) site.cron = options.body.enabled;
      return { ok: true };
    }
    if (path.startsWith("/api/sites/") && options.method === "DELETE") {
      const id = path.split("/").pop();
      demoState.sites = demoState.sites.filter(item => item.id !== id);
      return { ok: true };
    }
    if (path === "/api/settings") {
      demoState.settings = { ...options.body };
      return demoState.settings;
    }
    return { ok: true };
  }

  async function initialize() {
    bindEvents();
    setGreeting();
    if (demoMode) {
      currentState = await api("/api/state");
      showApp();
      return;
    }
    try {
      currentState = await api("/api/state");
      showApp();
    } catch (error) {
      if (error.status !== 401) showToast("Connection problem", error.message, "error");
      showLogin();
    }
  }

  function bindEvents() {
    $("#loginForm").addEventListener("submit", login);
    $("#logoutButton").addEventListener("click", logout);
    $("#refreshButton").addEventListener("click", refreshState);
    $("#profileButton").addEventListener("click", event => { event.stopPropagation(); $("#profileMenu").classList.toggle("hidden"); });
    document.addEventListener("click", () => $("#profileMenu").classList.add("hidden"));
    $$("[data-password-toggle]").forEach(button => button.addEventListener("click", () => togglePassword(button)));
    $$("[data-page]").forEach(link => link.addEventListener("click", () => navigate(link.dataset.page)));
    $$("[data-page-target]").forEach(button => button.addEventListener("click", () => navigate(button.dataset.pageTarget)));
    $$("[data-open-site-modal]").forEach(button => button.addEventListener("click", openSiteModal));
    $$(".modal-close").forEach(button => button.addEventListener("click", closeSiteModal));
    $("#siteModal").addEventListener("click", event => { if (event.target.id === "siteModal") closeSiteModal(); });
    $("#siteForm").addEventListener("submit", createSite);
    $("#closeCredentials").addEventListener("click", closeCredentials);
    $("#copyCredentials").addEventListener("click", copyCredentials);
    $("#sitesTable").addEventListener("click", handleSiteAction);
    $("#siteSearch").addEventListener("input", renderSites);
    $("#servicesGrid").addEventListener("click", handleServiceAction);
    $("#restartAll").addEventListener("click", restartAll);
    $("#backupButton").addEventListener("click", runBackup);
    $$('[data-quick-backup]').forEach(button => button.addEventListener("click", runBackup));
    $("#settingsForm").addEventListener("submit", saveSettings);
    $("#passwordForm").addEventListener("submit", changePassword);
    $("#reloadLogs").addEventListener("click", loadLogs);
    $("#logTarget").addEventListener("change", loadLogs);
    $("#mobileMenu").addEventListener("click", toggleSidebar);
    $("#sidebarScrim").addEventListener("click", closeSidebar);
    window.addEventListener("hashchange", () => navigate(location.hash.slice(1) || "dashboard", false));
    document.addEventListener("keydown", event => {
      if (event.key === "Escape") { closeSiteModal(); $("#profileMenu").classList.add("hidden"); closeSidebar(); }
    });
  }

  function showLogin() {
    $("#appView").classList.add("hidden");
    $("#loginView").classList.remove("hidden");
    setTimeout(() => $("#loginPassword").focus(), 50);
  }

  function showApp() {
    $("#loginView").classList.add("hidden");
    $("#appView").classList.remove("hidden");
    renderAll();
    navigate(location.hash.slice(1) || "dashboard", false);
  }

  async function login(event) {
    event.preventDefault();
    const form = event.currentTarget;
    const submit = $("button[type=submit]", form);
    const errorNode = $("#loginError");
    submit.disabled = true;
    errorNode.classList.add("hidden");
    try {
      const data = new FormData(form);
      await api("/api/login", { method: "POST", body: { username: data.get("username"), password: data.get("password") } });
      currentState = await api("/api/state");
      form.reset();
      showApp();
    } catch (error) {
      errorNode.textContent = error.message;
      errorNode.classList.remove("hidden");
    } finally { submit.disabled = false; }
  }

  async function logout() {
    try { await api("/api/logout", { method: "POST" }); } catch { /* clear local view anyway */ }
    currentState = null;
    showLogin();
  }

  async function refreshState() {
    const button = $("#refreshButton");
    button.classList.add("spinning");
    try {
      currentState = await api("/api/state");
      renderAll();
      showToast("Server refreshed", "Metrics and service health are up to date.");
    } catch (error) { handleError(error); }
    finally { setTimeout(() => button.classList.remove("spinning"), 350); }
  }

  function renderAll() {
    if (!currentState) return;
    const { system, username, settings, version } = currentState;
    const displayName = titleCase(username);
    $("#topOS").textContent = system.os;
    $("#topHostname").textContent = system.hostname;
    $("#profileName").textContent = displayName;
    $("#greetingName").textContent = displayName;
    $("#avatar").textContent = username.slice(0, 1).toUpperCase();
    $("#sideVersion").textContent = `v${version}`;
    $("#settingsVersion").textContent = `v${version}`;
    $("#settingsOS").textContent = system.os;
    $("#detailOS").textContent = system.os;
    $("#detailIP").textContent = system.ipAddress;
    $("#modalServerIP").textContent = system.ipAddress;
    $("#siteNavCount").textContent = currentState.sites.length;
    renderMetrics();
    renderServices();
    renderActivity();
    renderSites();
    renderBackups();
    fillSettings(settings);
    const healthy = system.services.every(service => service.state === "running");
    $("#noticeBanner").querySelector("strong").textContent = healthy ? "Your WHMCS stack is healthy" : "One or more services need attention";
    $("#noticeBanner").querySelector("small").textContent = healthy ? "All required services are online and responding normally." : "Open Services to review and restart the affected component.";
  }

  function renderMetrics() {
    const system = currentState.system;
    setMetric("cpu", system.cpuPercent);
    setMetric("memory", system.memory.percent);
    setMetric("disk", system.disk.percent);
    $("#loadValue").textContent = `Load average ${Number(system.loadAverage).toFixed(2)}`;
    $("#memoryDetail").textContent = `${formatBytes(system.memory.used)} of ${formatBytes(system.memory.total)} used`;
    $("#diskDetail").textContent = `${formatBytes(system.disk.used)} of ${formatBytes(system.disk.total)} used`;
    const remaining = Math.max(0, 100 - system.memory.percent);
    $("#memoryTrend").textContent = `${remaining.toFixed(0)}% free`;
    $("#uptimeValue").textContent = formatUptime(system.uptimeSeconds);
  }

  function setMetric(name, value) {
    const safe = Math.max(0, Math.min(100, Number(value) || 0));
    $(`#${name}Value`).textContent = safe.toFixed(safe < 10 ? 1 : 0);
    $(`#${name}Bar`).style.width = `${safe}%`;
    if (safe > 82) $(`#${name}Bar`).style.background = "#d95858";
    else if (safe > 65) $(`#${name}Bar`).style.background = "#e99b39";
  }

  function renderServices() {
    const services = currentState.system.services || [];
    $("#dashboardServices").innerHTML = services.map(service => {
      const meta = serviceMeta[service.key] || { symbol: "SV", description: "Essential server process." };
      const stopped = service.state !== "running";
      return `<div class="service-row"><span class="service-symbol">${meta.symbol}</span><span class="service-copy"><strong>${escapeHTML(service.label)}</strong><small>${escapeHTML(service.unit)}</small></span><span class="state-pill ${stopped ? "stopped" : ""}"><span class="pulse-dot"></span>${escapeHTML(service.state)}</span></div>`;
    }).join("");
    $("#servicesGrid").innerHTML = services.map(service => {
      const meta = serviceMeta[service.key] || { symbol: "SV", description: "Essential server process." };
      const stopped = service.state !== "running";
      return `<article class="service-card"><span class="service-card-icon">${meta.symbol}</span><div class="service-card-copy"><h3>${escapeHTML(service.label)}</h3><p>${escapeHTML(meta.description)}</p><span class="service-unit">${escapeHTML(service.unit)}.service</span></div><div class="service-card-actions"><span class="state-pill ${stopped ? "stopped" : ""}"><span class="pulse-dot"></span>${escapeHTML(service.state)}</span><button class="button button-secondary" data-service="${escapeHTML(service.key)}" data-action="${stopped ? "start" : "restart"}">${stopped ? "Start" : "Restart"}</button></div></article>`;
    }).join("");
  }

  function renderActivity() {
    const activities = (currentState.activities || []).slice(0, 5);
    if (!activities.length) {
      $("#activityList").innerHTML = '<div class="empty-state"><p>No panel activity yet.</p></div>';
      return;
    }
    $("#activityList").innerHTML = activities.map(item => `<div class="activity-row"><span class="activity-dot ${item.kind === "warning" ? "warning" : ""}"></span><div><strong>${escapeHTML(item.title)}</strong><small>${escapeHTML(item.detail)}</small></div><span class="activity-time">${relativeTime(item.createdAt)}</span></div>`).join("");
  }

  function renderSites() {
    const query = $("#siteSearch").value.trim().toLowerCase();
    const sites = currentState.sites.filter(site => site.domain.includes(query));
    $("#siteCount").textContent = currentState.sites.length;
    $("#sslCount").textContent = currentState.sites.filter(site => site.ssl).length;
    $("#cronCount").textContent = currentState.sites.filter(site => site.cron).length;
    if (!sites.length) {
      const copy = currentState.sites.length ? "No domains match your search." : "Provision your WHMCS domain, database and automation schedule in one short step.";
      $("#sitesTable").innerHTML = `<div class="empty-state"><span class="empty-icon">${icons.globe}</span><h3>${currentState.sites.length ? "Nothing found" : "No websites yet"}</h3><p>${copy}</p>${currentState.sites.length ? "" : '<button class="button button-primary" data-empty-add>Add your first website</button>'}</div>`;
      const emptyButton = $("[data-empty-add]", $("#sitesTable"));
      if (emptyButton) emptyButton.addEventListener("click", openSiteModal);
      return;
    }
    $("#sitesTable").innerHTML = `<table class="data-table"><thead><tr><th>DOMAIN</th><th>PHP</th><th>SSL</th><th>WHMCS CRON</th><th>CREATED</th><th></th></tr></thead><tbody>${sites.map(site => `<tr><td><span class="domain-cell"><span class="domain-favicon">${escapeHTML(site.domain[0])}</span><span class="domain-copy"><strong>${escapeHTML(site.domain)}</strong><small>${escapeHTML(site.root)}</small></span></span></td><td><span class="badge">PHP ${escapeHTML(site.phpVersion)}</span></td><td><span class="badge ${site.ssl ? "success" : ""}">${site.ssl ? '<span class="pulse-dot"></span>Protected' : "Not enabled"}</span></td><td><span class="badge ${site.cron ? "success" : ""}">${site.cron ? '<span class="pulse-dot"></span>Every 5 min' : "Disabled"}</span></td><td>${formatDate(site.createdAt)}</td><td><span class="row-actions">${site.ssl ? "" : `<button title="Enable SSL" data-site-action="ssl" data-id="${site.id}">${icons.shield}</button>`}<button title="${site.cron ? "Disable" : "Enable"} WHMCS cron" data-site-action="cron" data-id="${site.id}">${icons.clock}</button><button class="danger" title="Detach website" data-site-action="delete" data-id="${site.id}">${icons.trash}</button></span></td></tr>`).join("")}</tbody></table>`;
  }

  function renderBackups() {
    $("#retentionChip").textContent = `${currentState.settings.backupRetention}-day policy`;
    const backups = currentState.backups || [];
    if (!backups.length) {
      $("#backupsTable").innerHTML = `<div class="empty-state"><span class="empty-icon">${icons.refresh}</span><h3>No backups yet</h3><p>Create a recovery point after your first WHMCS website is provisioned.</p></div>`;
      return;
    }
    $("#backupsTable").innerHTML = `<table class="data-table"><thead><tr><th>WEBSITE</th><th>CREATED</th><th>SIZE</th><th>STATUS</th><th>STORAGE PATH</th></tr></thead><tbody>${backups.map(backup => `<tr><td><strong>${escapeHTML(backup.domain)}</strong></td><td>${formatDateTime(backup.createdAt)}</td><td>${formatBytes(backup.size)}</td><td><span class="badge success"><span class="pulse-dot"></span>${escapeHTML(backup.status)}</span></td><td><code>${escapeHTML(backup.path)}</code></td></tr>`).join("")}</tbody></table>`;
  }

  function fillSettings(settings) {
    $("#settingsEmail").value = settings.email || "";
    $("#settingsTimezone").value = settings.timezone || "UTC";
    $("#settingsRetention").value = settings.backupRetention || 14;
  }

  function navigate(page, updateHash = true) {
    const allowed = ["dashboard", "websites", "services", "backups", "logs", "settings"];
    if (!allowed.includes(page)) page = "dashboard";
    $$("[data-page-view]").forEach(view => view.classList.toggle("active", view.dataset.pageView === page));
    $$("[data-page]").forEach(link => link.classList.toggle("active", link.dataset.page === page));
    if (updateHash && location.hash !== `#${page}`) history.pushState(null, "", `#${page}`);
    closeSidebar();
    window.scrollTo({ top: 0, behavior: "smooth" });
    clearInterval(logTimer);
    if (page === "logs") {
      loadLogs();
      logTimer = setInterval(loadLogs, 30000);
    }
  }

  function openSiteModal() {
    $("#siteModal").classList.remove("hidden");
    setTimeout(() => $('#siteForm input[name="domain"]').focus(), 50);
  }

  function closeSiteModal() { $("#siteModal").classList.add("hidden"); }

  async function createSite(event) {
    event.preventDefault();
    const form = event.currentTarget;
    const submit = $('button[type="submit"]', form);
    const data = new FormData(form);
    const body = { domain: String(data.get("domain")).trim().toLowerCase(), createDatabase: data.get("createDatabase") === "on", enableCron: data.get("enableCron") === "on" };
    submit.disabled = true;
    submit.textContent = "Provisioning…";
    try {
      const result = await api("/api/sites", { method: "POST", body });
      closeSiteModal();
      form.reset();
      currentState = await api("/api/state");
      renderAll();
      if (result.database) showCredentials(result.database, result.site);
      else showToast("Website provisioned", `${result.site.domain} is ready for WHMCS files.`);
    } catch (error) { handleError(error); }
    finally { submit.disabled = false; submit.innerHTML = 'Provision website <svg viewBox="0 0 24 24"><path d="m9 18 6-6-6-6"/></svg>'; }
  }

  function showCredentials(credentials, site) {
    const rows = [["Website", site.domain], ["Database host", credentials.host], ["Database name", credentials.name], ["Database user", credentials.username], ["Database password", credentials.password]];
    credentialText = rows.map(([label, value]) => `${label}: ${value}`).join("\n");
    $("#credentialList").innerHTML = rows.map(([label, value]) => `<div><dt>${escapeHTML(label)}</dt><dd>${escapeHTML(value)}</dd></div>`).join("");
    $("#credentialsModal").classList.remove("hidden");
  }

  function closeCredentials() { credentialText = ""; $("#credentialsModal").classList.add("hidden"); }
  async function copyCredentials() {
    try { await navigator.clipboard.writeText(credentialText); showToast("Copied", "Database credentials copied to your clipboard."); }
    catch { showToast("Copy unavailable", "Select and copy the credentials manually.", "error"); }
  }

  async function handleSiteAction(event) {
    const button = event.target.closest("[data-site-action]");
    if (!button) return;
    const site = currentState.sites.find(item => item.id === button.dataset.id);
    if (!site) return;
    button.disabled = true;
    try {
      if (button.dataset.siteAction === "ssl") {
        let email = currentState.settings.email;
        if (!email) email = prompt("Email for Let's Encrypt renewal notices:", "") || "";
        if (!email) return;
        await api("/api/sites/ssl", { method: "POST", body: { id: site.id, email } });
        showToast("SSL enabled", `${site.domain} now redirects to HTTPS.`);
      } else if (button.dataset.siteAction === "cron") {
        await api("/api/sites/cron", { method: "POST", body: { id: site.id, enabled: !site.cron } });
        showToast("Schedule updated", `WHMCS cron ${site.cron ? "disabled" : "enabled"} for ${site.domain}.`);
      } else if (button.dataset.siteAction === "delete") {
        const accepted = confirm(`Detach ${site.domain} from LiteCP?\n\nIts files and database will be preserved.`);
        if (!accepted) return;
        await api(`/api/sites/${encodeURIComponent(site.id)}`, { method: "DELETE" });
        showToast("Website detached", "Nginx and cron configuration removed; data was preserved.");
      }
      currentState = await api("/api/state");
      renderAll();
    } catch (error) { handleError(error); }
    finally { button.disabled = false; }
  }

  async function handleServiceAction(event) {
    const button = event.target.closest("[data-service]");
    if (!button) return;
    button.disabled = true;
    const label = button.textContent;
    button.textContent = "Working…";
    try {
      await api("/api/services/action", { method: "POST", body: { service: button.dataset.service, action: button.dataset.action } });
      showToast("Service updated", `${titleCase(button.dataset.service)} ${button.dataset.action} completed.`);
      currentState = await api("/api/state");
      renderAll();
    } catch (error) { handleError(error); button.textContent = label; }
  }

  async function restartAll() {
    const button = $("#restartAll");
    if (!confirm("Restart Nginx, PHP-FPM, MariaDB and cron in sequence?")) return;
    button.disabled = true;
    const original = button.innerHTML;
    button.textContent = "Restarting stack…";
    try {
      for (const service of ["php", "database", "cron", "nginx"]) await api("/api/services/action", { method: "POST", body: { service, action: "restart" } });
      showToast("Stack restarted", "All essential services restarted successfully.");
      currentState = await api("/api/state");
      renderAll();
    } catch (error) { handleError(error); }
    finally { button.disabled = false; button.innerHTML = original; }
  }

  async function runBackup() {
    if (!currentState.sites.length) { showToast("No website available", "Add a WHMCS website before creating a backup.", "error"); return; }
    let site = currentState.sites[0];
    if (currentState.sites.length > 1) {
      const choices = currentState.sites.map((item, index) => `${index + 1}. ${item.domain}`).join("\n");
      const selected = Number(prompt(`Choose a website to back up:\n\n${choices}`, "1"));
      if (!selected || !currentState.sites[selected - 1]) return;
      site = currentState.sites[selected - 1];
    }
    showToast("Backup started", `Archiving ${site.domain}. This may take a moment.`);
    try {
      await api("/api/backups", { method: "POST", body: { siteId: site.id } });
      currentState = await api("/api/state");
      renderAll();
      showToast("Backup completed", `${site.domain} files and database were saved.`);
    } catch (error) { handleError(error); }
  }

  async function saveSettings(event) {
    event.preventDefault();
    const submit = $('button[type="submit"]', event.currentTarget);
    submit.disabled = true;
    try {
      const body = { email: $("#settingsEmail").value.trim(), timezone: $("#settingsTimezone").value, backupRetention: Number($("#settingsRetention").value) };
      await api("/api/settings", { method: "PUT", body });
      currentState.settings = body;
      renderAll();
      showToast("Preferences saved", "Your LiteCP defaults were updated.");
    } catch (error) { handleError(error); }
    finally { submit.disabled = false; }
  }

  async function changePassword(event) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    if (data.get("next") !== data.get("confirm")) { showToast("Passwords do not match", "Re-enter the new password in both fields.", "error"); return; }
    const submit = $('button[type="submit"]', form);
    submit.disabled = true;
    try {
      await api("/api/password", { method: "PUT", body: { current: data.get("current"), next: data.get("next") } });
      form.reset();
      showToast("Password updated", "Use your new password the next time you sign in.");
    } catch (error) { handleError(error); }
    finally { submit.disabled = false; }
  }

  async function loadLogs() {
    const viewer = $("#logViewer code");
    viewer.textContent = "Loading log entries…";
    try {
      const result = await api(`/api/logs?target=${encodeURIComponent($("#logTarget").value)}`);
      viewer.textContent = result.lines.length ? result.lines.join("\n") : "No recent entries for this source.";
    } catch (error) { viewer.textContent = `Could not load logs: ${error.message}`; }
  }

  function toggleSidebar() { $("#sidebar").classList.toggle("open"); $("#sidebarScrim").classList.toggle("visible"); }
  function closeSidebar() { $("#sidebar").classList.remove("open"); $("#sidebarScrim").classList.remove("visible"); }
  function togglePassword(button) { const input = $(`#${button.dataset.passwordToggle}`); input.type = input.type === "password" ? "text" : "password"; }
  function setGreeting() { const hour = new Date().getHours(); $("#greetingTime").textContent = hour < 12 ? "morning" : hour < 18 ? "afternoon" : "evening"; }

  function showToast(title, message, type = "success") {
    const toast = document.createElement("div");
    toast.className = `toast ${type}`;
    const strong = document.createElement("strong");
    const span = document.createElement("span");
    strong.textContent = title;
    span.textContent = message;
    toast.append(strong, span);
    $("#toastRegion").append(toast);
    setTimeout(() => toast.remove(), 4200);
  }

  function handleError(error) {
    if (error.status === 401) { showLogin(); return; }
    showToast("Action failed", error.message || "The server could not complete that action.", "error");
  }

  function escapeHTML(value) { return String(value ?? "").replace(/[&<>'"]/g, character => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" })[character]); }
  function titleCase(value) { return String(value || "admin").replace(/(^|[-_\s])\w/g, match => match.toUpperCase()).replace(/[-_]/g, " "); }
  function formatBytes(bytes) { const value = Number(bytes) || 0; if (!value) return "0 B"; const units = ["B", "KB", "MB", "GB", "TB"]; const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1); return `${(value / 1024 ** index).toFixed(index > 2 ? 1 : 0)} ${units[index]}`; }
  function formatUptime(seconds) { const days = Math.floor(seconds / 86400); const hours = Math.floor((seconds % 86400) / 3600); return days ? `${days}d ${hours}h` : `${hours}h ${Math.floor((seconds % 3600) / 60)}m`; }
  function formatDate(value) { return new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", year: "numeric" }).format(new Date(value)); }
  function formatDateTime(value) { return new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }).format(new Date(value)); }
  function relativeTime(value) { const seconds = Math.max(1, Math.floor((Date.now() - new Date(value).getTime()) / 1000)); if (seconds < 60) return "just now"; if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`; if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`; return `${Math.floor(seconds / 86400)}d ago`; }

  initialize();
})();
