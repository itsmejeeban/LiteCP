(() => {
  "use strict";
  const cp = window.LiteCP;
  const normalize = value => String(value || "").normalize("NFKD").replace(/[\u0300-\u036f]/g, "").toLowerCase().replace(/[^a-z0-9]+/g, " ").trim();
  const features = [
    ["system-updates", "System Update", "Compare and update system packages; schedule automatic updates", "", "apt dnf operating system service package version update automatic"],
    ["email/configure-webmail", "Configure Webmail", "Set up webmail for the first time", "", "roundcube hostname certificate tls mail setup install"],
    ["email/smtp-port-check", "SMTP Port Check", "Test outbound SMTP ports 25 and 465", "", "mail smtp provider blocked port connectivity firewall"],
    ["ssh-port", "SSH Port", "View or change the server SSH connection port", "", "security ssh port 369 connection remote access"],
    ["server-files", "Server File Explorer", "Browse server directories and view or download files", "", "server root filesystem explorer path read only"],
    ["settings/update", "LiteCP update", "Check for and install stable LiteCP releases", "#updateSettingsPanel", "update upgrade version panel release"],
    ["dashboard", "Overview", "CPU, memory, disk and uptime", "", "dashboard resources monitoring status"],
    ["websites", "Websites", "Add and manage WordPress, WHMCS and regular websites", "#sitesTable", "domain hosting php version"],
    ["websites", "Add website", "Create hosting for a domain", "", "create new wordpress whmcs install"],
    ["websites", "SSL certificates", "Enable HTTPS using the shield beside a website", "#sitesTable", "letsencrypt tls certificate renewal security"],
    ["websites", "Website cron jobs", "Create, edit and pause scheduled commands using a website's Cron jobs icon", "#sitesTable", "add custom task scheduler automation schedule"],
    ["websites", "Website FTP accounts", "Create and manage website logins using the FTP accounts icon", "#sitesTable", "ftps upload username password"],
    ["websites", "Website backups and restore", "Create, download or upload a backup using a website's backup icon", "#sitesTable", "manual zip public html database restore"],
    ["files", "File manager", "Browse and manage website files", ".file-manager-panel", "upload download edit rename copy move permissions compress extract trash fullscreen"],
    ["databases", "Databases", "Create databases and manage users and passwords", "#databasesTable", "mysql database username create reset credentials"],
    ["databases", "phpMyAdmin", "Click a database name to open its tables in a new tab", "#databasesTable", "pma mysql sql import export tables"],
    ["email/create-mailbox", "Create mailbox", "Add an email account for a hosted or additional domain", "#mailboxForm", "mail email account password quota imap smtp"],
    ["email/add-domain", "Mail domains", "Add a separate email domain", "#mailDomainForm", "email routing hosted domain"],
    ["email/create-alias", "Email forwarding", "Create an alias that forwards mail to another address", "#mailAliasForm", "alias redirect email forward"],
    ["email/mailboxes", "Webmail", "Open a mailbox using its Login Mailbox button", "#mailboxesTable", "roundcube email inbox login"],
    ["email/forward", "Email Forward", "Forward a source address to another email address", "#emailForwardForm", "forwarding redirect email alias destination"],
    ["email/delivery", "Email Delivery", "View email delivery logs, statuses and failure details", "#mailDeliveryPanel", "mail logs sent delivered bounced deferred rejected queue smtp"],
    ["email/domains", "Email DNS records", "Use Configuration or DNS records to view the required mail records", "#mailDomainsTable", "dkim spf dmarc mx ptr deliverability spam"],
    ["email", "Email hosting", "Enable or disable the server's mail services", "#mailHostingToggle", "smtp imap postfix dovecot"],
    ["applications", "Node.js hosting", "Enable Node.js and select an application runtime", "#applicationFeatures", "node nodejs npm javascript version selector"],
    ["applications", "Python hosting", "Enable Python and select an application runtime", "#applicationFeatures", "python pip flask django version selector"],
    ["applications", "Ruby hosting", "Enable Ruby and select an application runtime", "#applicationFeatures", "ruby rails gem version selector"],
    ["services", "Services", "View and restart hosting services", "", "nginx php fpm mysql mail cron start stop restart"],
    ["backups/system-snapshot", "System Snapshot", "Create or restore a complete LiteCP data ZIP", "#systemSnapshotPanel", "snapshot full backup restore migration settings websites databases mail"],
    ["extensions", "PHP extensions", "Search extension packages and filter by PHP version", "#extensionsTable", "install remove modules ioncube redis imagick mbstring curl zip"],
    ["analytics", "Usage graphs", "Live and historical CPU, memory and disk usage", "#usageView", "analytics graph cpu ram memory disk hourly daily weekly monthly"],
    ["disk-usage", "Disk Usages", "Directory-wise managed website storage pie chart", "", "analytics disk directory usage storage pie chart"],
    ["security", "Malware scan", "Scan a website with the antivirus service", "#scanForm", "virus clamav antivirus infected files"],
    ["waf", "Web application firewall", "Configure website WAF detection and blocking", "#wafTable", "waf modsecurity security protection"],
    ["firewall", "Firewall rules", "Manage inbound and outbound network access", "#firewallForm", "port ip tcp udp allow deny block network"],
    ["two-factor", "2FA", "Manage administrator authenticator and recovery codes", ".two-factor-panel", "two factor authentication totp security recovery codes authenticator"],
    ["backups/restore", "Restore backups", "Restore available local and recorded remote website backups", "#backupRestoreInventory", "restore recovery local remote storage"],
    ["backups/create", "Server backups", "Create a backup and review backup history", "#backupRunForm", "backup retention archive restore"],
    ["backups/add-destination", "Remote backup destinations", "Configure an off-server FTP, FTPS or SFTP destination", "#backupDestinationForm", "remote storage offsite upload password fingerprint"],
    ["migrations", "Live cPanel / Plesk migration", "Connect to a source server using root SSH access", "#liveSourceForm", "cpanel whm plesk migrate transfer root password private key ssh"],
    ["migrations", "Upload migration archive", "Inspect and import a website backup", "#migrationUploadForm", "migration restore tar zip sql database"],
    ["logs", "Server logs", "View Nginx, PHP, database, mail and cron logs", "#logTarget", "error access journal troubleshooting"],
    ["activities", "Recent Activities", "Review changes and choose how long activity history is kept", "#activityRetention", "audit event history retention seven 7 fifteen 15 days month year delete"],
    ["settings/email", "Administrator email", "Set the email used for certificate renewal notices", "#settingsEmail", "account email letsencrypt ssl"],
    ["settings/alerts", "Alerts", "Configure administrator email recipients and failure notifications", "#alertsSettingsPanel", "notification backup restore migration failed resource limit service down email"],
    ["settings/appearance", "Appearance", "Choose a panel theme", "#appearanceTheme", "theme default appearance"],
    ["settings/timezone", "Display timezone", "Choose the time zone used for panel dates", "#settingsTimezone", "time zone country utc timezone clock"],
    ["backups/settings", "Backup retention", "Set how long automatic backups are kept", "#settingsRetention", "backup days retention policy"],
    ["settings/password", "Administrator password", "Change your panel sign-in password", "#passwordForm", "account login credentials security"],
    ["settings/hostname", "Server hostname", "Change the server's operating-system hostname", "#hostnameForm", "hostname server name"],
    ["settings/dns", "DNS resolvers", "View current DNS and configure primary and secondary resolvers", "#resolversForm", "resolver dns nameserver 8.8.8.8 8.8.4.4 network"],
    ["settings/reboot", "Server reboot", "Review the server power controls", "#rebootServer", "restart reboot power"],
    ["settings", "Dark theme", "LiteCP uses a consistent dark interface", "", "theme appearance dark"],
    ["documentation", "Documentation", "Search the complete user manual", "#manualSearch", "help guide instructions features support manual"]
  ];
  function rank(items, query) {
    const text = normalize(query), tokens = text.split(" ").filter(Boolean);
    return items.map((item, index) => {
      const title = normalize(item.title), words = normalize([item.title, item.description, item.keywords, item.group].join(" "));
      const score = tokens.every(token => words.includes(token)) ? (title === text ? 100 : title.startsWith(text) ? 80 : title.includes(text) ? 60 : 20) : -1;
      return {item, score, index};
    }).filter(result => result.score >= 0).sort((a,b) => b.score-a.score || a.index-b.index).map(result => result.item);
  }
  const search = document.createElement("div"); search.className = "panel-search";
  search.innerHTML = '<div class="panel-search-field"><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 4.5 4.5"/></svg><input id="panelSearchInput" type="search" placeholder="Quick search…" aria-label="Search the panel" role="combobox" aria-autocomplete="list" aria-expanded="false" aria-controls="panelSearchResults" autocomplete="off" spellcheck="false" maxlength="150"></div><div class="panel-search-dropdown" hidden><p class="panel-search-status" role="status" aria-live="polite"></p><div id="panelSearchResults" role="listbox" aria-label="Panel search results"></div></div>';
  document.querySelector(".sidebar-brand").after(search);
  const input = search.querySelector("input"), dropdown = search.querySelector(".panel-search-dropdown"), list = search.querySelector("#panelSearchResults"), status = search.querySelector(".panel-search-status");
  // Keep results outside the scrolling sidebar so they are never clipped.
  document.body.append(dropdown);
  let results = [], selected = 0, manual = [], manualLoading = null, highlighted = null, highlightTimer = null;
  const entry = (page,title,description,selector,keywords,group="Features") => ({page,title,description,selector,keywords,group});
  function catalogue() {
    const items = features.map(item => entry(...item)), seen = new Set(items.map(item => item.page+":"+normalize(item.title)));
    for (const page of document.querySelectorAll("[data-page-view]")) {
      const name = page.dataset.pageView, heading = page.querySelector("h1")?.textContent || name;
      for (const node of page.querySelectorAll("h2, label.field > span:first-child")) {
        const settingsPanel=node.closest("[data-settings-panel]");
        if(node.closest(".hidden,[hidden]")&&!settingsPanel)continue;
        const targetPage=settingsPanel?"settings/"+settingsPanel.dataset.settingsPanel:name;
        const title = node.textContent.trim(); if (!title || title.length>100) continue;
        const key = targetPage+":"+normalize(title); if (seen.has(key)) continue; seen.add(key);
        const target = node.closest("label")?.querySelector("input:not([type=hidden]),select,textarea") || node.closest(".panel") || node;
        // Index labels and headings, never input values, commands, passwords or logs.
        items.push({...entry(targetPage,title,heading,"","","Settings"), target});
      }
    }
    const state = cp.state();
    for (const site of state?.sites || []) items.push({...entry("websites",site.domain,"Website · hosting, SSL, cron, FTP and backups","#sitesTable","domain website","Websites"),domain:site.domain});
    for (const database of state?.databases || []) items.push(entry("databases",database.name,"Database · "+database.username,"#databasesTable","mysql phpmyadmin sql database","Databases"));
    for (const mailbox of state?.mailboxes || []) items.push(entry("email/mailboxes",mailbox.address,"Mailbox · email account","#mailboxesTable","email mailbox webmail","Mailboxes"));
    for (const domain of state?.mailDomains || []) items.push(entry("email/domains",domain.domain,"Email domain · routing and DNS records","#mailDomainsTable","mail domain dns dkim spf dmarc mx","Mail domains"));
    return items.concat(manual);
  }
  function close() { dropdown.hidden=true; input.setAttribute("aria-expanded","false");input.removeAttribute("aria-activedescendant"); }
  function positionResults() {
    const rect=search.getBoundingClientRect(), width=Math.min(440,window.innerWidth-24);
    const top=Math.max(8,Math.min(rect.bottom+8,window.innerHeight-120));
    dropdown.style.width=width+"px";dropdown.style.left=Math.max(12,Math.min(rect.left,window.innerWidth-width-12))+"px";
    dropdown.style.top=top+"px";dropdown.style.maxHeight=Math.max(100,Math.min(420,window.innerHeight-top-12))+"px";
  }
  function select(index) {
    selected = (index+results.length)%results.length;
    [...list.children].forEach((node,i) => node.setAttribute("aria-selected",String(i===selected)));
    const node = list.children[selected]; if(node){input.setAttribute("aria-activedescendant",node.id);node.scrollIntoView({block:"nearest"});}
  }
  function show() {
    if (!cp.state()) { close(); return; }
    const matches = rank(catalogue(),input.value.trim()), limit = input.value.trim() ? 7 : 5;
    results = matches.slice(0,limit);selected=0;list.replaceChildren();
    for (const [index,item] of results.entries()) {
      const result=document.createElement("button");result.type="button";result.id="panel-search-result-"+index;result.className="panel-search-result";result.setAttribute("role","option");result.setAttribute("aria-selected",String(index===0));
      const title=document.createElement("strong"),description=document.createElement("span"),group=document.createElement("small");
      title.textContent=item.title;description.textContent=item.description;group.textContent=item.group;result.append(title,group,description);result.addEventListener("click",()=>open(item));list.append(result);
    }
    status.textContent = !results.length ? "No results. Try another feature or setting name." : !input.value.trim() ? "Quick access · type to search the panel" : `${matches.length} results${matches.length>limit?" · showing the best 7":""}`;
    dropdown.hidden=false;positionResults();input.setAttribute("aria-expanded","true");
    if(results.length)input.setAttribute("aria-activedescendant",list.children[0].id);else input.removeAttribute("aria-activedescendant");
  }
  function open(item) {
    close();input.value="";input.blur();cp.navigate(item.page);
    if(item.domain){const filter=document.querySelector("#siteSearch");filter.value=item.domain;filter.dispatchEvent(new Event("input",{bubbles:true}));}
    if(item.manualQuery){const filter=document.querySelector("#manualSearch");filter.value=item.manualQuery;filter.dispatchEvent(new Event("input",{bubbles:true}));}
    const target=item.target?.isConnected ? item.target : item.selector ? document.querySelector(item.selector) : document.querySelector(`[data-page-view="${item.page}"] .page-heading`);
    if(!target || target.closest("dialog, .hidden"))return;
    target.scrollIntoView({behavior:"smooth",block:"center"});
    highlighted?.classList.remove("panel-search-highlight");clearTimeout(highlightTimer);highlighted=target;target.classList.add("panel-search-highlight");
    highlightTimer=setTimeout(()=>target.classList.remove("panel-search-highlight"),2200);
    if(target.matches("input:not([type=hidden]),select,textarea,button"))target.focus({preventScroll:true});
  }
  async function loadManual() {
    if(manualLoading)return manualLoading;
    manualLoading=(async()=>{try{
      const response=await fetch("/manual.json");if(!response.ok)return;
      const sections=await response.json();if(!Array.isArray(sections))return;
      manual=sections.flatMap(section=>(Array.isArray(section.topics)?section.topics:[]).filter(topic=>typeof topic.title==="string").map(topic=>({...entry("documentation",topic.title,section.title,"#manualSearch",(topic.steps||[]).join(" "),"User manual"),manualQuery:topic.title})));
      if(document.activeElement===input)show();
    }catch{/* Features remain searchable when the manual is unavailable. */}})();return manualLoading;
  }
  input.addEventListener("focus",()=>{show();loadManual();});input.addEventListener("input",show);
  input.addEventListener("keydown",event=>{
    event.stopPropagation();
    if(event.key==="Escape"){event.preventDefault();close();return;}
    if(event.key==="ArrowDown"||event.key==="ArrowUp"){event.preventDefault();if(dropdown.hidden)show();if(results.length)select(selected+(event.key==="ArrowDown"?1:-1));}
    if(event.key==="Enter"){event.preventDefault();if(!dropdown.hidden&&results[selected])open(results[selected]);}
    if(event.key==="Tab")close();
  });
  document.addEventListener("keydown",event=>{
    if(!(event.ctrlKey||event.metaKey)||String(event.key).toLowerCase()!=="k"||!cp.state())return;
    event.preventDefault();event.stopPropagation();
    const sidebar=document.querySelector("#sidebar");
    if(window.innerWidth>900&&document.body.classList.contains("sidebar-collapsed"))document.querySelector("#sidebarBottomToggle")?.click();
    else if(window.innerWidth<=900&&!sidebar.classList.contains("open"))document.querySelector("#mobileMenu")?.click();
    input.focus();input.select();show();loadManual();
  });
  document.addEventListener("click",event=>{if(!search.contains(event.target)&&!dropdown.contains(event.target))close();});
  document.querySelector("#sidebar").addEventListener("scroll",close);
  window.addEventListener("resize",()=>{if(!dropdown.hidden)positionResults();});
  window.addEventListener("hashchange",close);
})();
