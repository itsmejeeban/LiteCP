(() => {
  "use strict";
  const cp=window.LiteCP,esc=cp.escapeHTML,card=document.createElement("article");
  card.className="panel overview-environment";
  card.innerHTML='<div class="panel-head"><div><h2>Server environment</h2><p>Installed hosting software and runtimes.</p></div><button type="button" class="button button-secondary" data-environment-refresh>Refresh</button></div><div class="settings-body"><dl class="environment-summary"></dl><div class="environment-software"></div><p class="feature-note" data-environment-status role="status"></p></div>';
  document.querySelector('[data-page-view="dashboard"] .metrics-grid').after(card);
  let loaded=0,pending=null;
  function environmentStatus(value){
    const text=String(value).trim();
    if(!text||text==='-'||/^Checking/i.test(text))return '';
    return /\b(not|none|no|unknown|unavailable|unable|disabled|inactive|stopped|failed|error|dead|missing|unsupported)\b/i.test(text)?'environment-error':'environment-good';
  }
  const row=(name,value,status=["Operating system","Hostname","Server IP"].includes(name)?"environment-info":environmentStatus(value))=>`<div><dt>${esc(name)}</dt><dd class="${status}">${esc(value)}</dd></div>`;
  function summary(state){const system=state?.system||{};card.querySelector('.environment-summary').innerHTML=row('Operating system',system.os||'Checking…')+row('Hostname',system.hostname||'—')+row('Server IP',system.ipAddress||'—')+row('Installed PHP versions',(system.phpVersions||[]).map(version=>'PHP '+version).join(' · ')||'None detected')+row('Database engine',system.databaseEngine||'Not detected')+row('Firewall',system.firewallBackend||'Not detected')+row('Web application firewall',system.wafEngine||'Not detected')+(system.services||[]).map(service=>row(service.label,service.state,["active","running"].includes(String(service.state).toLowerCase())?"environment-good":"environment-error")).join('')+row('Antivirus scanner',system.antivirusAvailable?'Installed':'Not detected')+row('Webmail',system.webmailAvailable?'Available':'Not configured');}
  async function load(force=false){
    const state=cp.state();if(!state)return;summary(state);if(pending)return pending;if(!force&&Date.now()-loaded<60000)return;
    const status=card.querySelector('[data-environment-status]'),button=card.querySelector('button');button.disabled=true;status.textContent='Checking installed software…';
    pending=(async()=>{try{const result=await cp.api('/api/environment-inventory');
      card.querySelector('.environment-software').innerHTML='<h3>Installed software</h3><dl class="environment-summary">'+result.software.map(item=>row(item.name,item.version,item.installed?"environment-good":"environment-error")).join('')+'</dl><h3>Application hosting</h3><dl class="environment-summary">'+Object.entries({node:'Node.js',python:'Python',ruby:'Ruby'}).map(([key,name])=>row(name,(result.enabled?.[key]?'Enabled':'Disabled')+' · '+(result.imageStatus==='Checked'?(result.images?.[key]?.length?'Installed container versions: '+result.images[key].join(', '):'No runtime image installed'):'Image detection unavailable'))).join('')+'</dl>';
      status.textContent='Host runtimes and installed hosting containers are listed separately. Additional versions download when an application starts.';loaded=Date.now();
    }catch(error){status.textContent='Unable to check installed software. '+error.message;}finally{button.disabled=false;pending=null;}})();return pending;
  }
  card.querySelector('button').onclick=()=>load(true);
  const render=window.LiteCPFeatures.render;window.LiteCPFeatures.render=state=>{render(state);summary(state);if(location.hash==='#dashboard'||!location.hash)load();};
  const navigate=window.LiteCPFeatures.navigate;window.LiteCPFeatures.navigate=id=>{navigate?.(id);if(id==='dashboard')load();};
  window.addEventListener('hashchange',()=>{if(location.hash==='#dashboard')load();});
  if(location.hash==='#dashboard'||!location.hash)load();
})();
