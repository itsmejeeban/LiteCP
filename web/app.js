(() => {
  "use strict";

  const $ = (selector, root = document) => root.querySelector(selector);
  const $$ = (selector, root = document) => [...root.querySelectorAll(selector)];
  const params = new URLSearchParams(location.search);
  const demoMode = location.protocol === "file:" || (["localhost", "127.0.0.1"].includes(location.hostname) && params.has("demo"));
  let currentState = null;
  let credentialText = "";
  let logTimer = null;
  let currentFilePath = "";
  let fileListingRequest = 0;
  let lastFileListing = null;
  let editorFilePath = "";
  let editorSiteID = "";
  let editorRequest = 0;
  let editorSaving = false;
  let editorOriginal = "";

  const icons = {
    copy: '<svg viewBox="0 0 24 24"><rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V4H4v12h4"/></svg>',
    globe: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c3 3.5 3 14 0 18M12 3c-3 3.5-3 14 0 18"/></svg>',
    shield: '<svg viewBox="0 0 24 24"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10Z"/><path d="m9 12 2 2 4-5"/></svg>',
    clock: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></svg>',
    refresh: '<svg viewBox="0 0 24 24"><path d="M20 7v5a8 8 0 1 1-2.3-5.7L20 8"/><path d="M20 3v5h-5"/></svg>',
    trash: '<svg viewBox="0 0 24 24"><path d="M4 7h16M9 7V4h6v3M7 7l1 14h8l1-14M10 11v6M14 11v6"/></svg>',
    folder: '<svg viewBox="0 0 24 24"><path d="M3 6h7l2 2h9v11H3z"/><path d="M3 9h18"/></svg>',
    file: '<svg viewBox="0 0 24 24"><path d="M6 3h9l4 4v14H6z"/><path d="M14 3v5h5"/></svg>',
    empty: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path d="M8 12h8M12 8v8"/></svg>'
  };

  const serviceMeta = {
    nginx: { symbol: "NX", description: "Handles secure web traffic and static assets." },
    php: { symbol: "PHP", description: "Runs per-site PHP versions through isolated FastCGI pools." },
    database: { symbol: "DB", description: "Stores clients, invoices and application data." },
    cron: { symbol: "CR", description: "Executes scheduled WHMCS and WordPress automation tasks." },
    mail: { symbol: "MX", description: "Provides authenticated SMTP submission and IMAP mailboxes." }
  };

  const demoState = {
    version: "0.5.1",
    username: "admin",
    settings: { email: "admin@hostingtom.com", timezone: "Asia/Kolkata", backupRetention: 14, emailHostingEnabled: true, resolvers: ["8.8.8.8", "8.8.4.4"] },
    sites: [
      { id: "site_demo_1", domain: "billing.hostingtom.com", root: "/var/www/litecp/billing.hostingtom.com/public", appType: "whmcs", appVersion: "9.0", phpVersion: "8.3", ssl: true, cron: true, wafMode: "block", database: "cp_billing", dbUser: "cp_whmcs", createdAt: new Date(Date.now() - 32 * 86400000).toISOString() },
      { id: "site_demo_2", domain: "blog.northstar.test", root: "/var/www/litecp/blog.northstar.test/public", appType: "wordpress", phpVersion: "8.4", ssl: false, cron: true, wafMode: "detect", database: "cp_blog", dbUser: "cp_blog", createdAt: new Date(Date.now() - 3 * 86400000).toISOString() }
    ],
    backups: [
      { id: "backup_1", siteId: "site_demo_1", domain: "billing.hostingtom.com", path: "/var/backups/litecp/billing.hostingtom.com-20260831-020000.tar.gz", size: 193715264, status: "complete", createdAt: new Date(Date.now() - 7 * 3600000).toISOString() },
      { id: "backup_2", siteId: "site_demo_2", domain: "clients.northstar.test", path: "/var/backups/litecp/clients.northstar.test-20260830-020000.tar.gz", size: 128427910, status: "complete", createdAt: new Date(Date.now() - 31 * 3600000).toISOString() }
    ],
    backupDestinations: [{ id: "remote_demo", name: "Off-site SFTP", protocol: "sftp", host: "backup.hostingtom.test", port: 22, username: "litecp", remotePath: "/backups", hasPassword: true, createdAt: new Date().toISOString() }],
    databases: [
      { id: "db_demo_1", name: "cp_billing", username: "cp_whmcs", siteId: "site_demo_1", domain: "billing.hostingtom.com", createdAt: new Date(Date.now() - 32 * 86400000).toISOString() },
      { id: "db_demo_2", name: "cp_blog", username: "cp_blog", siteId: "site_demo_2", domain: "blog.northstar.test", createdAt: new Date(Date.now() - 3 * 86400000).toISOString() }
    ],
    firewallRules: [{ id: "fw_demo", port: 10050, protocol: "tcp", source: "203.0.113.0/24", label: "Monitoring", createdAt: new Date().toISOString() }],
    mailDomains: [{ id: "md_demo", domain: "hostingtom.com", createdAt: new Date().toISOString() }],
    mailboxes: [{ id: "mb_demo", domain: "hostingtom.com", address: "billing@hostingtom.com", quotaMb: 2048, createdAt: new Date().toISOString() }],
    mailAliases: [{ id: "ma_demo", source: "sales@hostingtom.com", destination: "billing@hostingtom.com", createdAt: new Date().toISOString() }],
    activities: [
      { id: "a1", title: "Backup completed", detail: "billing.hostingtom.com files and database saved", kind: "success", createdAt: new Date(Date.now() - 7 * 3600000).toISOString() },
      { id: "a2", title: "SSL enabled", detail: "billing.hostingtom.com now redirects to HTTPS", kind: "success", createdAt: new Date(Date.now() - 2 * 86400000).toISOString() },
      { id: "a3", title: "Website added", detail: "blog.northstar.test is ready for WordPress", kind: "success", createdAt: new Date(Date.now() - 3 * 86400000).toISOString() }
    ],
    system: {
      os: "Ubuntu 24.04 LTS", osId: "ubuntu", hostname: "app-prod-01", ipAddress: "203.0.113.42", uptimeSeconds: 1240920, cpuPercent: 12.4, loadAverage: 0.18, demo: true,
      memory: { used: 887095296, total: 2147483648, percent: 41.3 },
      disk: { used: 19327352832, total: 85899345920, percent: 22.5 },
      services: [
        { key: "nginx", label: "Nginx", unit: "nginx", state: "running" },
        { key: "php", label: "Multi-PHP FPM", unit: "php7.3-fpm … php8.4-fpm", state: "running" },
        { key: "database", label: "MySQL", unit: "mysqld", state: "running" },
        { key: "cron", label: "App Scheduler", unit: "cron", state: "running" },
        { key: "mail", label: "Email Hosting", unit: "postfix, dovecot", state: "running" }
      ],
      phpVersions: ["7.3", "7.4", "8.1", "8.2", "8.3", "8.4"], databaseEngine: "MySQL", firewallBackend: "ufw"
    }
  };

  async function api(path, options = {}) {
    if (demoMode) return mockAPI(path, options);
    const config = { credentials: "same-origin", ...options };
    if (config.body && typeof config.body !== "string" && !(config.body instanceof FormData)) {
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
    if (path.startsWith("/api/activities")) {
      const query = new URL(path, location.href).searchParams;
      const page = Math.max(1, Number(query.get("page")) || 1);
      const perPage = Math.max(1, Number(query.get("perPage")) || 5);
      const start = (page - 1) * perPage;
      return { items: structuredClone(demoState.activities.slice(start, start + perPage)), total: demoState.activities.length, page, perPage };
    }
    if (path.startsWith("/api/logs")) return { target: "demo", lines: [
      "2026/09/01 10:18:02 [notice] LiteCP preview log stream connected",
      "2026/09/01 10:17:55 [info] PHP-FPM pool www is healthy (2 idle workers)",
      "2026/09/01 10:17:48 [info] GET /clientarea.php 200 42ms",
      "2026/09/01 10:15:01 [info] WHMCS automation cron completed successfully"
    ] };
    if (path === "/api/sites" && options.method === "POST") {
      const input = options.body;
      const databaseName = `cp_${String(input.domain).toLowerCase().replace(/[^a-z0-9]/g, "_").slice(0, 8)}_${Date.now().toString(36).slice(-5)}`;
      const site = { id: `demo_${Date.now()}`, domain: input.domain, root: `/var/www/litecp/${input.domain}/public`, appType: input.appType, appVersion: input.appVersion || "", phpVersion: input.phpVersion, ssl: false, cron: input.appType !== "regular" && !!input.enableCron, wafMode: "off", database: input.createDatabase ? databaseName : "", dbUser: input.createDatabase ? databaseName : "", createdAt: new Date().toISOString() };
      demoState.sites.push(site);
      if (input.createDatabase) demoState.databases.push({ id: `db_${Date.now()}`, name: databaseName, username: databaseName, siteId: site.id, domain: site.domain, createdAt: site.createdAt });
      return { site, database: input.createDatabase ? { name: databaseName, username: databaseName, password: "uG7J6Jb8z2YdZPBJqx9YdX9n", host: "localhost" } : null };
    }
    if (path === "/api/backups" && options.method === "POST") {
      const site = demoState.sites.find(item => item.id === options.body.siteId);
      const remote = demoState.backupDestinations.find(item => item.id === options.body.destinationId);
      const backup = { id: `backup_${Date.now()}`, siteId: site.id, domain: site.domain, path: `/var/backups/litecp/${site.domain}-preview.tar.gz`, size: 184812032, status: "complete", destinationName: remote?.name || "", remotePath: remote ? `${remote.protocol}://${remote.host}${remote.remotePath}/${site.domain}-preview.tar.gz` : "", createdAt: new Date().toISOString() };
      demoState.backups.unshift(backup);
      return backup;
    }
    if (path.startsWith("/api/files") && (!options.method || options.method === "GET")) {
      if (path.startsWith("/api/files/content")) return { path: "configuration.php", content: "<?php\n// WHMCS configuration preview\n", mode: "0640" };
      return { siteId: "site_demo_1", domain: "billing.hostingtom.com", path: currentFilePath, entries: [
        { name: "assets", path: currentFilePath ? `${currentFilePath}/assets` : "assets", type: "directory", size: 0, mode: "0750", modified: new Date().toISOString() },
        { name: "index.php", path: currentFilePath ? `${currentFilePath}/index.php` : "index.php", type: "file", size: 2840, mode: "0640", modified: new Date().toISOString() },
        { name: "whmcs.zip", path: currentFilePath ? `${currentFilePath}/whmcs.zip` : "whmcs.zip", type: "file", size: 48512000, mode: "0640", modified: new Date().toISOString() }
      ] };
    }
    if (path === "/api/files/create" && options.method === "POST") return { path: [options.body.path, options.body.name].filter(Boolean).join("/") };
    if (path === "/api/databases" && options.method === "POST") {
      const item = { id: `db_${Date.now()}`, name: options.body.name, username: options.body.username, createdAt: new Date().toISOString() };
      demoState.databases.push(item); return { database: item, password: options.body.password || "demo-generated-password", host: "localhost" };
    }
    if (path.startsWith("/api/databases/") && options.method === "DELETE") { demoState.databases = demoState.databases.filter(item => item.id !== path.split("/").pop()); return { ok: true }; }
    if (path === "/api/backup-destinations" && options.method === "POST") { const item = { id: `remote_${Date.now()}`, ...options.body, hasPassword: true, createdAt: new Date().toISOString() }; delete item.password; demoState.backupDestinations.push(item); return item; }
    if (path.startsWith("/api/backup-destinations/") && options.method === "DELETE") { demoState.backupDestinations = demoState.backupDestinations.filter(item => item.id !== path.split("/").pop()); return { ok: true }; }
    if (path === "/api/firewall" && options.method === "POST") { const item = { id: `fw_${Date.now()}`, ...options.body, createdAt: new Date().toISOString() }; demoState.firewallRules.push(item); return item; }
    if (path.startsWith("/api/firewall/") && options.method === "PUT") { const id = path.split("/").pop(); const index = demoState.firewallRules.findIndex(item => item.id === id); if (index >= 0) demoState.firewallRules[index] = { ...demoState.firewallRules[index], ...options.body, id }; return demoState.firewallRules[index]; }
    if (path.startsWith("/api/firewall/") && options.method === "DELETE") { demoState.firewallRules = demoState.firewallRules.filter(item => item.id !== path.split("/").pop()); return { ok: true }; }
    if (path === "/api/mail/domains" && options.method === "POST") { const item = { id: `md_${Date.now()}`, domain: options.body.domain, createdAt: new Date().toISOString() }; demoState.mailDomains.push(item); return item; }
    if (path === "/api/mail/mailboxes" && options.method === "POST") { const item = { id: `mb_${Date.now()}`, address: `${options.body.localPart}@${options.body.domain}`, domain: options.body.domain, quotaMb: options.body.quotaMb, createdAt: new Date().toISOString() }; demoState.mailboxes.push(item); return item; }
    if (path === "/api/mail/aliases" && options.method === "POST") { const item = { id: `ma_${Date.now()}`, source: options.body.source, destination: options.body.destination, createdAt: new Date().toISOString() }; demoState.mailAliases.push(item); return item; }
    if (path.startsWith("/api/mail/domains/") && options.method === "DELETE") { demoState.mailDomains = demoState.mailDomains.filter(item => item.id !== path.split("/").pop()); return { ok: true }; }
    if (path.startsWith("/api/mail/mailboxes/") && options.method === "DELETE") { demoState.mailboxes = demoState.mailboxes.filter(item => item.id !== path.split("/").pop()); return { ok: true }; }
    if (path.startsWith("/api/mail/aliases/") && options.method === "DELETE") { demoState.mailAliases = demoState.mailAliases.filter(item => item.id !== path.split("/").pop()); return { ok: true }; }
    if (path === "/api/mail/hosting" && options.method === "PUT") { demoState.settings.emailHostingEnabled = !!options.body.enabled; return { enabled: demoState.settings.emailHostingEnabled }; }
    if (path === "/api/mail/mailboxes/password" && options.method === "PUT") return { ok: true };
    if (path === "/api/sites/waf" && options.method === "PUT") { const site = demoState.sites.find(item => item.id === options.body.id); if (site) site.wafMode = options.body.mode; return { ok: true }; }
    if (path === "/api/system/hostname" && options.method === "PUT") { demoState.system.hostname = options.body.hostname; return { ok: true }; }
    if (path === "/api/system/resolvers" && options.method === "PUT") { demoState.settings.resolvers = [...options.body.resolvers]; return { resolvers: demoState.settings.resolvers }; }
    if (path === "/api/system/reboot" && options.method === "POST") return { scheduled: true };
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
    if (path === "/api/sites/php") {
      const site = demoState.sites.find(item => item.id === options.body.id);
      if (site) site.phpVersion = options.body.version;
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
    $("#siteAppType").addEventListener("change", configureSiteForm);
    $("#siteAppVersion").addEventListener("change", configureSiteForm);
    $("#closeCredentials").addEventListener("click", closeCredentials);
    $("#copyCredentials").addEventListener("click", copyCredentials);
    $("#sitesTable").addEventListener("click", handleSiteAction);
    $("#sitesTable").addEventListener("change", handlePHPChange);
    $("#siteSearch").addEventListener("input", renderSites);
    $("#servicesGrid").addEventListener("click", handleServiceAction);
    $("#restartAll").addEventListener("click", restartAll);
    $$('[data-quick-backup]').forEach(button => button.addEventListener("click", runBackup));
    $("#backupRunForm").addEventListener("submit", runBackup);
    $("#backupDestinationForm").addEventListener("submit", createBackupDestination);
    $("#backupProtocol").addEventListener("change", configureBackupProtocol);
    $("#backupDestinationsTable").addEventListener("click", handleBackupDestinationAction);
    $("#fileSiteSelect").addEventListener("change", () => { currentFilePath = ""; loadFiles(); });
    $("#refreshFilesButton").addEventListener("click", loadFiles);
    $("#fileUpButton").addEventListener("click", fileUp);
    $("#newFolderButton").addEventListener("click", createFolder);
    $("#uploadButton").addEventListener("click", () => $("#fileUploadInput").click());
    $("#fileUploadInput").addEventListener("change", uploadFiles);
    $("#archiveButton").addEventListener("click", archiveSelectedFiles);
    $("#fileTable").addEventListener("click", handleFileAction);
    $("#fileBreadcrumb").addEventListener("click", event => { const button = event.target.closest("[data-file-path]"); if (button) { currentFilePath = button.dataset.filePath; loadFiles(); } });
    $("#editorForm").addEventListener("submit", saveEditor);
    $("#closeEditor").addEventListener("click", closeEditor);
    $("#cancelEditor").addEventListener("click", closeEditor);
    $("#databaseForm").addEventListener("submit", createDatabase);
    $("#databasesTable").addEventListener("click", handleDatabaseAction);
    $("#firewallForm").addEventListener("submit", createFirewallRule);
    $("#firewallTable").addEventListener("click", handleFirewallAction);
    $("#mailDomainForm").addEventListener("submit", createMailDomain);
    $("#mailboxForm").addEventListener("submit", createMailbox);
    $("#mailAliasForm").addEventListener("submit", createMailAlias);
    $("#mailDomainsTable").addEventListener("click", handleMailAction);
    $("#mailboxesTable").addEventListener("click", handleMailAction);
    $("#mailAliasesTable").addEventListener("click", handleMailAction);
    $$("form[data-settings-part]").forEach(form => form.addEventListener("submit", saveSettings));
    $("#passwordForm").addEventListener("submit", changePassword);
    $("#reloadLogs").addEventListener("click", loadLogs);
    $("#logTarget").addEventListener("change", loadLogs);
    $("#mobileMenu").addEventListener("click", toggleSidebar);
    $("#sidebarScrim").addEventListener("click", closeSidebar);
    window.addEventListener("hashchange", () => navigate(location.hash.slice(1) || "dashboard", false));
    document.addEventListener("keydown", event => {
      if (event.key === "Escape") { closeSiteModal(); closeEditor(); $("#profileMenu").classList.add("hidden"); closeSidebar(); }
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
    $("#settingsPHP").textContent = (system.phpVersions || []).map(item => `PHP ${item}`).join(", ") || "No runtime detected";
    $("#settingsDatabase").textContent = system.databaseEngine || "Database";
    $("#settingsFirewall").textContent = system.firewallBackend || "Firewall";
    $("#databaseEngine").textContent = system.databaseEngine || "Database";
    $("#firewallBackend").textContent = system.firewallBackend || "Firewall";
    $("#siteDatabaseLabel").textContent = `Create a ${system.databaseEngine || "database"} database`;
    $("#detailOS").textContent = system.os;
    $("#detailIP").textContent = system.ipAddress;
    $("#modalServerIP").textContent = system.ipAddress;
    $("#siteNavCount").textContent = currentState.sites.length;
    renderMetrics();
    renderServices();
    renderActivity();
    renderSites();
    renderBackups();
    renderFileSiteOptions();
    renderDatabases();
    renderFirewall();
    renderMail();
    window.LiteCPFeatures?.render(currentState);
    fillSettings(settings);
    const healthy = system.services.every(service => ["running", "disabled"].includes(service.state));
    $("#noticeBanner").querySelector("strong").textContent = healthy ? "Your hosting stack is healthy" : "One or more services need attention";
    $("#noticeBanner").querySelector("small").textContent = healthy ? "Required services are active and their lightweight health probes passed." : "Open Services to review and restart the affected component.";
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
      const unitLabel = service.key === "php" || service.key === "mail" ? service.unit : `${service.unit}.service`;
      return `<article class="service-card"><span class="service-card-icon">${meta.symbol}</span><div class="service-card-copy"><h3>${escapeHTML(service.label)}</h3><p>${escapeHTML(meta.description)}</p><span class="service-unit">${escapeHTML(unitLabel)}</span></div><div class="service-card-actions"><span class="state-pill ${stopped ? "stopped" : ""}"><span class="pulse-dot"></span>${escapeHTML(service.state)}</span><button class="button button-secondary" data-service="${escapeHTML(service.key)}" data-action="${stopped ? "start" : "restart"}">${stopped ? "Start" : "Restart"}</button></div></article>`;
    }).join("");
  }

  function renderActivity() {
    if (window.LiteCPFeatures) { window.LiteCPFeatures.activity(); return; }
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
      const copy = currentState.sites.length ? "No domains match your search." : "Provision WHMCS, WordPress or a regular PHP/HTML website with an isolated runtime in one short step.";
      $("#sitesTable").innerHTML = `<div class="empty-state"><span class="empty-icon">${icons.globe}</span><h3>${currentState.sites.length ? "Nothing found" : "No websites yet"}</h3><p>${copy}</p>${currentState.sites.length ? "" : '<button class="button button-primary" data-empty-add>Add your first website</button>'}</div>`;
      const emptyButton = $("[data-empty-add]", $("#sitesTable"));
      if (emptyButton) emptyButton.addEventListener("click", openSiteModal);
      return;
    }
    $("#sitesTable").innerHTML = `<table class="data-table"><thead><tr><th>DOMAIN</th><th>APPLICATION</th><th>RUNTIME</th><th>SSL</th><th>APP CRON</th><th>CREATED</th><th></th></tr></thead><tbody>${sites.map(site => `<tr><td><span class="domain-cell"><span class="domain-favicon">${escapeHTML(site.domain[0])}</span><span class="domain-copy"><strong><a class="website-link" href="${site.ssl ? "https" : "http"}://${escapeHTML(site.domain)}/" target="_blank" rel="noopener noreferrer" aria-label="Open ${escapeHTML(site.domain)} in a new tab">${escapeHTML(site.domain)}</a></strong><small>${escapeHTML(site.root)}</small></span></span></td><td><span class="badge">${escapeHTML(site.application?.enabled ? ({node:"Node.js",python:"Python",ruby:"Ruby"}[site.application.language]||site.application.language) : applicationLabel(site.appType))}${site.appVersion ? ` ${escapeHTML(site.appVersion)}` : ""}</span></td><td>${renderPHPSelect(site)}</td><td><span class="badge ${site.ssl ? "success" : ""}">${site.ssl ? '<span class="pulse-dot"></span>Protected' : "Not enabled"}</span></td><td><span class="badge ${site.cron ? "success" : ""}">${site.cron ? '<span class="pulse-dot"></span>Every 5 min' : site.appType === "regular" ? "Not applicable" : "Disabled"}</span></td><td>${formatDate(site.createdAt)}</td><td><span class="row-actions">${site.ssl ? "" : `<button aria-label="Enable SSL for ${escapeHTML(site.domain)}" title="Enable SSL" data-site-action="ssl" data-id="${escapeHTML(site.id)}">${icons.shield}</button>`}${site.appType === "regular" ? "" : `<button aria-label="${site.cron ? "Disable" : "Enable"} cron for ${escapeHTML(site.domain)}" title="${site.cron ? "Disable" : "Enable"} ${escapeHTML(applicationLabel(site.appType))} cron" data-site-action="cron" data-id="${escapeHTML(site.id)}">${icons.clock}</button>`}<button class="danger" aria-label="Detach ${escapeHTML(site.domain)}" title="Detach website" data-site-action="delete" data-id="${escapeHTML(site.id)}">${icons.trash}</button></span></td></tr>`).join("")}</tbody></table>`;
  }

  function renderPHPSelect(site) {
    if(site.application?.enabled)return `<span class="badge">${escapeHTML(site.application.language)} ${escapeHTML(site.application.version)}</span>`;
    const installed = currentState.system.phpVersions || [];
    const compatible = compatiblePHPVersions(site.appType || "whmcs", site.appVersion).filter(version => installed.includes(version));
    if (!compatible.includes(site.phpVersion)) compatible.unshift(site.phpVersion);
    const options = [...new Set(compatible)].map(version => `<option value="${escapeHTML(version)}" ${version === site.phpVersion ? "selected" : ""}>PHP ${escapeHTML(version)}</option>`).join("");
    return `<select class="inline-php-select" data-php-version data-id="${escapeHTML(site.id)}" aria-label="PHP version for ${escapeHTML(site.domain)}">${options}</select>`;
  }

  function renderBackups() {
    $("#retentionChip").textContent = `${currentState.settings.backupRetention}-day retention`;
    const siteValue = $("#backupSite").value;
    $("#backupSite").innerHTML = (currentState.sites || []).map(site => `<option value="${escapeHTML(site.id)}">${escapeHTML(site.domain)}</option>`).join("");
    if ((currentState.sites || []).some(site => site.id === siteValue)) $("#backupSite").value = siteValue;
    const destinationValue = $("#backupDestination").value;
    const destinations = currentState.backupDestinations || [];
    $("#backupDestination").innerHTML = '<option value="">Local storage only</option>' + destinations.map(destination => `<option value="${escapeHTML(destination.id)}">${escapeHTML(destination.name)} · ${escapeHTML(destination.protocol.toUpperCase())}</option>`).join("");
    if (destinations.some(item => item.id === destinationValue)) $("#backupDestination").value = destinationValue;
    $("#backupDestinationsTable").innerHTML = destinations.length ? `<table class="data-table"><thead><tr><th>NAME</th><th>PROTOCOL</th><th>SERVER</th><th>REMOTE PATH</th><th></th></tr></thead><tbody>${destinations.map(destination => `<tr><td><strong>${escapeHTML(destination.name)}</strong></td><td><span class="badge">${escapeHTML(destination.protocol.toUpperCase())}${destination.protocol === "ftp" ? " · Unencrypted" : ""}</span></td><td>${escapeHTML(destination.username)}@${escapeHTML(destination.host)}:${destination.port}</td><td><code>${escapeHTML(destination.remotePath)}</code></td><td><span class="row-actions"><button class="danger" data-backup-destination-action="delete" data-id="${escapeHTML(destination.id)}" title="Remove destination" aria-label="Remove backup destination ${escapeHTML(destination.name)}">${icons.trash}</button></span></td></tr>`).join("")}</tbody></table>` : '<div class="empty-state"><p>No remote backup destinations configured.</p></div>';
    const backups = currentState.backups || [];
    if (!backups.length) {
      $("#backupsTable").innerHTML = `<div class="empty-state"><span class="empty-icon">${icons.refresh}</span><h3>No backups yet</h3><p>Create a recovery point after your first website is provisioned.</p></div>`;
      return;
    }
    $("#backupsTable").innerHTML = `<table class="data-table"><thead><tr><th>WEBSITE</th><th>CREATED</th><th>SIZE</th><th>STATUS</th><th>LOCAL PATH</th><th>REMOTE</th></tr></thead><tbody>${backups.map(backup => `<tr><td><strong>${escapeHTML(backup.domain)}</strong></td><td>${formatDateTime(backup.createdAt)}</td><td>${formatBytes(backup.size)}</td><td><span class="badge success"><span class="pulse-dot"></span>${escapeHTML(backup.status)}</span></td><td><code>${escapeHTML(backup.path)}</code></td><td>${backup.destinationName ? `<strong>${escapeHTML(backup.destinationName)}</strong><br><small>${escapeHTML(backup.remotePath)}</small>` : "Local only"}</td></tr>`).join("")}</tbody></table>`;
  }

  function renderFileSiteOptions() {
    const select = $("#fileSiteSelect");
    const selected = select.value;
    select.innerHTML = (currentState.sites || []).map(site => `<option value="${escapeHTML(site.id)}">${escapeHTML(site.domain)}</option>`).join("");
    if ((currentState.sites || []).some(site => site.id === selected)) select.value = selected;
  }

  function renderDatabases() {
    const databases = currentState.databases || [];
    $("#databasesTable").innerHTML = databases.length ? `<table class="data-table"><thead><tr><th>DATABASE</th><th>USERNAME</th><th>WEBSITE</th><th>AUTH</th><th>CREATED</th><th></th></tr></thead><tbody>${databases.map(database => `<tr><td><span class="database-value"><button class="database-link" data-database-action="open" data-id="${escapeHTML(database.id)}" title="Open ${escapeHTML(database.name)} in phpMyAdmin">${escapeHTML(database.name)}</button><button class="database-copy" data-database-action="copy-name" data-id="${escapeHTML(database.id)}" aria-label="Copy database name ${escapeHTML(database.name)}" title="Copy database name">${icons.copy}</button></span></td><td><span class="database-value"><code>${escapeHTML(database.username)}</code><button class="database-copy" data-database-action="copy-user" data-id="${escapeHTML(database.id)}" aria-label="Copy username ${escapeHTML(database.username)}" title="Copy username">${icons.copy}</button></span></td><td>${escapeHTML(database.domain || "Standalone")}</td><td>${database.legacyAuth ? '<span class="badge">Legacy PHP</span>' : '<span class="badge success">Modern</span>'}</td><td>${formatDate(database.createdAt)}</td><td><span class="row-actions"><button class="text-action" data-database-action="password" data-id="${escapeHTML(database.id)}" title="Reset password">Password</button><button class="danger" data-database-action="delete" data-id="${escapeHTML(database.id)}" title="Delete database" aria-label="Delete database ${escapeHTML(database.name)}">${icons.trash}</button></span></td></tr>`).join("")}</tbody></table>` : '<div class="empty-state"><p>No managed databases yet.</p></div>';
  }

  function renderFirewall() {
    const protectedRules = [
      { port: "22", protocol: "tcp", label: "SSH" }, { port: "80", protocol: "tcp", label: "HTTP" },
      { port: "443", protocol: "tcp", label: "HTTPS" }, { port: "8443", protocol: "tcp", label: "LiteCP" },
      { port: "25, 465, 587, 993", protocol: "tcp", label: "Email hosting" }
    ];
    const managed = currentState.firewallRules || [];
    const rows = protectedRules.map(rule => `<tr><td><strong>${rule.port}</strong></td><td>${rule.protocol.toUpperCase()}</td><td>any</td><td>${rule.label}</td><td><span class="badge success">Protected</span></td></tr>`).join("") + managed.map(rule => `<tr><td><strong>${rule.port}</strong></td><td>${escapeHTML(rule.protocol.toUpperCase())}</td><td><code>${escapeHTML(rule.source)}</code></td><td>${escapeHTML(rule.label || "Custom rule")}</td><td><span class="row-actions"><button class="danger" data-firewall-action="delete" data-id="${escapeHTML(rule.id)}" aria-label="Remove firewall rule ${rule.port}/${escapeHTML(rule.protocol)}">${icons.trash}</button></span></td></tr>`).join("");
    $("#firewallTable").innerHTML = `<table class="data-table"><thead><tr><th>PORT</th><th>PROTOCOL</th><th>SOURCE</th><th>LABEL</th><th></th></tr></thead><tbody>${rows}</tbody></table>`;
  }

  function renderMail() {
    const domains = currentState.mailDomains || [];
    const mailboxes = currentState.mailboxes || [];
    const aliases = currentState.mailAliases || [];
    const selected = $("#mailboxDomain").value;
    const websiteDomains = [...new Set((currentState.sites || []).map(site => site.domain))];
    const availableDomains = [...new Set([...domains.map(item => item.domain), ...websiteDomains])].sort();
    $("#mailboxDomain").innerHTML = availableDomains.length ? availableDomains.map(domain => `<option value="${escapeHTML(domain)}">${escapeHTML(domain)}${domains.some(item => item.domain === domain) ? "" : " — website"}</option>`).join("") : '<option value="">Add a website or mail domain first</option>';
    if (availableDomains.includes(selected)) $("#mailboxDomain").value = selected;
    $("#mailDomainsTable").innerHTML = domains.length ? `<table class="data-table"><thead><tr><th>DOMAIN</th><th>MAILBOXES</th><th>ALIASES</th><th>CREATED</th><th></th></tr></thead><tbody>${domains.map(domain => `<tr><td><strong>${escapeHTML(domain.domain)}</strong></td><td>${mailboxes.filter(item => item.domain === domain.domain).length}</td><td>${aliases.filter(item => item.source.endsWith("@" + domain.domain)).length}</td><td>${formatDate(domain.createdAt)}</td><td><span class="row-actions"><button class="danger" data-mail-action="domain-delete" data-id="${escapeHTML(domain.id)}" aria-label="Remove mail domain ${escapeHTML(domain.domain)}">${icons.trash}</button></span></td></tr>`).join("")}</tbody></table>` : '<div class="empty-state"><p>No mail domains configured.</p></div>';
    $("#mailboxesTable").innerHTML = mailboxes.length ? `<table class="data-table"><thead><tr><th>ADDRESS</th><th>QUOTA</th><th>CREATED</th><th></th></tr></thead><tbody>${mailboxes.map(mailbox => `<tr><td><strong>${escapeHTML(mailbox.address)}</strong></td><td>${formatBytes(mailbox.quotaMb * 1024 * 1024)}</td><td>${formatDate(mailbox.createdAt)}</td><td><span class="row-actions"><button class="text-action" data-mail-action="mailbox-password" data-id="${escapeHTML(mailbox.id)}">Password</button><button class="danger" data-mail-action="mailbox-delete" data-id="${escapeHTML(mailbox.id)}" aria-label="Disable mailbox ${escapeHTML(mailbox.address)}">${icons.trash}</button></span></td></tr>`).join("")}</tbody></table>` : '<div class="empty-state"><p>No mailboxes created.</p></div>';
    $("#mailAliasesTable").innerHTML = aliases.length ? `<table class="data-table"><thead><tr><th>SOURCE</th><th>DESTINATION</th><th>CREATED</th><th></th></tr></thead><tbody>${aliases.map(alias => `<tr><td><strong>${escapeHTML(alias.source)}</strong></td><td>${escapeHTML(alias.destination)}</td><td>${formatDate(alias.createdAt)}</td><td><span class="row-actions"><button class="danger" data-mail-action="alias-delete" data-id="${escapeHTML(alias.id)}" aria-label="Remove alias ${escapeHTML(alias.source)}">${icons.trash}</button></span></td></tr>`).join("")}</tbody></table>` : '<div class="empty-state"><p>No forwarding aliases configured.</p></div>';
  }

  function fillSettings(settings) {
    $("#settingsEmail").value = settings.email || "";
    $("#settingsTimezone").value = settings.timezone || "UTC";
    $("#settingsRetention").value = settings.backupRetention || 14;
  }

  function navigate(page, updateHash = true) {
    const settingsPart=String(page).startsWith("settings/")?String(page).slice(9):"";
    const settingsRoute=["password","email","timezone","backups","hostname","dns","reboot"].includes(settingsPart)?`settings/${settingsPart}`:"";
    if(settingsRoute)page="settings";
    const allowed = ["dashboard", "websites", "files", "databases", "email", "services", "backups", "firewall", "logs", "settings", "migrations", "extensions", "security", "activities", "documentation", "applications"];
    if (!allowed.includes(page)) page = "dashboard";
    $$("[data-page-view]").forEach(view => view.classList.toggle("active", view.dataset.pageView === page));
    $$("[data-page]").forEach(link => {
      const active = link.dataset.page === page;
      link.classList.toggle("active", active);
      if (active) link.setAttribute("aria-current", "page"); else link.removeAttribute("aria-current");
    });
    const route=settingsRoute||page;
    if (updateHash && location.hash !== `#${route}`) history.pushState(null, "", `#${route}`);
    closeSidebar();
    window.scrollTo({ top: 0, behavior: "smooth" });
    clearInterval(logTimer);
    if (page === "logs") {
      loadLogs();
      logTimer = setInterval(loadLogs, 30000);
    }
    if (page === "files") loadFiles();
    window.LiteCPFeatures?.navigate(page);
  }

  function openSiteModal() {
	configureSiteForm();
    $("#siteModal").classList.remove("hidden");
    setTimeout(() => $('#siteForm input[name="domain"]').focus(), 50);
  }

  function closeSiteModal() { $("#siteModal").classList.add("hidden"); }

  function configureSiteForm() {
    const appType = $("#siteAppType").value || "whmcs";
    const appVersion = appType === "whmcs" ? $("#siteAppVersion").value : "";
    const installed = currentState?.system?.phpVersions || [];
    const compatible = compatiblePHPVersions(appType, appVersion);
    const preferred = appType === "wordpress" || appType === "regular" ? "8.4" : "8.3";
    $("#sitePHPVersion").innerHTML = ["7.3", "7.4", "8.1", "8.2", "8.3", "8.4"].map(version => {
      const available = installed.includes(version);
      const allowed = compatible.includes(version);
      const note = !available ? " — unavailable on this server" : !allowed ? ` — not supported by ${applicationLabel(appType)}` : "";
      return `<option value="${version}" ${version === preferred ? "selected" : ""} ${available && allowed ? "" : "disabled"}>PHP ${version}${note}</option>`;
    }).join("");
    if (!installed.includes(preferred)) {
      const fallback = compatible.find(version => installed.includes(version));
      if (fallback) $("#sitePHPVersion").value = fallback;
    }
    $("#whmcsVersionField").classList.toggle("hidden", appType !== "whmcs");
    const cronInput = $('#cronOptionCard input[name="enableCron"]');
    const cronWasDisabled = cronInput.disabled;
    $("#cronOptionCard").classList.toggle("hidden", appType === "regular");
    cronInput.disabled = appType === "regular";
    if (appType === "regular") cronInput.checked = false;
    else if (cronWasDisabled) cronInput.checked = true;
    if (appType === "wordpress") {
      $("#phpCompatibilityNote").textContent = "WordPress recommends PHP 8.3 or newer and requires PHP 7.4 or newer.";
      $("#cronOptionTitle").textContent = "Enable WordPress system cron";
      $("#cronOptionHelp").innerHTML = "Runs <code>wp-cron.php</code> every five minutes with the selected PHP version.";
    } else if (appType === "whmcs") {
      $("#phpCompatibilityNote").textContent = appVersion === "9.0" ? "WHMCS 9.0 requires PHP 8.2 or 8.3." : "WHMCS 8.13 LTS supports PHP 7.3 through 8.3; legacy PHP versions are end-of-life.";
      $("#cronOptionTitle").textContent = "Enable WHMCS automation cron";
      $("#cronOptionHelp").innerHTML = "Runs <code>crons/cron.php</code> every five minutes with the selected PHP version.";
    } else {
      $("#phpCompatibilityNote").textContent = "Regular PHP/HTML websites can use any installed runtime; PHP 8.3 or newer is recommended.";
    }
  }

  async function createSite(event) {
    event.preventDefault();
    const form = event.currentTarget;
    const submit = $('button[type="submit"]', form);
    const data = new FormData(form);
    const body = { domain: String(data.get("domain")).trim().toLowerCase(), appType: String(data.get("appType")), appVersion: String(data.get("appVersion") || ""), phpVersion: String(data.get("phpVersion")), createDatabase: data.get("createDatabase") === "on", enableCron: data.get("enableCron") === "on" };
    submit.disabled = true;
    submit.textContent = "Provisioning…";
    try {
      const result = await api("/api/sites", { method: "POST", body });
      closeSiteModal();
      form.reset();
      currentState = await api("/api/state");
      renderAll();
      if (result.database) showCredentials(result.database, result.site);
      else showToast("Website provisioned", `${result.site.domain} is ready for ${applicationLabel(result.site.appType)} files.`);
    } catch (error) { handleError(error); }
    finally { submit.disabled = false; submit.innerHTML = 'Provision website <svg viewBox="0 0 24 24"><path d="m9 18 6-6-6-6"/></svg>'; }
  }

  function showCredentials(credentials, site) {
    const rows = [["Website", site.domain], ["Database host", credentials.host], ["Database name", credentials.name], ["Database user", credentials.username], ["Database password", credentials.password]];
    showOneTimeCredentials("Website provisioned", "Save these database credentials now. The password is not stored by LiteCP.", rows);
  }

  function showOneTimeCredentials(title, description, rows) {
    credentialText = rows.map(([label, value]) => `${label}: ${value}`).join("\n");
    $("#credentialsTitle").textContent = title;
    $("#credentialsModal .modal-head p").textContent = description;
    $("#credentialList").innerHTML = rows.map(([label, value]) => `<div><dt>${escapeHTML(label)}</dt><dd>${escapeHTML(value)}</dd></div>`).join("");
    $$("#credentialList > div").forEach((row, index) => {
      if (/password/i.test(rows[index][0])) {
        const input=document.createElement("input");input.type="password";input.value=String(rows[index][1]);input.readOnly=true;input.autocomplete="off";input.setAttribute("aria-label",rows[index][0]);
        row.querySelector("dd").replaceChildren(input);window.LiteCPPasswordCopy.enhance(input);
      }
    });
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
    if (button.dataset.siteAction === "files") { $("#fileSiteSelect").value = site.id; currentFilePath = ""; navigate("files"); return; }
    button.disabled = true;
    try {
      if (button.dataset.siteAction === "ssl") {
        const email = await window.LiteCPDialogs.prompt(`Enable HTTPS for ${site.domain}. Enter an email address for Let's Encrypt certificate renewal notices. The domain must point to this server.`, currentState.settings.email || "", { title: "Enable SSL certificate", label: "Renewal email address", type: "email", required: true, confirmText: "Enable SSL", placeholder: "you@example.com" });
        if (!email) return;
        await api("/api/sites/ssl", { method: "POST", body: { id: site.id, email } });
        showToast("SSL enabled", `${site.domain} now redirects to HTTPS.`);
      } else if (button.dataset.siteAction === "cron") {
        await api("/api/sites/cron", { method: "POST", body: { id: site.id, enabled: !site.cron } });
        showToast("Schedule updated", `${applicationLabel(site.appType)} cron ${site.cron ? "disabled" : "enabled"} for ${site.domain}.`);
      } else if (button.dataset.siteAction === "delete") {
        const accepted = await window.LiteCPDialogs.confirm(`Remove hosting configuration for ${site.domain}?\n\nIts files and database will be preserved. The website will stop being served through LiteCP.`, { title: "Detach website", confirmText: "Detach website", danger: true });
        if (!accepted) return;
        await api(`/api/sites/${encodeURIComponent(site.id)}`, { method: "DELETE" });
        showToast("Website detached", "Nginx and cron configuration removed; data was preserved.");
      }
      currentState = await api("/api/state");
      renderAll();
    } catch (error) { handleError(error); }
    finally { button.disabled = false; }
  }

  async function handlePHPChange(event) {
    const select = event.target.closest("[data-php-version]");
    if (!select) return;
    const site = currentState.sites.find(item => item.id === select.dataset.id);
    if (!site || site.phpVersion === select.value) return;
    const version = select.value;
    select.disabled = true;
    try {
      let databasePassword = "";
      const database = currentState.databases.find(item => item.siteId === site.id);
      if (["7.3", "7.4"].includes(version) && database && !database.legacyAuth) {
        databasePassword = await window.LiteCPFeatures.askPassword(`Current database password for ${database.username}`, false) || "";
        if (!databasePassword) {
          renderSites();
          return;
        }
      }
      await api("/api/sites/php", { method: "POST", body: { id: site.id, version, databasePassword } });
      showToast("PHP version changed", `${site.domain} now uses PHP ${version}.`);
      currentState = await api("/api/state");
      renderAll();
    } catch (error) {
      renderSites();
      handleError(error);
    }
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
    if (!await window.LiteCPDialogs.confirm(`Restart Nginx, PHP-FPM, ${currentState.system.databaseEngine || "database"}, mail and cron in sequence?`)) return;
    button.disabled = true;
    const original = button.innerHTML;
    button.textContent = "Restarting stack…";
    try {
      for (const service of ["php", "database", ...(currentState.settings.emailHostingEnabled === false ? [] : ["mail"]), "cron", "nginx"]) await api("/api/services/action", { method: "POST", body: { service, action: "restart" } });
      showToast("Stack restarted", "All essential services restarted successfully.");
      currentState = await api("/api/state");
      renderAll();
    } catch (error) { handleError(error); }
    finally { button.disabled = false; button.innerHTML = original; }
  }

  async function runBackup(event) {
    if (event?.preventDefault) event.preventDefault();
    if (!currentState.sites.length) { showToast("No website available", "Add a website before creating a backup.", "error"); return; }
    const quick = !event || event.currentTarget?.hasAttribute?.("data-quick-backup");
    const siteId = quick ? currentState.sites[0].id : $("#backupSite").value;
    const destinationId = quick ? "" : $("#backupDestination").value;
    const site = currentState.sites.find(item => item.id === siteId);
    if (!site) return;
    const button = quick ? null : $("#backupButton");
    if (button) { button.disabled = true; button.textContent = "Creating backup…"; }
    showToast("Backup started", `Archiving ${site.domain}. This may take a moment.`);
    try {
      await api("/api/backups", { method: "POST", body: { siteId: site.id, destinationId } });
      currentState = await api("/api/state");
      renderAll();
      showToast("Backup completed", destinationId ? `${site.domain} was saved locally and uploaded off-server.` : `${site.domain} files and database were saved locally.`);
    } catch (error) { handleError(error); }
    finally { if (button) { button.disabled = false; button.textContent = "Create backup"; } }
  }

  function configureBackupProtocol() {
    const protocol = $("#backupProtocol").value;
    const port = $('#backupDestinationForm input[name="port"]');
    port.value = protocol === "sftp" ? 22 : protocol === "ftps" ? 990 : 21;
    $("#hostKeyField").classList.toggle("hidden", protocol !== "sftp");
    $("#hostKeyField input").required = protocol === "sftp";
  }

  async function createBackupDestination(event) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    const submit = $('button[type="submit"]', form);
    submit.disabled = true;
    try {
      await api("/api/backup-destinations", { method: "POST", body: {
        name: String(data.get("name")).trim(), protocol: data.get("protocol"), host: String(data.get("host")).trim(),
        port: Number(data.get("port")), username: String(data.get("username")).trim(), password: data.get("password"),
        remotePath: String(data.get("remotePath")).trim(), hostKeySha256: String(data.get("hostKeySha256") || "").trim()
      } });
      form.reset(); configureBackupProtocol();
      currentState = await api("/api/state"); renderAll();
      showToast("Destination saved", "The remote credential was encrypted and stored.");
    } catch (error) { handleError(error); }
    finally { submit.disabled = false; }
  }

  async function handleBackupDestinationAction(event) {
    const button = event.target.closest("[data-backup-destination-action]");
    if (!button || !await window.LiteCPDialogs.confirm("Remove this remote destination? Existing backups will not be deleted.")) return;
    try {
      await api(`/api/backup-destinations/${encodeURIComponent(button.dataset.id)}`, { method: "DELETE" });
      currentState = await api("/api/state"); renderAll();
      showToast("Destination removed", "Stored remote credentials were removed from LiteCP.");
    } catch (error) { handleError(error); }
  }

  async function loadFiles() {
    const request = ++fileListingRequest;
    const siteId = $("#fileSiteSelect").value;
    if (!siteId) { $("#fileTable").innerHTML = '<div class="empty-state"><p>Add a website before using the file manager.</p></div>'; return; }
    window.LiteCPFileManager?.loading();
    $("#fileTable").innerHTML = '<div class="loading-lines"><i></i><i></i><i></i></div>';
    try {
      const listing = await api(`/api/files?siteId=${encodeURIComponent(siteId)}&path=${encodeURIComponent(currentFilePath)}`);
      if (request !== fileListingRequest || siteId !== $("#fileSiteSelect").value) return;
      currentFilePath = listing.path || "";
      lastFileListing = listing;
      window.LiteCPFeatures?.rememberListing(listing);
      renderFileListing(listing);
    } catch (error) {
      if (request !== fileListingRequest) return;
      if (window.LiteCPFileManager && lastFileListing?.siteId === siteId) {
        currentFilePath = lastFileListing.path || "";
        renderFileListing(lastFileListing); handleError(error);
      } else $("#fileTable").innerHTML = `<div class="empty-state"><p>${escapeHTML(error.message)}</p></div>`;
    }
  }

  function renderFileListing(listing) {
    if (window.LiteCPFileManager) { window.LiteCPFileManager.render(listing); return; }
    const parts = currentFilePath ? currentFilePath.split("/") : [];
    $("#fileBreadcrumb").innerHTML = '<button data-file-path="">public</button>' + parts.map((part, index) => `<button data-file-path="${escapeHTML(parts.slice(0, index + 1).join("/"))}">${escapeHTML(part)}</button>`).join("");
    $("#fileUpButton").disabled = !currentFilePath;
    const entries = window.LiteCPFeatures ? window.LiteCPFeatures.filterFiles(listing.entries || []) : listing.entries || [];
    if (!entries.length) { $("#fileTable").innerHTML = '<div class="empty-state"><p>This directory is empty.</p></div>'; return; }
    $("#fileTable").innerHTML = `<table class="data-table"><thead><tr><th><input class="file-check" type="checkbox" data-file-select-all></th><th>NAME</th><th>SIZE</th><th>PERMISSIONS</th><th>MODIFIED</th><th></th></tr></thead><tbody>${entries.map(entry => {
      const isDirectory = entry.type === "directory";
      const canEdit = entry.type === "file";
      const isZip = canEdit && entry.name.toLowerCase().endsWith(".zip");
      return `<tr><td><input class="file-check" type="checkbox" data-file-select value="${escapeHTML(entry.path)}" aria-label="Select ${escapeHTML(entry.name)}"></td><td><span class="file-entry"><span class="file-type-icon">${isDirectory ? "DIR" : isZip ? "ZIP" : "FILE"}</span><button class="file-name-button ${isDirectory ? "directory" : ""}" data-file-action="${isDirectory ? "open" : "edit"}" data-path="${escapeHTML(entry.path)}">${escapeHTML(entry.name)}</button></span></td><td>${isDirectory ? "—" : formatBytes(entry.size)}</td><td><code>${escapeHTML(entry.mode)}</code></td><td>${formatDateTime(entry.modified)}</td><td><span class="row-actions">${isZip ? `<button class="text-action" data-file-action="extract" data-path="${escapeHTML(entry.path)}">Extract</button>` : ""}${canEdit ? `<button class="text-action" data-file-action="download" data-path="${escapeHTML(entry.path)}">Download</button>` : ""}<button class="text-action" data-file-action="rename" data-path="${escapeHTML(entry.path)}">Rename</button><button class="text-action" data-file-action="chmod" data-path="${escapeHTML(entry.path)}" data-mode="${escapeHTML(entry.mode)}">Mode</button><button class="danger" data-file-action="delete" data-path="${escapeHTML(entry.path)}" aria-label="Delete ${escapeHTML(entry.name)}">${icons.trash}</button></span></td></tr>`;
    }).join("")}</tbody></table>`;
    const selectAll = $("[data-file-select-all]", $("#fileTable"));
    selectAll.addEventListener("change", () => $$('[data-file-select]', $("#fileTable")).forEach(item => { item.checked = selectAll.checked; }));
  }

  function fileUp() {
    const parts = currentFilePath.split("/").filter(Boolean); parts.pop(); currentFilePath = parts.join("/"); loadFiles();
  }

  async function createFolder() {
    const name = await window.LiteCPDialogs.prompt("New folder name:", "new-folder");
    if (!name) return;
    try { await api("/api/files/directory", { method: "POST", body: { siteId: $("#fileSiteSelect").value, path: currentFilePath, name } }); await loadFiles(); showToast("Folder created", name); }
    catch (error) { handleError(error); }
  }

  async function uploadFiles(event) {
    const files = [...event.target.files];
    if (!files.length) return;
    const body = new FormData();
    body.append("siteId", $("#fileSiteSelect").value); body.append("path", currentFilePath);
    files.forEach(file => body.append("files", file, file.name));
    showToast("Upload started", `${files.length} file(s) are being transferred.`);
    try { await api("/api/files/upload", { method: "POST", body }); await loadFiles(); showToast("Upload completed", `${files.length} file(s) uploaded.`); }
    catch (error) { handleError(error); }
    finally { event.target.value = ""; }
  }

  async function archiveSelectedFiles() {
    const paths = $$('[data-file-select]:checked', $("#fileTable")).map(input => input.value);
    if (!paths.length) { showToast("Nothing selected", "Select files or folders to add to a ZIP archive.", "error"); return; }
    const name = await window.LiteCPDialogs.prompt("Archive name:", "archive.zip");
    if (!name) return;
    const destination = currentFilePath ? `${currentFilePath}/${name}` : name;
    try { await api("/api/files/archive", { method: "POST", body: { siteId: $("#fileSiteSelect").value, paths, destination } }); await loadFiles(); showToast("ZIP created", destination); }
    catch (error) { handleError(error); }
  }

  async function handleFileAction(event) {
    const button = event.target.closest("[data-file-action]");
    if (!button) return;
    const action = button.dataset.fileAction;
    const path = button.dataset.path;
    if (action === "open") { currentFilePath = path; loadFiles(); return; }
    if (action === "download") { if (demoMode) showToast("Preview", "Downloads are disabled in demo mode."); else location.href = `/api/files/download?siteId=${encodeURIComponent($("#fileSiteSelect").value)}&path=${encodeURIComponent(path)}`; return; }
    if (action === "edit") { openEditor(path); return; }
    try {
      if (action === "extract") {
        if (!await window.LiteCPDialogs.confirm(`Extract ${path} into the current directory? Existing files with matching names will be overwritten.`)) return;
        await api("/api/files/extract", { method: "POST", body: { siteId: $("#fileSiteSelect").value, path, destination: currentFilePath } });
      } else if (action === "rename") {
        const oldName = path.split("/").pop(); const next = await window.LiteCPDialogs.prompt("New name:", oldName); if (!next || next === oldName) return;
        const parent = path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "";
        await api("/api/files/rename", { method: "POST", body: { siteId: $("#fileSiteSelect").value, path, destination: parent ? `${parent}/${next}` : next } });
      } else if (action === "chmod") {
        const mode = await window.LiteCPDialogs.prompt("Octal permissions (world-writable and special modes are blocked):", button.dataset.mode); if (!mode) return;
        await api("/api/files/permissions", { method: "PUT", body: { siteId: $("#fileSiteSelect").value, path, mode } });
      } else if (action === "delete") {
        if (!await window.LiteCPDialogs.confirm(`Move ${path} to Trash? You can restore it from the Trash view.`)) return;
        await api("/api/files", { method: "DELETE", body: { siteId: $("#fileSiteSelect").value, path } });
      }
      await loadFiles(); showToast("File operation completed", path);
    } catch (error) { handleError(error); }
  }

  async function openEditor(path) {
    if (editorSaving) return;
    const request = ++editorRequest, siteId = $("#fileSiteSelect").value;
    try {
      const result = await api(`/api/files/content?siteId=${encodeURIComponent(siteId)}&path=${encodeURIComponent(path)}`);
      if (request !== editorRequest || siteId !== $("#fileSiteSelect").value) return;
      editorSiteID = siteId;
      editorFilePath = path; editorOriginal = result.content; $("#editorTitle").textContent = `Edit ${path.split("/").pop()}`; $("#editorPath").textContent = path; $("#editorContent").value = result.content; $("#editorModal").classList.remove("hidden");
      await window.LiteCPFeatures?.openEditor(path, result.content);
    } catch (error) { if (request === editorRequest) handleError(error); }
  }

  async function closeEditor() {
    if (editorSaving) return;
    const content = window.LiteCPFeatures?.editorValue() ?? $("#editorContent").value;
    if (editorFilePath && content !== editorOriginal && !await window.LiteCPDialogs.confirm("Discard unsaved editor changes?")) return;
    editorRequest++; editorFilePath = ""; editorSiteID = ""; $("#editorModal").classList.add("hidden");
  }

  async function saveEditor(event) {
    event.preventDefault(); if (!editorFilePath || !editorSiteID || editorSaving) return;
    editorSaving = true;
    const submit = $('button[type="submit"]', event.currentTarget); submit.disabled = true;
    try { const content = window.LiteCPFeatures?.editorValue() ?? $("#editorContent").value; await api("/api/files/content", { method: "PUT", body: { siteId: editorSiteID, path: editorFilePath, content } }); editorOriginal = content; await loadFiles(); showToast("File saved", "Changes are live on the website."); }
    catch (error) { handleError(error); }
    finally { editorSaving = false; submit.disabled = false; }
  }

  async function createDatabase(event) {
    event.preventDefault(); const form = event.currentTarget; const data = new FormData(form); const submit = $('button[type="submit"]', form); submit.disabled = true; const inlineError=form.querySelector('[data-database-form-error]'); if(inlineError)inlineError.textContent="";
    try {
      const result = await api("/api/databases", { method: "POST", body: { name: String(data.get("name")).trim(), username: String(data.get("username")).trim(), password: data.get("password") } });
      form.reset(); currentState = await api("/api/state"); renderAll();
      form.dispatchEvent(new Event("database-created"));
      showOneTimeCredentials("Database created", "Save this password now. LiteCP does not store database passwords.", [["Host", result.host], ["Database", result.database.name], ["Username", result.database.username], ["Password", result.password]]);
    } catch (error) { if(inlineError)inlineError.textContent=error.message; handleError(error); }
    finally { submit.disabled = false; }
  }

  async function handleDatabaseAction(event) {
    const button = event.target.closest("[data-database-action]"); if (!button) return;
    const database = (currentState.databases || []).find(item => item.id === button.dataset.id); if (!database) return;
    try {
      if (button.dataset.databaseAction === "copy-name" || button.dataset.databaseAction === "copy-user") {
        await navigator.clipboard.writeText(button.dataset.databaseAction === "copy-name" ? database.name : database.username);
        showToast("Copied", button.dataset.databaseAction === "copy-name" ? "Database name copied." : "Database username copied.");
        return;
      }
      if (button.dataset.databaseAction === "open") {
        if (demoMode) { showToast("Server required", "phpMyAdmin is available on the installed server."); return; }
        const target = "litecp_pma_" + Date.now();
        const tab = window.open("about:blank", target);
        if (!tab) { showToast("Tab blocked", "Allow new tabs for LiteCP, then click the database again.", "error"); return; }
        try {
          const result = await api("/api/databases/phpmyadmin", { method: "POST", body: { id: database.id } });
          const form = document.createElement("form"); form.method = "POST"; form.action = "/api/phpmyadmin/sign-on.php"; form.target = target;
          const ticket = document.createElement("input"); ticket.type = "hidden"; ticket.name = "ticket"; ticket.value = result.ticket; form.append(ticket);
          document.body.append(form); form.submit(); form.remove(); tab.opener = null; tab.focus();
        } catch (error) { tab.close(); throw error; }
        return;
      }
      if (button.dataset.databaseAction === "delete") {
        if (!await window.LiteCPDialogs.confirm(`Permanently delete database ${database.name} and user ${database.username}? This cannot be undone.`)) return;
        await api(`/api/databases/${encodeURIComponent(database.id)}`, { method: "DELETE" });
      } else {
        const password = await window.LiteCPFeatures.askPassword(`New password for ${database.username}`, true); if (password === null) return;
        const result = await api("/api/databases/password", { method: "PUT", body: { id: database.id, password } });
        showOneTimeCredentials("Database password reset", "Save the new password and update the application configuration.", [["Database", database.name], ["Username", database.username], ["Password", result.password]]);
      }
      currentState = await api("/api/state"); renderAll();
    } catch (error) { handleError(error); }
  }

  async function createFirewallRule(event) {
    if (window.LiteCPFeatures) { await window.LiteCPFeatures.saveFirewall(event); return; }
    event.preventDefault(); const form = event.currentTarget; const data = new FormData(form); const submit = $('button[type="submit"]', form); submit.disabled = true;
    try { await api("/api/firewall", { method: "POST", body: { port: Number(data.get("port")), protocol: data.get("protocol"), source: String(data.get("source")).trim(), label: String(data.get("label")).trim() } }); form.reset(); currentState = await api("/api/state"); renderAll(); showToast("Firewall rule added", "The permanent firewall configuration was reloaded."); }
    catch (error) { handleError(error); }
    finally { submit.disabled = false; }
  }

  async function handleFirewallAction(event) {
    if (window.LiteCPFeatures) { await window.LiteCPFeatures.firewallAction(event); return; }
    const button = event.target.closest("[data-firewall-action]"); if (!button || !await window.LiteCPDialogs.confirm("Remove this custom firewall rule?")) return;
    try { await api(`/api/firewall/${encodeURIComponent(button.dataset.id)}`, { method: "DELETE" }); currentState = await api("/api/state"); renderAll(); showToast("Firewall rule removed", "The permanent firewall configuration was reloaded."); }
    catch (error) { handleError(error); }
  }

  async function createMailDomain(event) {
    event.preventDefault(); const form = event.currentTarget; const data = new FormData(form);
    try { await api("/api/mail/domains", { method: "POST", body: { domain: String(data.get("domain")).trim().toLowerCase() } }); form.reset(); currentState = await api("/api/state"); renderAll(); showToast("Mail domain added", "Add mailboxes after the required DNS records are configured."); }
    catch (error) { handleError(error); }
  }

  async function createMailbox(event) {
    event.preventDefault(); const form = event.currentTarget; const data = new FormData(form);
    try {
      const domain = String(data.get("domain")).trim().toLowerCase();
      await api("/api/mail/mailboxes", { method: "POST", body: { domain, localPart: String(data.get("localPart")).trim().toLowerCase(), password: data.get("password"), quotaMb: Number(data.get("quotaMb")) } }); form.reset(); currentState = await api("/api/state"); renderAll(); showToast("Mailbox created", "The address is ready for authenticated SMTP and IMAP."); }
    catch (error) { handleError(error); }
  }

  async function createMailAlias(event) {
    event.preventDefault(); const form = event.currentTarget; const data = new FormData(form);
    try { await api("/api/mail/aliases", { method: "POST", body: { source: String(data.get("source")).trim().toLowerCase(), destination: String(data.get("destination")).trim() } }); form.reset(); currentState = await api("/api/state"); renderAll(); showToast("Alias created", "Incoming messages will be forwarded to the destination."); }
    catch (error) { handleError(error); }
  }

  async function handleMailAction(event) {
    const button = event.target.closest("[data-mail-action]"); if (!button) return;
    const action = button.dataset.mailAction;
    try {
      if (action === "mailbox-password") {
        const password = await window.LiteCPFeatures.askPassword("New mailbox password", true); if (!password) return;
        await api("/api/mail/mailboxes/password", { method: "PUT", body: { id: button.dataset.id, password } }); showToast("Mailbox password reset", "Update the email client with the new password.");
      } else {
        const warning = action === "domain-delete" ? "Removing a mail domain disables all of its mailboxes and aliases. Stored Maildir messages are kept. Continue?" : action === "mailbox-delete" ? "Disable this mailbox login? Stored Maildir messages are kept." : "Remove this forwarding alias?";
        if (!await window.LiteCPDialogs.confirm(warning)) return;
        const endpoint = action === "domain-delete" ? "domains" : action === "mailbox-delete" ? "mailboxes" : "aliases";
        await api(`/api/mail/${endpoint}/${encodeURIComponent(button.dataset.id)}`, { method: "DELETE" });
      }
      currentState = await api("/api/state"); renderAll();
    } catch (error) { handleError(error); }
  }

  async function saveSettings(event) {
    event.preventDefault();
    const submit = $('button[type="submit"]', event.currentTarget);
    submit.disabled = true;
    try {
      const saved=currentState.settings, part=event.currentTarget.dataset.settingsPart;
      const body = {email:saved.email||"",timezone:saved.timezone||"UTC",backupRetention:saved.backupRetention||14};
      if(part==="email")body.email=$("#settingsEmail").value.trim();
      else if(part==="timezone")body.timezone=$("#settingsTimezone").value;
      else if(part==="backups")body.backupRetention=Number($("#settingsRetention").value);
      else throw new Error("Choose a settings section before saving.");
      await api("/api/settings", { method: "PUT", body });
      currentState.settings = { ...currentState.settings, ...body };
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
      currentState = null;
      showLogin();
      showToast("Password updated", "All administrator sessions were signed out. Sign in with the new password.");
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

  function compatiblePHPVersions(appType, appVersion = "") {
    if (appType === "wordpress") return ["7.4", "8.1", "8.2", "8.3", "8.4"];
    if (appType === "regular") return ["7.3", "7.4", "8.1", "8.2", "8.3", "8.4"];
    return appVersion === "9.0" ? ["8.2", "8.3"] : ["7.3", "7.4", "8.1", "8.2", "8.3"];
  }

  function applicationLabel(appType) { return appType === "wordpress" ? "WordPress" : appType === "regular" ? "Regular Website" : "WHMCS"; }

  function escapeHTML(value) { return String(value ?? "").replace(/[&<>'"]/g, character => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" })[character]); }
  function titleCase(value) { return String(value || "admin").replace(/(^|[-_\s])\w/g, match => match.toUpperCase()).replace(/[-_]/g, " "); }
  function formatBytes(bytes) { const value = Number(bytes) || 0; if (!value) return "0 B"; const units = ["B", "KB", "MB", "GB", "TB"]; const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1); return `${(value / 1024 ** index).toFixed(index > 2 ? 1 : 0)} ${units[index]}`; }
  function formatUptime(seconds) { const days = Math.floor(seconds / 86400); const hours = Math.floor((seconds % 86400) / 3600); return days ? `${days}d ${hours}h` : `${hours}h ${Math.floor((seconds % 3600) / 60)}m`; }
  function displayTimezone() { return currentState?.settings?.timezone || "UTC"; }
  function formatDate(value) { return new Intl.DateTimeFormat(undefined, { timeZone: displayTimezone(), month: "short", day: "numeric", year: "numeric" }).format(new Date(value)); }
  function formatDateTime(value) { return new Intl.DateTimeFormat(undefined, { timeZone: displayTimezone(), month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", timeZoneName: "short" }).format(new Date(value)); }
  function relativeTime(value) { const seconds = Math.max(1, Math.floor((Date.now() - new Date(value).getTime()) / 1000)); if (seconds < 60) return "just now"; if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`; if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`; return `${Math.floor(seconds / 86400)}d ago`; }

  window.LiteCP = { api, escapeHTML, showToast, handleError, navigate, renderAll, loadFiles, renderFileListing, openEditor,
    goToFilePath: path => { currentFilePath = path; return loadFiles(); },
    state: () => currentState, path: () => currentFilePath, demo: demoMode,
    refresh: async () => { currentState = await api("/api/state"); renderAll(); },
    showOneTimeCredentials, formatBytes, formatDateTime };
  initialize();
})();
