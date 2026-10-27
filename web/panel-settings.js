(() => {
  "use strict";
  const cp=window.LiteCP, $=s=>document.querySelector(s), esc=cp.escapeHTML;
  const hostnameRow = document.createElement("span");
  hostnameRow.className = "server-hostname-row";
  $("#topHostname").before(hostnameRow);
  hostnameRow.append($("#topHostname"));
  hostnameRow.insertAdjacentHTML("beforeend",'<span id="topServerIP" class="top-server-ip"></span>');
  const form=$("#resolversForm");
  form.querySelector(".panel-head p").textContent="View current default DNS servers. Automatic enforcement restores Google DNS after boot and every five minutes.";
  form.querySelector(".settings-body").innerHTML=`<div class="resolver-current"><strong>Current resolvers</strong><div id="currentResolverList" aria-live="polite">Reading current configuration…</div></div><div class="resolver-fields"><label class="field"><span>Primary resolver</span><input id="resolverPrimary" name="primary" required placeholder="8.8.8.8" autocomplete="off" spellcheck="false"><small>IPv4 or IPv6 address</small></label><label class="field"><span>Secondary resolver</span><input id="resolverSecondary" name="secondary" placeholder="8.8.4.4" autocomplete="off" spellcheck="false"><small>Optional alternate DNS server</small></label></div><input id="resolversInput" type="hidden"><p class="feature-note">Enforced defaults: 8.8.8.8 and 8.8.4.4. Other addresses applied here are temporary and will be replaced within five minutes or after reboot. Existing split-domain DNS routes remain unchanged.</p><button class="button button-secondary" type="submit">Apply resolvers</button>`;
  form.addEventListener("submit",async event=>{
    event.preventDefault();event.stopImmediatePropagation();
    if(!form.reportValidity())return;
    const resolvers=[$("#resolverPrimary").value.trim(),$("#resolverSecondary").value.trim()].filter(Boolean);
    if(resolvers.some(value=>/[\s,]/.test(value))){cp.showToast("Invalid resolver","Enter one IP address in each field.","error");return;}
    if(!await window.LiteCPDialogs.confirm(`Apply ${resolvers.join(" and ")} as the current default DNS resolvers? Automatic enforcement restores 8.8.8.8 and 8.8.4.4 within five minutes and after reboot. Incorrect addresses can interrupt application connections.`))return;
    const button=form.querySelector('button[type="submit"]');button.disabled=true;
    try{await cp.api("/api/system/resolvers",{method:"PUT",body:{resolvers}});await cp.refresh();cp.showToast("Resolvers updated","The current system resolver list has been refreshed.");}
    catch(error){cp.handleError(error);}finally{button.disabled=false;}
  },true);
  const oldRender=window.LiteCPFeatures.render;
  function render(state){
    oldRender(state);
    $("#topServerIP").textContent=state.system.ipAddress || "";
    const current=state.system.currentResolvers || [];
    $("#currentResolverList").innerHTML=current.length ? current.map(value=>`<code>${esc(value)}</code>`).join("") : '<span>No resolver addresses could be read from the server.</span>';
    const configured=state.settings.resolvers?.length ? state.settings.resolvers : current;
    if(!form.contains(document.activeElement)){
      $("#resolverPrimary").value=configured[0] || "";
      $("#resolverSecondary").value=configured[1] || "";
    }
  }
  window.LiteCPFeatures.render=render;
  if(cp.state())render(cp.state());

  // Preserve the existing mobile drawer and add a persistent desktop toggle.
  const toggle=$("#mobileMenu"), sidebar=$("#sidebar"), scrim=$("#sidebarScrim"), mobile=matchMedia("(max-width: 900px)");
  toggle.setAttribute("aria-controls","sidebar");
  try{document.body.classList.toggle("sidebar-collapsed",localStorage.getItem("litecp-sidebar-hidden")==="true");}catch{}
  function syncSidebar(){
    const visible=mobile.matches ? sidebar.classList.contains("open") : !document.body.classList.contains("sidebar-collapsed");
    toggle.setAttribute("aria-expanded",String(visible));toggle.setAttribute("aria-label",visible ? "Hide navigation" : "Show navigation");toggle.title=visible ? "Hide navigation" : "Show navigation";
    sidebar.inert=!visible;
  }
  toggle.addEventListener("click",event=>{event.stopImmediatePropagation();if(mobile.matches){sidebar.classList.toggle("open");scrim.classList.toggle("visible",sidebar.classList.contains("open"));}else{document.body.classList.toggle("sidebar-collapsed");try{localStorage.setItem("litecp-sidebar-hidden",String(document.body.classList.contains("sidebar-collapsed")));}catch{}}syncSidebar();},true);
  scrim.addEventListener("click",()=>requestAnimationFrame(syncSidebar));
  sidebar.addEventListener("click",()=>requestAnimationFrame(syncSidebar));
  mobile.addEventListener("change",()=>{sidebar.classList.remove("open");scrim.classList.remove("visible");syncSidebar();});
  syncSidebar();

  const select=$("#settingsTimezone");
  select.setAttribute("aria-label","Display timezone");
  select.insertAdjacentHTML("beforebegin",'<input type="search" id="timezoneSearch" aria-label="Search time zones" placeholder="Search country, region or city…" autocomplete="off">');
  select.insertAdjacentHTML("afterend",'<small id="timezoneCount" aria-live="polite"></small>');
  let zones=[...select.options].map(option=>option.value), unsupported=new Set();
  function populateTimezones(){
    const selected=select.value || cp.state()?.settings.timezone || "UTC", query=$("#timezoneSearch").value.toLowerCase();
    const visible=zones.filter(zone=>zone===selected || zone.toLowerCase().replaceAll("_"," ").includes(query.replaceAll("_"," ")));
    select.innerHTML=visible.map(zone=>`<option value="${esc(zone)}" ${zone===selected ? "selected" : ""} ${unsupported.has(zone) ? "disabled" : ""}>${esc(zone)}${unsupported.has(zone) ? " — browser update required" : ""}</option>`).join("");
    $("#timezoneCount").textContent=query ? `${visible.length} matching entries (current selection retained)` : `${zones.length} global time-zone entries, including UTC and regional aliases`;
  }
  $("#timezoneSearch").addEventListener("input",populateTimezones);
  cp.api("/timezones.json").then(values=>{
    const saved=cp.state()?.settings.timezone || select.value || "UTC";
    zones=["UTC",...new Set([...values,saved].filter(value=>value!=="UTC"))].sort((a,b)=>a==="UTC" ? -1 : b==="UTC" ? 1 : a.localeCompare(b));
    unsupported=new Set(zones.filter(zone=>{try{new Intl.DateTimeFormat("en",{timeZone:zone});return false;}catch{return true;}}));
    populateTimezones();select.value=saved;
  }).catch(()=>{if(Intl.supportedValuesOf){zones=["UTC",...new Set([...zones,...Intl.supportedValuesOf("timeZone")])].filter((zone,index,all)=>all.indexOf(zone)===index);populateTimezones();}else $("#timezoneCount").textContent="Could not load the full time-zone catalog. Refresh to retry.";});
})();
