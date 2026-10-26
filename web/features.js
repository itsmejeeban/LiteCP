(() => {
  "use strict";
  const cp = window.LiteCP;
  const $ = (s, root = document) => root.querySelector(s);
  const $$ = (s, root = document) => [...root.querySelectorAll(s)];
  const esc = cp.escapeHTML;
  let activityPage = 1, activityRequest = 0, listing = null, editor = null, aceLoading = null, preview = null;
  const icon = '<svg viewBox="0 0 24 24"><rect x="4" y="4" width="16" height="16" rx="3"/><path d="M8 12h8M12 8v8"/></svg>';
  const fileManagerIcon = '<svg viewBox="0 0 24 24"><path d="M3 6h7l2 2h9v11H3z"/><path d="M3 9h18"/></svg>';
  const page = (id, title, copy, content) => `<section class="page" data-page-view="${id}"><div class="page-heading"><div><span class="eyebrow">LITECP MANAGEMENT</span><h1>${title}</h1><p>${copy}</p></div></div>${content}</section>`;
  const field = (label, input) => `<label class="field"><span>${label}</span>${input}</label>`;
  const resultBox = id => `<pre class="feature-output hidden" id="${id}" role="status"></pre>`;
  const output = (id, value) => { const node = $(`#${id}`); node.textContent = value; node.classList.remove("hidden"); };
  const notifyError = cp.handleError;
  const selectedPaths = () => $$("[data-file-select]:checked").map(node => node.value);
  const siteID = () => $("#fileSiteSelect").value;

  const nav = $(".nav");
  [["migrations", "Migration"], ["extensions", "PHP extensions"], ["security", "Website security"]].forEach(([id, title]) => {
    const link = document.createElement("a"); link.href = `#${id}`; link.dataset.page = id; link.className = "nav-item";
    link.innerHTML = `${icon}<span>${title}</span>`; link.addEventListener("click", () => cp.navigate(id)); nav.insertBefore(link, $(".nav-label-second"));
  });
  $(".main-content").insertAdjacentHTML("beforeend", page("extensions", "PHP extensions", "Manage optional extension packages per runtime. Core application modules remain protected.", `
    <article class="panel"><div class="panel-head"><div><h2>Installed runtimes</h2><p>Changes restart all websites using the selected PHP version. PHP 7.3 container modules require an image rebuild.</p></div><button class="button button-secondary" id="extensionsRefresh">Refresh</button></div><div id="extensionsTable"></div></article>`));
  $(".main-content").insertAdjacentHTML("beforeend", page("security", "Website security", "On-demand ClamAV scans and ModSecurity with the OWASP Core Rule Set.", `
    <div class="management-grid"><form id="scanForm" class="panel compact-form-card"><div class="panel-head"><div><h2>Malware scan</h2><p>Scan only; files are never automatically deleted.</p></div></div><div class="settings-body">
    ${field("Website", '<select name="siteId" id="scanSite" required></select>')}${field("Path within website", '<input name="path" placeholder="Leave empty to scan the whole site">')}
    <p class="feature-note">The scanner runs on demand, with no resident antivirus daemon. Large or encrypted files may be skipped. A scan cannot guarantee a malware-free website.</p><button class="button button-primary" type="submit">Run ClamAV scan</button></div></form>
    <article class="panel"><div class="panel-head"><div><h2>Web application firewall</h2><p id="wafEngineLabel">Checking engine…</p></div></div><div id="wafTable"></div></article></div>${resultBox("scanOutput")}`));
  $(".main-content").insertAdjacentHTML("beforeend", page("migrations", "Website migration", "Inspect a cPanel or Plesk website archive, then import into an empty LiteCP website.", `
    <div class="management-grid"><form id="migrationUploadForm" class="panel compact-form-card"><div class="panel-head"><div><h2>1. Inspect archive</h2><p>The source server is not modified.</p></div></div><div class="settings-body">
    ${field("Source", '<select name="platform"><option value="cpanel">cPanel account backup</option><option value="plesk">Plesk website export</option></select>')}
    ${field("Archive", '<input name="archive" type="file" accept=".zip,.tar,.tgz,.gz" required><small>ZIP, TAR or TAR.GZ, up to 2 GiB compressed / 8 GiB extracted.</small>')}
    <p class="feature-note">cPanel: archive must contain public_html. Plesk: export files with an httpdocs folder. Encrypted, nested and incremental server backups are not supported. Links are skipped.</p>
    <button class="button button-primary" type="submit">Upload and inspect</button><button class="button button-ghost" type="button" id="migrationStagesRefresh">Show staged archives</button><div id="migrationStages"></div></div></form>
    <form id="migrationImportForm" class="panel compact-form-card"><div class="panel-head"><div><h2>2. Review and import</h2><p id="migrationSummary">Upload an archive first.</p></div></div><div class="settings-body">
    ${field("Target website", '<select name="siteId" id="migrationSite" required></select>')}${field("Website root in archive", '<select name="root" id="migrationRoot" required></select>')}
    ${field("SQL dump (optional)", '<select name="sql" id="migrationSQL"><option value="">Files only</option></select>')}${field("Target database password", '<input name="password" type="password" autocomplete="current-password"><small>Required only for SQL import. The dump must omit CREATE DATABASE / USE statements for the old database.</small>')}
    <p class="feature-note">Create the target website first and move its placeholder to Trash. Files are staged before an atomic document-root switch. SQL imports use the target's restricted database user; a failed import resets the previously empty managed database.</p>
    <button class="button button-primary" type="submit" id="migrationImportButton" disabled>Import selected website</button><button class="button button-ghost" type="button" id="migrationDiscardButton" disabled>Delete staged archive</button></div></form></div>
    <div class="info-callout"><div><strong>Website files and a selected database only</strong><p>Update application credentials and paths after import. Mail accounts, DNS, SSL, server settings, addon sites and arbitrary cron jobs are not imported. Verify the website before changing DNS.</p></div></div>${resultBox("migrationOutput")}`));

  // Persistent, device-local theme selection. No account data is stored here.
  $(".topbar-actions").insertAdjacentHTML("afterbegin", '<label class="theme-control"><span class="sr-only">Theme</span><select id="themeSelect" aria-label="Theme"><option value="system">System theme</option><option value="light">Light theme</option><option value="dark">Dark theme</option></select></label>');
  try { $("#themeSelect").value = localStorage.getItem("litecp-theme") || "system"; } catch { /* storage unavailable */ }
  function theme() {
    const value = $("#themeSelect").value;
    document.documentElement.dataset.theme = value === "dark" || (value === "system" && matchMedia("(prefers-color-scheme: dark)").matches) ? "dark" : "light";
    try { localStorage.setItem("litecp-theme", value); } catch { /* storage unavailable */ }
    if (editor) editor.setTheme(`ace/theme/${document.documentElement.dataset.theme === "dark" ? "tomorrow_night" : "tomorrow"}`);
  }
  $("#themeSelect").addEventListener("change", theme); matchMedia("(prefers-color-scheme: dark)").addEventListener("change", theme);

  $(".activity-panel").insertAdjacentHTML("beforeend", '<nav class="pagination" aria-label="Activity pages"><button class="button button-ghost" id="activityPrevious">Previous</button><span id="activityPageLabel" aria-live="polite"></span><button class="button button-ghost" id="activityNext">Next</button></nav>');
  $("#activityPrevious").addEventListener("click", () => { if (activityPage > 1) { activityPage--; activity(); } });
  $("#activityNext").addEventListener("click", () => { activityPage++; activity(); });
  async function activity() {
    const request = ++activityRequest;
    try {
      let result;
      if (cp.demo) {
        const items = cp.state()?.activities || []; result = { items: items.slice((activityPage - 1)*5, activityPage*5), total: items.length, pages: Math.ceil(items.length/5) };
      } else result = await cp.api(`/api/activities?page=${activityPage}&perPage=5`);
      if (request !== activityRequest) return;
      if (!result.items.length && activityPage > 1) { activityPage = Math.max(1,result.pages); activity(); return; }
      $("#activityList").innerHTML = result.items.length ? result.items.map(item => `<div class="activity-row"><span class="activity-dot ${item.kind === "warning" ? "warning" : ""}"></span><div><strong>${esc(item.title)}</strong><small>${esc(item.detail)}</small></div><span class="activity-time">${esc(cp.formatDateTime(item.createdAt))}</span></div>`).join("") : '<div class="empty-state"><p>No activity yet.</p></div>';
      $("#activityPageLabel").textContent = `Page ${activityPage} of ${Math.max(1,result.pages)} · ${result.total} retained events`;
      $("#activityPrevious").disabled = activityPage <= 1; $("#activityNext").disabled = activityPage >= result.pages;
    } catch (error) { $("#activityPageLabel").textContent = error.message; }
  }

  // Add explicit source/destination and action controls to the existing rule form.
  $("#firewallForm .panel-head h2").textContent = "Add or edit rule";
  $("#firewallForm .panel-head p").textContent = "Outbound rules filter new host-originated connections; established replies remain allowed.";
  $("#firewallForm .settings-body").insertAdjacentHTML("afterbegin", `${field("Direction", '<select name="direction"><option value="inbound">Inbound</option><option value="outbound">Outbound</option></select>')}${field("Action", '<select name="action"><option value="allow">Allow</option><option value="deny">Deny</option></select>')}<input name="id" type="hidden">`);
  $("#firewallForm .settings-body").insertAdjacentHTML("beforeend", '<button type="button" class="button button-ghost" id="firewallCancelEdit">Cancel edit</button>');
  const directionChange = () => { const inbound = $('#firewallForm [name="direction"]').value === "inbound"; $('#firewallForm [name="source"]').parentElement.querySelector("span").textContent = inbound ? "Source IP / CIDR" : "Destination IP / CIDR"; };
  $('#firewallForm [name="direction"]').addEventListener("change", directionChange);
  $("#firewallCancelEdit").addEventListener("click", () => { $("#firewallForm").reset(); $('#firewallForm [name="id"]').value = ""; directionChange(); });
  $('[data-page-view="firewall"] .page-heading p').textContent = "Manage custom inbound and outbound rules. Default policies and installer-managed access remain protected.";
  $('[data-page-view="firewall"] .management-table h2').textContent = "Managed rules";
  async function saveFirewall(event) {
    event.preventDefault(); const form = event.currentTarget; const data = new FormData(form);
    const direction = data.get("direction");
    const body = { direction, action: data.get("action"), port: Number(data.get("port")), protocol: data.get("protocol"), label: data.get("label"), [direction === "outbound" ? "destination" : "source"]: String(data.get("source")).trim() };
    const id = String(data.get("id") || "");
    if (body.action === "deny" && !confirm(`Apply this ${direction} block? It can interrupt applications using port ${body.port}.`)) return;
    await busy(form, async () => { await cp.api(`/api/firewall${id ? "/"+encodeURIComponent(id) : ""}`, {method:id ? "PUT":"POST", body}); form.reset(); $('#firewallForm [name="id"]').value = ""; directionChange(); await cp.refresh(); cp.showToast("Firewall saved", "The custom rule has been applied."); });
  }
  async function firewallAction(event) {
    const button = event.target.closest("[data-firewall-action]"); if (!button) return;
    const rule = (cp.state().firewallRules || []).find(item => item.id === button.dataset.id); if (!rule) return;
    if (button.dataset.firewallAction === "edit") {
      const form = $("#firewallForm"); for (const key of ["id", "port", "protocol", "label"]) form.elements[key].value = rule[key] || "";
      form.elements.direction.value = rule.direction || "inbound"; form.elements.action.value = rule.action || "allow";
      form.elements.source.value = (rule.direction === "outbound" ? rule.destination : rule.source) || "any"; directionChange(); form.scrollIntoView({behavior:"smooth"}); return;
    }
    if (!confirm("Delete this custom firewall rule?")) return;
    try { await cp.api(`/api/firewall/${encodeURIComponent(rule.id)}`, {method:"DELETE"}); await cp.refresh(); cp.showToast("Rule removed", "Permanent firewall configuration updated."); } catch (error) { notifyError(error); }
  }

  // File workspace: batch copy/move/delete, filtering, sorting and recoverable trash.
  $(".file-toolbar").insertAdjacentHTML("beforeend", '<button class="button button-secondary" id="newFileButton">New file</button><button class="button button-secondary" data-transfer="copy">Copy selected</button><button class="button button-secondary" data-transfer="move">Move selected</button><button class="button button-ghost" id="trashSelected">Trash selected</button><button class="button button-ghost" id="openTrash">Trash</button>');
  $(".file-breadcrumb").insertAdjacentHTML("afterend", '<div class="file-filters"><input id="fileSearch" type="search" placeholder="Filter current folder" aria-label="Filter files"><label><input type="checkbox" id="showHidden" checked> Show dotfiles</label><select id="fileSort" aria-label="Sort files"><option value="name">Name</option><option value="modified">Newest modified</option><option value="size">Largest first</option></select></div>');
  $(".file-manager-panel").insertAdjacentHTML("afterend", '<article class="panel hidden" id="trashPanel"><div class="panel-head"><div><h2>Trash / Bin</h2><p>Stored outside the public website. Nothing is automatically purged.</p></div><div class="feature-actions"><button class="button button-ghost" id="closeTrash">Close</button><button class="button button-secondary" id="emptyTrash">Empty trash permanently</button></div></div><div id="trashTable"></div></article>');
  ["fileSearch", "fileSort", "showHidden"].forEach(id => $(`#${id}`).addEventListener(id === "fileSearch" ? "input" : "change", () => { if (listing) cp.renderFileListing(listing); }));
  $("#newFileButton").addEventListener("click", async () => { const name = prompt("New file name:", "index.php"); if (!name) return; try { const result = await cp.api("/api/files/create",{method:"POST",body:{siteId:siteID(),path:cp.path(),name}}); await cp.loadFiles(); await cp.openEditor(result.path); } catch(error){notifyError(error);} });
  $$('[data-transfer]').forEach(button => button.addEventListener("click", async () => {
    const paths = selectedPaths(); if (!paths.length) { cp.showToast("Select files", "Choose files or folders first.","error"); return; }
    const destination = prompt("Destination folder relative to the website root (empty = root). Existing names are not overwritten:", ""); if (destination === null) return;
    try { button.disabled = true; await cp.api("/api/files/transfer", {method:"POST",body:{siteId:siteID(),paths,destination,operation:button.dataset.transfer}}); await cp.loadFiles(); } catch(error){notifyError(error);} finally{button.disabled=false;}
  }));
  $("#trashSelected").addEventListener("click",async()=>{
    const paths=selectedPaths();if(!paths.length || !confirm(`Move ${paths.length} selected item(s) to Trash?`))return;
    try{for(const path of paths)await cp.api("/api/files",{method:"DELETE",body:{siteId:siteID(),path}});await cp.loadFiles();cp.showToast("Moved to Trash","Items can be restored from the Trash view.");}catch(error){notifyError(error);await cp.loadFiles();}
  });
  $("#openTrash").addEventListener("click",loadTrash); $("#closeTrash").addEventListener("click",()=>$("#trashPanel").classList.add("hidden"));
  $("#fileSiteSelect").addEventListener("change",()=>$("#trashPanel").classList.add("hidden"));
  async function loadTrash(){
    if(!siteID())return;$("#trashPanel").classList.remove("hidden");
    try{const result=cp.demo?{entries:[]}:await cp.api(`/api/files/trash?siteId=${encodeURIComponent(siteID())}`);
      $("#trashTable").innerHTML=result.entries.length?`<table class="data-table"><thead><tr><th>ORIGINAL PATH</th><th>SIZE</th><th>DELETED</th><th>ACTIONS</th></tr></thead><tbody>${result.entries.map(item=>`<tr><td><code>${esc(item.originalPath)}</code></td><td>${cp.formatBytes(item.size)}</td><td>${cp.formatDateTime(item.deletedAt)}</td><td><span class="row-actions"><button class="text-action" data-trash="restore" data-id="${esc(item.id)}">Restore</button><button class="text-action danger" data-trash="delete" data-id="${esc(item.id)}">Delete permanently</button></span></td></tr>`).join("")}</tbody></table>`:'<div class="empty-state"><p>Trash is empty.</p></div>';
    }catch(error){notifyError(error);}
  }
  $("#trashTable").addEventListener("click",async event=>{
    const button=event.target.closest("[data-trash]");if(!button)return;
    const restore=button.dataset.trash==="restore";let destination="";
    if(restore){destination=prompt("Restore path (leave empty for the original location; its parent folder must exist):","");if(destination===null)return;}
    else if(!confirm("Permanently delete this trash item? It cannot be recovered by LiteCP."))return;
    try{await cp.api("/api/files/trash",{method:restore?"POST":"DELETE",body:{siteId:siteID(),id:button.dataset.id,destination}});await loadTrash();await cp.loadFiles();}catch(error){notifyError(error);}
  });
  $("#emptyTrash").addEventListener("click",async()=>{if(!confirm("Permanently empty this website's Trash? Every item in it will be irrecoverable."))return;try{await cp.api("/api/files/trash",{method:"DELETE",body:{siteId:siteID(),id:""}});await loadTrash();}catch(error){notifyError(error);}});

  // Ace loads from the embedded asset bundle only when editing a file.
  $("#editorForm .modal-body").insertAdjacentHTML("afterbegin", '<div class="editor-tools"><label>Language <select id="editorLanguage"><option value="text">Plain text</option><option value="php">PHP</option><option value="html">HTML</option><option value="css">CSS</option><option value="javascript">JavaScript</option><option value="json">JSON</option><option value="sql">SQL</option><option value="yaml">YAML</option><option value="ini">INI</option><option value="sh">Shell</option><option value="markdown">Markdown</option></select></label><button type="button" class="button button-ghost" id="editorFind">Find / replace</button><button type="button" class="button button-ghost" id="editorWrap">Word wrap</button><button type="button" class="button button-ghost" id="editorFullscreen">Expand</button><span id="editorPosition">UTF-8</span></div><div id="aceEditor" class="hidden" aria-label="Website code editor"></div>');
  function loadAce(){if(window.ace)return Promise.resolve();if(aceLoading)return aceLoading;aceLoading=new Promise((resolve,reject)=>{const script=document.createElement("script");script.src="/vendor/ace/ace.js";script.onload=resolve;script.onerror=()=>{aceLoading=null;reject(new Error("The code editor could not load. The text editor remains available."));};document.head.append(script);});return aceLoading;}
  async function openEditor(path,content){
    // Ace injects its own CSS; the panel permits inline styles, but not inline
    // scripts/eval. Workers stay disabled and every script is served locally.
    try{await loadAce();ace.config.set("basePath","/vendor/ace");ace.config.set("useStrictCSP",false);
      if(!editor){editor=ace.edit("aceEditor");editor.setOptions({fontSize:"14px",showPrintMargin:false,tabSize:2,useSoftTabs:true});editor.session.setUseWorker(false);editor.commands.addCommand({name:"saveLiteCP",bindKey:{win:"Ctrl-S",mac:"Command-S"},exec:()=>$("#editorForm").requestSubmit()});editor.selection.on("changeCursor",()=>{const pos=editor.getCursorPosition();$("#editorPosition").textContent=`Ln ${pos.row+1}, Col ${pos.column+1} · UTF-8`;});}
      const suffix=path.split(".").pop().toLowerCase();const map={js:"javascript",mjs:"javascript",htm:"html",yml:"yaml",md:"markdown",conf:"ini",txt:"text"};const language=map[suffix]||suffix;$("#editorLanguage").value=$$('#editorLanguage option').some(o=>o.value===language)?language:"text";
      editor.session.setMode(`ace/mode/${$("#editorLanguage").value}`);editor.setValue(content,-1);theme();$("#editorContent").classList.add("hidden");$("#aceEditor").classList.remove("hidden");editor.resize();editor.focus();
    }catch(error){cp.showToast("Text editor fallback",error.message,"error");}
  }
  $("#editorLanguage").addEventListener("change",()=>editor?.session.setMode(`ace/mode/${$("#editorLanguage").value}`));
  $("#editorFind").addEventListener("click",()=>editor?.execCommand("replace"));
  $("#editorWrap").addEventListener("click",()=>editor?.session.setUseWrapMode(!editor.session.getUseWrapMode()));
  $("#editorFullscreen").addEventListener("click",()=>{$("#editorModal .editor-modal").classList.toggle("expanded");editor?.resize();});

  function generatePassword(){const alphabet="ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#_-";const max=256-(256%alphabet.length);let value="";while(value.length<24){const bytes=crypto.getRandomValues(new Uint8Array(48));for(const b of bytes){if(b<max)value+=alphabet[b%alphabet.length];if(value.length===24)break;}}return value;}
  function enhancePasswords(root=document){
    $$('input[type="password"]',root).forEach(input=>{
      if(input.dataset.enhanced || input.id==="loginPassword")return;input.dataset.enhanced="1";
      const controls=document.createElement("span");controls.className="password-actions";
      const toggle=document.createElement("button");toggle.type="button";toggle.className="button button-ghost";toggle.textContent="Show";toggle.setAttribute("aria-label","Show password");toggle.addEventListener("click",()=>{const show=input.type==="password";input.type=show?"text":"password";toggle.textContent=show?"Hide":"Show";toggle.setAttribute("aria-label",show?"Hide password":"Show password");});controls.append(toggle);
      if(input.autocomplete!=="current-password" && !["current","confirm"].includes(input.name)){
        const generate=document.createElement("button");generate.type="button";generate.className="button button-ghost";generate.textContent="Generate";
        generate.addEventListener("click",()=>{input.value=generatePassword();if(input.name==="next" && input.form?.elements.confirm)input.form.elements.confirm.value=input.value;input.dispatchEvent(new Event("input",{bubbles:true}));});controls.prepend(generate);
      }
      input.insertAdjacentElement("afterend",controls);
    });
  }
  function askPassword(title,generate){
    return new Promise(resolve=>{
      const dialog=document.createElement("dialog");dialog.className="password-dialog";
      dialog.innerHTML=`<form method="dialog"><h2>${esc(title)}</h2>${field("Password",`<input name="password" type="password" autocomplete="${generate?"new-password":"current-password"}" ${generate?'minlength="12"':''} required>`)}<div class="feature-actions"><button class="button button-ghost" type="button" data-cancel>Cancel</button><button class="button button-primary" type="submit">Continue</button></div></form>`;
      document.body.append(dialog);enhancePasswords(dialog);let finished=false;
      const finish=value=>{if(finished)return;finished=true;dialog.close();dialog.remove();resolve(value);};
      $("[data-cancel]",dialog).addEventListener("click",()=>finish(null));dialog.addEventListener("cancel",()=>finish(null));$("form",dialog).addEventListener("submit",event=>{event.preventDefault();finish($('input',dialog).value);});dialog.showModal();$('input',dialog).focus();
    });
  }
  enhancePasswords();

  $("[data-page-view=email] .page-heading").insertAdjacentHTML("beforeend", '<div class="feature-actions"><a class="button button-secondary hidden" id="webmailLink" target="_blank" rel="noopener">Open webmail</a><button class="button button-primary" id="mailHostingToggle">Email hosting</button></div>');
  $("#mailHostingToggle").addEventListener("click",async()=>{const enabled=cp.state().settings.emailHostingEnabled!==false;if(!confirm(`${enabled?"Disable":"Enable"} email hosting? ${enabled?"SMTP delivery and mailbox access will stop; messages and accounts will be kept.":"Postfix, Dovecot and DKIM services will start."}`))return;try{await cp.api("/api/mail/hosting",{method:"PUT",body:{enabled:!enabled}});await cp.refresh();}catch(error){notifyError(error);}});
  $("[data-page-view=email] .stacked-panels").insertAdjacentHTML("beforeend", '<article class="panel hidden" id="mailDNSPanel"><div class="panel-head"><div><h2 id="mailDNSTitle">DNS records</h2><p>Publish these at your DNS provider. LiteCP does not change public DNS.</p></div></div><div id="mailDNSTable"></div><p class="feature-note" id="mailDNSNote"></p></article>');
  $("#mailDomainsTable").addEventListener("click",async event=>{const button=event.target.closest("[data-dns-domain]");if(!button)return;try{if(cp.demo)throw new Error("DNS generation requires the installed mail server.");const data=await cp.api(`/api/mail/dns?domain=${encodeURIComponent(button.dataset.dnsDomain)}`);$("#mailDNSPanel").classList.remove("hidden");$("#mailDNSTitle").textContent=`DNS for ${button.dataset.dnsDomain}`;$("#mailDNSTable").innerHTML=`<table class="data-table dns-table"><thead><tr><th>TYPE</th><th>NAME</th><th>VALUE</th></tr></thead><tbody>${data.records.map(item=>`<tr><td>${esc(item.type)}</td><td><code>${esc(item.name)}</code></td><td><code>${esc(item.value)}</code></td></tr>`).join("")}</tbody></table>`;$("#mailDNSNote").textContent=(!data.dkimReady?"DKIM is not ready; install/configure OpenDKIM first. ":"")+data.note;}catch(error){notifyError(error);}});

  $("[data-page-view=settings] .settings-grid").insertAdjacentHTML("beforeend", `
    <form class="panel settings-card" id="hostnameForm"><div class="panel-head"><div><h2>Server hostname</h2><p>Changes the OS hostname, not public DNS or mail identity.</p></div></div><div class="settings-body">${field("Hostname",'<input name="hostname" id="hostnameInput" required maxlength="253">')}<button class="button button-secondary" type="submit">Change hostname</button></div></form>
    <form class="panel settings-card" id="resolversForm"><div class="panel-head"><div><h2>DNS resolvers</h2><p>Last applied through LiteCP; blank means existing provider settings are unmanaged.</p></div></div><div class="settings-body">${field("Resolver IPs",'<input name="resolvers" id="resolversInput" required placeholder="8.8.8.8, 8.8.4.4"><small>Comma-separated, one to four IP addresses. Private networks may require the provider’s DNS.</small>')}<button class="button button-secondary" type="submit">Apply resolvers</button></div></form>
    <article class="panel settings-card"><div class="panel-head"><div><h2>Server power</h2><p>Rebooting temporarily interrupts every website and mailbox.</p></div></div><div class="settings-body"><button class="button button-secondary danger" id="rebootServer">Reboot server</button></div></article>`);
  $("#hostnameForm").addEventListener("submit",event=>{event.preventDefault();busy(event.currentTarget,async()=>{await cp.api("/api/system/hostname",{method:"PUT",body:{hostname:$("#hostnameInput").value}});await cp.refresh();cp.showToast("Hostname changed","Public DNS and certificate names were not changed.");});});
  $("#resolversForm").addEventListener("submit",event=>{event.preventDefault();if(!confirm("Change system DNS resolvers? Incorrect values may interrupt application connections."))return;busy(event.currentTarget,async()=>{await cp.api("/api/system/resolvers",{method:"PUT",body:{resolvers:$("#resolversInput").value.split(",").map(v=>v.trim()).filter(Boolean)}});await cp.refresh();cp.showToast("Resolvers changed","Persistent DNS settings applied.");});});
  $("#rebootServer").addEventListener("click",async()=>{const hostname=cp.state().system.hostname;const value=prompt(`Type ${hostname} to reboot this server. All hosted services will disconnect.`,"");if(value!==hostname)return;try{await cp.api("/api/system/reboot",{method:"POST",body:{confirm:value}});cp.showToast("Reboot scheduled","The server will restart shortly. Reconnect after it returns.");}catch(error){notifyError(error);}});

  async function busy(form,action){const controls=$$("button",form);controls.forEach(b=>b.disabled=true);form.setAttribute("aria-busy","true");try{await action();}catch(error){const outputId={scanForm:"scanOutput",migrationUploadForm:"migrationOutput",migrationImportForm:"migrationOutput"}[form.id];if(outputId)output(outputId,`Request failed: ${error.message}`);notifyError(error);}finally{controls.forEach(b=>b.disabled=false);form.removeAttribute("aria-busy");}}
  async function extensions(){
    $("#extensionsTable").innerHTML='<div class="empty-state"><p>Reading PHP modules…</p></div>';
    try{if(cp.demo)throw new Error("Extension management is available on the installed Linux server.");const result=await cp.api("/api/php/extensions");$("#extensionsTable").innerHTML=`<table class="data-table"><thead><tr><th>PHP</th><th>EXTENSION</th><th>STATE</th><th>ACTION</th></tr></thead><tbody>${result.extensions.map(item=>`<tr><td>PHP ${esc(item.version)}</td><td>${esc(item.label)}</td><td><span class="badge ${item.installed?"success":""}">${item.installed?"Installed":"Not installed"}</span></td><td>${item.readOnly?'Image-managed':`<button class="button button-ghost" data-extension="${esc(item.key)}" data-version="${esc(item.version)}" data-install="${!item.installed}">${item.installed?"Remove":"Install"}</button>`}</td></tr>`).join("")}</tbody></table>`;}catch(error){$("#extensionsTable").innerHTML=`<div class="empty-state"><p>${esc(error.message)}</p></div>`;}
  }
  $("#extensionsRefresh").addEventListener("click",extensions);
  $("#extensionsTable").addEventListener("click",async event=>{const b=event.target.closest("[data-extension]");if(!b)return;const installed=b.dataset.install==="true";if(!confirm(`${installed?"Install":"Remove"} ${b.dataset.extension} for PHP ${b.dataset.version}? Sites on this runtime will briefly restart; removal may break application features.`))return;b.disabled=true;try{await cp.api("/api/php/extensions",{method:"PUT",body:{version:b.dataset.version,extension:b.dataset.extension,installed}});await extensions();}catch(error){notifyError(error);b.disabled=false;}});
  $("#wafTable").addEventListener("change",async event=>{const select=event.target.closest("[data-waf]");if(!select)return;if(select.value==="block"&&!confirm("Enable blocking? Test legitimate WHMCS/WordPress requests in Detect mode first to identify false positives.")){render(cp.state());return;}select.disabled=true;try{await cp.api("/api/sites/waf",{method:"PUT",body:{id:select.dataset.waf,mode:select.value}});await cp.refresh();}catch(error){notifyError(error);render(cp.state());}});
  $("#scanForm").addEventListener("submit",event=>{event.preventDefault();busy(event.currentTarget,async()=>{if(cp.demo)throw new Error("ClamAV scans require the installed server.");output("scanOutput","Scanning… This may take several minutes. Do not submit another scan.");const data=new FormData(event.currentTarget);const result=await cp.api("/api/security/scan",{method:"POST",body:{siteId:data.get("siteId"),path:data.get("path")}});output("scanOutput",`Result: ${result.status}\n\n${result.output}\n\nNo files were modified.`);});});
  $("#migrationUploadForm").addEventListener("submit",event=>{event.preventDefault();const form=event.currentTarget;if(preview){notifyError(new Error("Delete the currently staged archive before uploading another."));return;}busy(form,async()=>{if(cp.demo)throw new Error("Archive migration requires the installed server.");output("migrationOutput","Uploading and inspecting archive… No live website data is being changed.");preview=await cp.api("/api/migrations",{method:"POST",body:new FormData(form)});$("#migrationSummary").textContent=`${preview.files} files · ${cp.formatBytes(preview.bytes)} · ${preview.skipped} links/special entries skipped`;
    $("#migrationRoot").innerHTML=preview.roots.map(root=>`<option>${esc(root)}</option>`).join("");$("#migrationSQL").innerHTML='<option value="">Files only</option>'+preview.databases.map(file=>`<option>${esc(file)}</option>`).join("");$("#migrationImportButton").disabled=false;$("#migrationDiscardButton").disabled=false;output("migrationOutput",`Archive staged. Review the source root and target website. Staging ID: ${preview.id}`);});});
  $("#migrationImportForm").addEventListener("submit",event=>{event.preventDefault();if(!preview)return;const form=event.currentTarget;if(!confirm("Import files and the selected database into this empty target? Confirm that the target website and database are empty and unused."))return;busy(form,async()=>{const data=new FormData(form);output("migrationOutput","Staging and validating the import… Keep this page open.");const result=await cp.api("/api/migrations/import",{method:"POST",body:{id:preview.id,siteId:data.get("siteId"),root:data.get("root"),sql:data.get("sql"),password:data.get("password")}});form.elements.password.value="";output("migrationOutput",result.note);await cp.refresh();});});
  $("#migrationDiscardButton").addEventListener("click",async()=>{if(!preview||!confirm("Delete the staged migration archive? Imported website files will be kept."))return;try{await cp.api(`/api/migrations/${encodeURIComponent(preview.id)}`,{method:"DELETE"});preview=null;$("#migrationImportButton").disabled=true;$("#migrationDiscardButton").disabled=true;$("#migrationRoot").innerHTML="";$("#migrationSQL").innerHTML='<option value="">Files only</option>';$("#migrationSummary").textContent="Upload an archive first.";output("migrationOutput","Staged archive deleted.");}catch(error){notifyError(error);}});
  $("#migrationStagesRefresh").addEventListener("click",async()=>{
    try { if(cp.demo)throw new Error("Migration staging requires the installed server.");const result=await cp.api("/api/migrations");const container=$("#migrationStages");container.replaceChildren();
      if(!result.items.length)container.textContent="No staged archives.";
      result.items.forEach(item=>{const button=document.createElement("button");button.type="button";button.className="button button-ghost";button.textContent=`${item.platform} · ${cp.formatDateTime(item.createdAt)} · ${cp.formatBytes(item.bytes)}`;
        button.addEventListener("click",()=>{preview=item;$("#migrationRoot").innerHTML=item.roots.map(root=>`<option>${esc(root)}</option>`).join("");$("#migrationSQL").innerHTML='<option value="">Files only</option>'+item.databases.map(file=>`<option>${esc(file)}</option>`).join("");$("#migrationSummary").textContent=`${item.files} files · ${cp.formatBytes(item.bytes)} · ${item.skipped} skipped`;$("#migrationImportButton").disabled=false;$("#migrationDiscardButton").disabled=false;output("migrationOutput",`Staged archive selected: ${item.id}`);});container.append(button);});
    } catch(error){notifyError(error);}
  });

  function fillSiteSelect(id,state){const node=$(`#${id}`),value=node.value;node.innerHTML=state.sites.map(s=>`<option value="${esc(s.id)}">${esc(s.domain)}</option>`).join("");if(state.sites.some(s=>s.id===value))node.value=value;}
  function render(state){
    if(!state)return;
    $$('[data-site-action="delete"]').forEach(button=>{if(button.parentElement.querySelector('[data-site-action="files"]'))return;button.insertAdjacentHTML("beforebegin",`<button title="Open file manager" aria-label="Open file manager" data-site-action="files" data-id="${esc(button.dataset.id)}">${fileManagerIcon}</button>`);});
    const rows=(state.firewallRules||[]).map(rule=>`<tr><td>${esc(rule.direction||"inbound")}</td><td>${esc(rule.action||"allow")}</td><td>${rule.port}/${esc(rule.protocol)}</td><td><code>${esc(rule.direction==="outbound"?rule.destination:rule.source)}</code></td><td>${esc(rule.label)}</td><td><span class="row-actions"><button class="text-action" data-firewall-action="edit" data-id="${esc(rule.id)}">Edit</button><button class="text-action danger" data-firewall-action="delete" data-id="${esc(rule.id)}">Delete</button></span></td></tr>`).join("");
    $("#firewallTable").innerHTML=`<p class="feature-note">Installer rules for SSH, HTTP, HTTPS and LiteCP remain protected. Other outbound traffic remains allowed unless blocked by a rule. Host rules do not govern separately routed container/VM traffic.</p><table class="data-table"><thead><tr><th>DIRECTION</th><th>ACTION</th><th>PORT</th><th>ADDRESS</th><th>LABEL</th><th></th></tr></thead><tbody>${rows||'<tr><td colspan="6">No custom firewall rules.</td></tr>'}</tbody></table>`;
    $("#mailHostingToggle").textContent=state.settings.emailHostingEnabled===false?"Enable email hosting":"Disable email hosting";
    const available=state.system.webmailAvailable && state.settings.emailHostingEnabled!==false;$("#webmailLink").classList.toggle("hidden",!available);$("#webmailLink").href=state.system.webmailUrl||`https://${location.hostname}:8444/`;
    $$('[data-mail-action="domain-delete"]').forEach(button=>{const domain=state.mailDomains.find(d=>d.id===button.dataset.id);if(domain)button.insertAdjacentHTML("beforebegin",`<button class="text-action" data-dns-domain="${esc(domain.domain)}">DNS records</button>`);});
    $$('[data-service="mail"]').forEach(button=>{button.disabled=state.settings.emailHostingEnabled===false;});
    ["scanSite","migrationSite"].forEach(id=>fillSiteSelect(id,state));
    $("#wafEngineLabel").textContent=`Engine: ${state.system.wafEngine||"not installed"}. Begin with Detect; Blocking may need application-specific tuning.`;
    $("#wafTable").innerHTML=`<table class="data-table"><thead><tr><th>WEBSITE</th><th>WAF MODE</th></tr></thead><tbody>${state.sites.map(site=>`<tr><td>${esc(site.domain)}</td><td><select data-waf="${esc(site.id)}" aria-label="WAF mode for ${esc(site.domain)}">${[["off","Off"],["detect","Detect / log"],["block","Block attacks"]].map(([v,label])=>`<option value="${v}" ${(site.wafMode||"off")===v?"selected":""}>${label}</option>`).join("")}</select></td></tr>`).join("")}</tbody></table>`;
    if(document.activeElement!==$("#hostnameInput"))$("#hostnameInput").value=state.system.hostname||"";
    if(document.activeElement!==$("#resolversInput"))$("#resolversInput").value=(state.settings.resolvers||[]).join(", ");
  }
  window.LiteCPFeatures={render,activity,saveFirewall,firewallAction,askPassword,openEditor,editorValue:()=>editor?editor.getValue():$("#editorContent").value,
    rememberListing:value=>{listing=value;},filterFiles:values=>{const query=$("#fileSearch").value.toLowerCase();const items=values.filter(item=>(!query||item.name.toLowerCase().includes(query))&&($("#showHidden").checked||!item.name.startsWith(".")));const sort=$("#fileSort").value;return items.sort((a,b)=>a.type!==b.type?(a.type==="directory"?-1:1):sort==="size"?b.size-a.size:sort==="modified"?new Date(b.modified)-new Date(a.modified):a.name.localeCompare(b.name));},
    navigate:id=>{if(id==="extensions")extensions();}
  };
  if(cp.state()){render(cp.state());activity();cp.navigate(location.hash.slice(1)||"dashboard",false);}
})();
