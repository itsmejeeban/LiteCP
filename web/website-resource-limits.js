(() => {
 'use strict';
 const cp=window.LiteCP,esc=cp.escapeHTML,page=document.createElement('section');
 page.className='page';page.dataset.pageView='website-resource-limits';
 if(location.hash==='#website-resource-limits')page.classList.add('active');
 page.innerHTML=`<div class="page-heading"><h1>Resource Limit</h1></div><article class="panel"><form class="settings-body website-tool-picker" data-picker><label class="field"><span>Website</span><select required aria-label="Website for resource limits"></select></label><button class="button button-primary" data-create-one>Create resource limit</button><button type="button" class="button button-secondary" data-create-all>Create resource limits for all websites</button><button type="button" class="button button-danger" data-remove-all>Remove resource limits from all websites</button></form><form class="settings-body" data-editor hidden><h2 data-editor-title></h2><p data-bulk-details hidden>These values apply to websites without saved limits. Websites with saved limits use their existing values. LiteCP attempts to enable enforcement for each website.</p><div data-site-details><p data-capabilities role="status"></p><label class="resource-enforcement-toggle"><input name="enabled" type="checkbox"><span>Enable resource enforcement</span></label><p>Enabled native PHP/static websites use a dedicated runtime. CPU exhaustion for 10 seconds, memory OOM or disk exhaustion blocks only this website. Recovery retries every five minutes. Unsupported runtimes remain unavailable.</p><p data-runtime-status role="status"></p><details><summary>Violation history</summary><ul data-history></ul></details></div><div class="form-grid"><label class="field">CPU (% of one core)<input name="cpuPercent" type="number" min="1" max="102400" step="0.1" value="100" required></label><label class="field">Memory (MiB)<input name="memoryMiB" type="number" min="32" max="1073741824" value="512" required></label><label class="field">Disk (MiB)<input name="diskMiB" type="number" min="1" max="1099511627776" value="10240" required></label></div><details><summary>Advanced settings</summary><div class="form-grid"><label class="field">Process limit<input name="tasks" type="number" min="8" max="1048576" value="64" required></label><label class="field">Concurrent requests<input name="concurrentRequests" type="number" min="1" max="65535" value="8" required></label><label class="field">Read limit (MiB/s; 0 = unrestricted)<input name="readMiBPerSecond" type="number" min="0" value="0" required></label><label class="field">Write limit (MiB/s; 0 = unrestricted)<input name="writeMiBPerSecond" type="number" min="0" value="0" required></label></div></details><p>Disk allowance includes all website-home folders, local backups, Trash and protected runtime temporary files. Shared MySQL, mail and FTP processing are outside CPU and memory isolation. Existing application-container safety caps remain in place. Disable enforcement before editing allowance values, then enable it again.</p><div class="form-actions"><button class="button button-primary" data-save>Create resource limit</button><button type="button" class="button button-secondary" data-disable hidden>Disable limits</button><button type="button" class="button button-secondary" data-cancel>Cancel</button></div></form><p class="settings-body" data-status role="status"></p></article>`;
 page.insertAdjacentHTML('beforeend',`<article class="panel"><div class="panel-head"><h2>Website resource limits</h2><button type="button" class="button button-secondary" data-refresh>Refresh</button></div><div class="settings-body"><p data-live-status role="status"></p><label class="field" style="max-width:420px"><span>Search resource limits</span><input type="search" data-resource-search placeholder="Search by domain" autocomplete="off" aria-label="Search resource limits by domain"></label><p>Observed website-owned processes only; shared services and remapped container users are excluded. CPU and memory refresh every 3 seconds; disk every 60 seconds. Enabled rows use aggregate cgroup counters and project-quota usage. Disabled drafts show observed website-owned processes only.</p><div class="table-scroll"><table class="data-table"><thead><tr><th>Website</th><th scope="col" data-sort-column="status"><button type="button" data-sort-key="status">Status <span aria-hidden="true">&#8597;</span></button></th><th scope="col" data-sort-column="cpuPercent"><button type="button" data-sort-key="cpuPercent" title="Sort by current CPU usage" aria-label="Sort by current CPU usage">CPU / limit <span aria-hidden="true">&#8597;</span></button></th><th scope="col" data-sort-column="memoryMiB"><button type="button" data-sort-key="memoryMiB" title="Sort by current memory usage" aria-label="Sort by current memory usage">Memory / limit <span aria-hidden="true">&#8597;</span></button></th><th scope="col" data-sort-column="diskMiB"><button type="button" data-sort-key="diskMiB" title="Sort by current disk space used" aria-label="Sort by current disk space used">Disk / limit <span aria-hidden="true">&#8597;</span></button></th><th scope="col" data-sort-column="tasks"><button type="button" data-sort-key="tasks" title="Sort by current process count" aria-label="Sort by current process count">Processes / limit <span aria-hidden="true">&#8597;</span></button></th><th>Action</th></tr></thead><tbody data-resources></tbody></table></div><div class="button-row"><label>Per page <select data-page-size><option>10</option><option>25</option><option>50</option><option>100</option></select></label><button type="button" class="button button-secondary" data-previous>Previous</button><span data-count></span><button type="button" class="button button-secondary" data-next>Next</button></div></div></article>`);
 page.insertAdjacentHTML('beforeend',`<article class="panel" data-usage-panel hidden><div class="panel-head"><h2 data-usage-title></h2><button type="button" class="button button-secondary" data-usage-close>Close</button></div><div class="settings-body"><label class="field">History range<select data-usage-range><option value="minute">Minute-wise · Last hour</option><option value="hour">Hour-wise · Last 24 hours</option><option value="day">Day view · Last 24 hours by minute</option><option value="week">Week view · Daily peaks</option><option value="month">Month view · Daily peaks</option></select></label><p data-usage-status role="status"></p><div class="form-grid" data-usage-charts></div></div></article>`);
 document.querySelector('.main-content').append(page);
 const link=document.createElement('a');link.className='nav-item';link.dataset.page='website-resource-limits';link.href='#website-resource-limits';link.innerHTML='<img src="assets/sidebar/services.svg" width="20" height="20" alt=""><span>Resource Limit</span>';link.onclick=e=>{e.preventDefault();cp.navigate('website-resource-limits')};document.querySelector('.nav').append(link);
 const picker=page.querySelector('[data-picker]'),select=picker.querySelector('select'),editor=page.querySelector('[data-editor]'),status=page.querySelector('[data-status]');let generation=0,active='',savedEnabled=false,bulk=false,busy=false;
 function runtimeStatus(row){const state=row.state||'Disabled',parts=[state];if(state==='Resource blocked'&&row.reason)parts.push('Cause: '+row.reason);if(row.error)parts.push(row.error);if((state==='Resource blocked'||state==='Recovering')&&row.nextRecovery&&new Date(row.nextRecovery).getTime()>0)parts.push('Next recovery '+new Date(row.nextRecovery).toLocaleString());return parts.join(' · ');}
 const limitsURL='/api/sites/resource-limits';
 const fields=['cpuPercent','memoryMiB','diskMiB','tasks','concurrentRequests','readMiBPerSecond','writeMiBPerSecond'];
 function render(){
  const old=select.value,sites=cp.state()?.sites||[];
  select.innerHTML=sites.map(site=>`<option value="${esc(site.id)}">${esc(site.domain)}</option>`).join('');
  if(sites.some(site=>site.id===old))select.value=old;
  for(const name of ['[data-create-one]','[data-create-all]','[data-remove-all]'])page.querySelector(name).disabled=busy||!sites.length;
  if(active&&!sites.some(site=>site.id===active))closeEditor();
 }
 function closeEditor(){generation++;active='';bulk=false;editor.hidden=true;}
 function showEditor(title,isBulk,settings){
  bulk=isBulk;
  editor.reset();
  for(const key of fields)if(settings?.[key]!=null)editor.elements[key].value=settings[key];
  editor.elements.enabled.checked=!!settings?.enabled;
  savedEnabled=editor.elements.enabled.checked;
  editor.elements.enabled.disabled=isBulk;
  $('[data-editor-title]').textContent=title;
  $('[data-site-details]').hidden=isBulk;
  $('[data-bulk-details]').hidden=!isBulk;
  $('[data-disable]').hidden=!settings?.enabled;
  $('[data-save]').textContent=isBulk?'Create and enable limits':settings?'Save changes':'Create resource limit';
  editor.hidden=false;
  status.textContent='';
 }
 select.onchange=()=>{if(!bulk&&!busy){closeEditor();status.textContent='';}};
 async function openEditor(){
  const site=cp.state()?.sites?.find(item=>item.id===select.value);
  if(busy||!site)return;
  closeEditor();
  const seq=generation;
  status.textContent='Checking capabilities…';
  try{
   const data=await cp.api(limitsURL+'?siteId='+encodeURIComponent(site.id));
   if(seq!==generation)return;
   active=site.id;
   showEditor((data.settings?'Edit resource limit for ':'Create resource limit for ')+site.domain,false,data.settings);
   editor.elements.enabled.disabled=!data.capabilities.ready&&!data.settings?.enabled;
   $('[data-capabilities]').textContent=data.capabilities.ready?'Native PHP/static enforcement is available.':data.capabilities.prerequisites.join('. ');
   $('[data-runtime-status]').textContent=runtimeStatus({...data,reason:data.usage?.Reason,error:data.usage?.error});
   $('[data-history]').innerHTML=(data.history||[]).map(row=>`<li>${esc(row.at)} — ${esc(row.event)} ${esc(row.reason||'')}</li>`).join('')||'<li>No recorded violations.</li>';
  }catch(error){if(seq===generation){if(error.status===401)handleExpiredSession();else status.textContent=error.message;}}
 }
 picker.onsubmit=e=>{e.preventDefault();openEditor()};
 page.querySelector('[data-create-all]').onclick=()=>{
  if(busy||!cp.state()?.sites?.length)return;
  closeEditor();
  showEditor('Create resource limits for all websites',true);
 };
 async function applyToAll(defaults){
  const sites=[...(cp.state()?.sites||[])];
  const removing=defaults===null,verb=removing?'Removing':'Enabling';
  let changed=0,skipped=0,remaining=0,blocked=0,recoveries=0;
  const failures=[];
  for(const [index,site] of sites.entries()){
   status.textContent=`${verb} limits for ${index+1} of ${sites.length} · ${site.domain}`;
   const url=limitsURL+'?siteId='+encodeURIComponent(site.id);
   try{
    const current=await cp.api(url);
    if(removing&&!current.settings){
     skipped++;
     continue;
    }
    if(!removing&&current.settings?.enabled){
     if(current.enforced){
      skipped++;
      if(current.state==='Resource blocked')blocked++;
     }else{
      await cp.api(url,{method:'POST',body:{}});
      recoveries++;
     }
     continue;
    }
    if(removing){
     if(current.settings.enabled)await cp.api(url,{method:'PUT',body:{...current.settings,enabled:false}});
     await cp.api(url,{method:'DELETE',body:{confirm:true}});
    }else{
     const result=await cp.api(url,{method:'PUT',body:{...(current.settings||defaults),enabled:true}});
     if(!result.enforced||result.state!=='Active')throw new Error(runtimeStatus(result));
    }
    changed++;
   }catch(error){
    failures.push(site.domain+' — '+error.message);
    if(error.status===401){remaining=sites.length-index-1;handleExpiredSession();break;}
   }
  }
  status.textContent=`${removing?'Removed':'Enabled'}: ${changed} · Skipped: ${skipped} · Failed: ${failures.length}`
   +(blocked?` · Already enforced but blocked: ${blocked}`:'')
   +(recoveries?` · Recovery requested: ${recoveries}`:'')
   +(remaining?` · Unattempted: ${remaining}`:'')+(failures.length?' · '+failures.join(' | '):'');
  listPage=1;
 }
 page.querySelector('[data-remove-all]').onclick=async()=>{
  if(busy||!cp.state()?.sites?.length)return;
  busy=true;
  render();
  try{
   if(!await window.LiteCPDialogs.confirm('Disable enforcement and remove saved resource limits for every website?',{title:'Remove all resource limits',confirmText:'Remove all resource limits',danger:true}))return;
   closeEditor();
   await applyToAll(null);
   if(listRequest)await listRequest;
   await loadList();
  }catch(error){status.textContent=error.message;}
  finally{busy=false;render();renderList();}
 };
 page.querySelector('[data-disable]').onclick=()=>{editor.elements.enabled.checked=false;editor.onsubmit({preventDefault(){}})};
 page.querySelector('[data-cancel]').onclick=closeEditor;
 editor.onsubmit=async e=>{
  e.preventDefault();
  if(busy||(!bulk&&!active))return;
  busy=true;
  select.disabled=true;
  const buttons=page.querySelectorAll('button');
  buttons.forEach(button=>button.disabled=true);
  const body={enabled:!bulk&&editor.elements.enabled.checked};
  for(const key of fields)body[key]=Number(editor.elements[key].value);
  try{
   if(bulk){
    await applyToAll(body);
   }else{
    const result=await cp.api(limitsURL+'?siteId='+encodeURIComponent(active),{method:'PUT',body});
    if(!body.enabled)status.textContent='Resource settings saved. Enforcement is disabled.';
    else if(result.state==='Resource blocked')status.textContent='Settings retained. Website remains resource blocked; use Retry now for recovery.';
    else if(result.enforced&&result.state==='Active')status.textContent='Resource limits applied and verified.';
    else status.textContent='Resource settings saved. '+runtimeStatus(result);
   }
   closeEditor();
   if(listRequest)await listRequest;
   await loadList();
  }catch(error){status.textContent=error.message;}
  finally{
   busy=false;
   select.disabled=false;
   buttons.forEach(button=>button.disabled=false);
   render();
   renderList();
  }
 };
 let items=[],listPage=1,listRequest=null,timer=null;
 const $=selector=>page.querySelector(selector);
 let sessionExpired = false;
 function handleExpiredSession() {
  sessionExpired = true;
  clearTimeout(timer);
  closeEditor();
  items = [];
  renderList();
  $('[data-refresh]').disabled = true;
  $('[data-live-status]').textContent = 'Your session has expired. Sign in again to load current limits.';
 }
 const visible=()=>location.hash==='#website-resource-limits'&&!document.hidden;
 let sortKey = '', sortDirection = 1;
 const search = $('[data-resource-search]');
 search.value='';
 search.oninput=()=>{listPage=1;renderList();};
 const pageSize = $('[data-page-size]');
 pageSize.onchange = () => { listPage = 1; renderList(); };
 const sortKeys = ['status', 'cpuPercent', 'memoryMiB', 'diskMiB', 'tasks'];
 const usageSortFields = {cpuPercent:'cpuPercent', memoryMiB:'memoryBytes', diskMiB:'diskBytes', tasks:'processes'};
 function usageSortValue(row, key) {
  const usage = row.usage || {};
  // Disk collection is independent of process sampling. Missing samples must
  // stay at the end in either direction, while a measured zero remains valid.
  if (key !== 'diskMiB' && !usage.available) return null;
  const value = usage[usageSortFields[key]];
  return Number.isFinite(value) ? value : null;
 }
 for (const key of sortKeys) {
  $('[data-sort-key="' + key + '"]').onclick = () => {
   sortDirection = sortKey === key ? -sortDirection : (key === 'status' ? 1 : -1);
   sortKey = key;
   for (const column of sortKeys) {
    const header = $('[data-sort-column="' + column + '"]');
    const icon = $('[data-sort-key="' + column + '"] span');
    if (column === key) header.setAttribute('aria-sort', sortDirection === 1 ? 'ascending' : 'descending');
    else header.removeAttribute('aria-sort');
    icon.textContent = column !== key ? '\u2195' : sortDirection === 1 ? '\u2191' : '\u2193';
   }
   listPage = 1;
   renderList();
  };
 }
 function renderList(){
  const size = Number(pageSize.value);
  const key = sortKey;
  const query=search.value.trim().toLowerCase();
  const filtered=items.filter(row=>row.domain.toLowerCase().includes(query));
  const ordered = key ? [...filtered].sort((a, b) => {
   let comparison;
   if (key === 'status') comparison = (a.state || 'Disabled').localeCompare(b.state || 'Disabled');
   else {
    const left = usageSortValue(a, key), right = usageSortValue(b, key);
    if (left == null && right != null) return 1;
    if (right == null && left != null) return -1;
    comparison = left == null ? 0 : left - right;
   }
   return sortDirection * comparison || a.domain.localeCompare(b.domain);
  }) : filtered;
  const pages=Math.max(1,Math.ceil(filtered.length/size));listPage=Math.max(1,Math.min(listPage,pages));
  $('[data-resources]').innerHTML=ordered.slice((listPage-1)*size,listPage*size).map(row=>{const u=row.usage||{},p=row.settings;return `<tr><td>${esc(row.domain)}<br><small>Requests: ${p.concurrentRequests}; I/O read/write: ${p.readMiBPerSecond||'Unlimited'} / ${p.writeMiBPerSecond||'Unlimited'} MiB/s</small></td><td>${esc(runtimeStatus(row))}<br><small>${row.enforced?'Kernel enforcement verified':p.enabled?'Enforcement unavailable':'Enforcement off'}</small></td><td>${u.cpuPercent==null?(u.available?'Sampling…':'Unavailable'):Number(u.cpuPercent).toFixed(1)+'%'} / ${p.cpuPercent}%</td><td>${u.available?esc(cp.formatBytes(u.memoryBytes)):'Unavailable'} / ${esc(cp.formatBytes(p.memoryMiB*1048576))}</td><td>${u.diskBytes==null?'Unavailable / pending':esc(cp.formatBytes(u.diskBytes))} / ${esc(cp.formatBytes(p.diskMiB*1048576))}</td><td>${u.available?u.processes:'Unavailable'} / ${p.tasks}</td><td><button type="button" class="button button-secondary" data-edit-resource="${esc(row.id)}">Edit</button> <button type="button" class="button button-secondary" data-usage-open="${esc(row.id)}">History</button> <button type="button" class="button button-danger" data-remove-resource="${esc(row.id)}">Remove</button>${p.enabled&&(row.state==='Resource blocked'||String(row.state).startsWith('Error'))?` <button type="button" class="button button-secondary" data-retry-resource="${esc(row.id)}">Retry now</button>`:''}</td></tr>`}).join('')||(sessionExpired?'':'<tr><td colspan="7">No resource limits created. Choose a website above to create its settings.</td></tr>');
  $('[data-count]').textContent=`Page ${listPage} of ${pages} · ${filtered.length} websites`;
  $('[data-previous]').disabled=busy||listPage<=1;$('[data-next]').disabled=busy||listPage>=pages;
 }
 function loadList(){
  if(sessionExpired||listRequest)return listRequest;
  listRequest=cp.api(limitsURL).then(data=>{
   items=data.items||[];
   renderList();
   if(active&&!editor.hidden){
    const current=items.find(row=>row.id===active);
    if(current&&current.settings.enabled===savedEnabled)$('[data-runtime-status]').textContent=runtimeStatus(current);
   }
   $('[data-live-status]').textContent=(data.note?data.note+' ':'')+'Updated '+new Date(data.updatedAt).toLocaleTimeString();
  }).catch(error=>{
   if(error.status===401)handleExpiredSession();
   else $('[data-live-status]').textContent='Unable to refresh usage: '+error.message;
  }).finally(()=>{
   listRequest=null;
   clearTimeout(timer);
   if(!sessionExpired&&visible())timer=setTimeout(loadList,3000);
  });
  return listRequest;
 }
 let historySite='',historyGeneration=0,historyTimer;
 function usageChart(points,key,label,format){
  const available=points.filter(p=>Number.isFinite(p[key]));
  if(!available.length)return `<section><h3>${label}</h3><p>No samples yet.</p></section>`;
  const max=Math.max(1,...available.map(p=>p[key]));
  const x=i=>8+304*i/Math.max(1,points.length-1),y=v=>92-84*v/max;
  let path='',previous=false;
  const dots=points.map((p,i)=>{
   if(!Number.isFinite(p[key])){previous=false;return '';}
   path+=`${previous?'L':'M'}${x(i)},${y(p[key])} `;previous=true;
   return `<circle cx="${x(i)}" cy="${y(p[key])}" r="1.5" fill="currentColor"><title>${esc(new Date(p.at).toLocaleString())} · ${esc(format(p[key]))}</title></circle>`;
  }).join('');
  return `<section><h3>${label}</h3><p>Peak ${esc(format(Math.max(...available.map(p=>p[key]))))}</p><svg viewBox="0 0 320 100" width="100%" role="img" aria-label="${label} history"><path d="M8 92H312" stroke="currentColor" opacity=".3"/><path d="${path}" fill="none" stroke="currentColor"/>${dots}</svg><small>${esc(new Date(points[0].at).toLocaleString())} · ${esc(new Date(points.at(-1).at).toLocaleString())}</small></section>`;
 }
 async function loadUsageHistory(){
  clearTimeout(historyTimer);
  if(!historySite||!visible())return;
  const seq=++historyGeneration;
  try{
   const query=new URLSearchParams({siteId:historySite,range:$('[data-usage-range]').value});
   const data=await cp.api('/api/sites/resource-limits/history?'+query);
   if(seq!==historyGeneration)return;
   const points=data.points||[];
   $('[data-usage-charts]').innerHTML=usageChart(points,'cpuPercent','CPU (% of one core)',v=>v.toFixed(1)+'%')+usageChart(points,'memoryBytes','Memory',cp.formatBytes)+usageChart(points,'diskBytes','Disk',cp.formatBytes)+usageChart(points,'processes','Processes',String);
   $('[data-usage-status]').textContent='Recorded every minute. Longer buckets show peaks. Gaps indicate missing measurements.';
  }catch(error){if(seq===historyGeneration)$('[data-usage-status]').textContent=error.message;}
  finally{if(seq===historyGeneration&&visible()&&historySite)historyTimer=setTimeout(loadUsageHistory,15000);}
 }
 $('[data-usage-range]').onchange=loadUsageHistory;
 $('[data-usage-close]').onclick=()=>{historySite='';historyGeneration++;clearTimeout(historyTimer);$('[data-usage-panel]').hidden=true;};
 function activate(){clearTimeout(timer);clearTimeout(historyTimer);if(visible()){loadList();loadUsageHistory();}}
 $('[data-refresh]').onclick=loadList;
 $('[data-previous]').onclick=()=>{if(busy||listPage<=1)return;listPage--;renderList()};$('[data-next]').onclick=()=>{if(busy||listPage>=Math.max(1,Math.ceil(items.length/Number(pageSize.value))))return;listPage++;renderList()};
 $('[data-resources]').onclick=async e=>{
 const history=e.target.closest('[data-usage-open]');if(history){
  const row=items.find(item=>item.id===history.dataset.usageOpen);if(!row)return;
  historySite=row.id;$('[data-usage-title]').textContent='Resource history · '+row.domain;
  $('[data-usage-charts]').replaceChildren();$('[data-usage-status]').textContent='Loading history·';
  const panel=$('[data-usage-panel]');panel.hidden=false;panel.scrollIntoView({block:'nearest'});loadUsageHistory();return;
 }
 const retry=e.target.closest('[data-retry-resource]');if(retry){if(busy)return;busy=true;retry.disabled=true;try{await cp.api('/api/sites/resource-limits?siteId='+encodeURIComponent(retry.dataset.retryResource),{method:'POST',body:{}});status.textContent='Recovery requested. The monitor will verify health before opening access.';await loadList()}catch(error){cp.handleError(error)}finally{busy=false;retry.disabled=false;render()}return;}
 const remove=e.target.closest('[data-remove-resource]');
 if(remove){
  if(busy)return;const id=remove.dataset.removeResource,row=items.find(item=>item.id===id);if(!row)return;
  if(!await window.LiteCPDialogs.confirm(`Remove saved resource-limit settings for ${row.domain}? Website files, databases and services will be kept.`,{title:'Remove resource limit',confirmText:'Remove',danger:true}))return;
  busy=true;remove.disabled=true;
  try{await cp.api('/api/sites/resource-limits?siteId='+encodeURIComponent(id),{method:'DELETE',body:{confirm:true}});if(active===id)closeEditor();status.textContent='Resource settings removed.';await loadList();}catch(error){cp.handleError(error);}finally{busy=false;remove.disabled=false;render();}return;
 }
const button=e.target.closest('[data-edit-resource]');if(!button||busy)return;select.value=button.dataset.editResource;openEditor()};
 const previousNavigate=window.LiteCPFeatures.navigate;
 window.LiteCPFeatures.navigate=function(...args){previousNavigate?.apply(this,args);activate()};
 window.addEventListener('hashchange',activate);document.addEventListener('visibilitychange',activate);
 renderList();activate();
 const previous=window.LiteCPFeatures.render;window.LiteCPFeatures.render=state=>{previous(state);render();};render();
})();
