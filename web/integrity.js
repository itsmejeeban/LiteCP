(() => {
 'use strict';
 const cp=window.LiteCP,esc=cp.escapeHTML,page=document.createElement('section');
 page.className='page integrity-page';page.dataset.pageView='file-integrity';
 page.innerHTML=`<div class="page-heading"><div><h1>File Integrity Monitoring</h1><p>Review changes to critical server files. Monitored files are never modified by this tool.</p></div><button class="button button-secondary" data-refresh>Refresh</button></div><article class="panel"><div class="settings-body"><p>A baseline records SHA-256 hashes, file permissions and symbolic-link targets. Create it only after verifying that the server is trusted. This is change detection, not a malware verdict or a tamper-proof audit service.</p><p data-scope></p><p>Website content, databases and logs are excluded. Symbolic links are recorded without following them. Checks stop with an error at 50,000 entries, 512 MiB total or 128 MiB per file.</p><div class="feature-actions"><button class="button button-primary" data-baseline>Create trusted baseline</button><button class="button button-secondary" data-scan>Check now</button></div><form data-settings><label><input type="checkbox" name="enabled"> Enable scheduled monitoring</label><label>Check frequency<select name="interval"><option value="15">Every 15 minutes</option><option value="60">Hourly</option><option value="1440">Daily</option></select></label><button class="button button-secondary">Save schedule</button></form><p role="status" data-status></p><progress aria-label="Checking file integrity" hidden></progress><p data-summary></p><div class="table-scroll" data-report></div><div class="feature-actions"><button class="button button-secondary" data-prev>Previous</button><span data-count></span><button class="button button-secondary" data-next>Next</button></div></div></article>`;
 document.querySelector('.main-content').append(page);
 const nav=document.createElement('a');nav.className='nav-item';nav.href='#file-integrity';nav.dataset.page='file-integrity';nav.innerHTML='<img src="assets/sidebar/security.svg" alt="" width="20" height="20"><span>File Integrity Monitoring</span>';
 document.querySelector('[data-page="firewall"]').before(nav);nav.onclick=e=>{e.preventDefault();cp.navigate('file-integrity');};
 const $=selector=>page.querySelector(selector);let data=null,pending=false,timer=null,offset=0;
 function report(){
  const changes=data?.settings.changes||[],slice=changes.slice(offset,offset+50);
  $('[data-report]').innerHTML='<table class="data-table"><thead><tr><th>File / path</th><th>Change</th></tr></thead><tbody>'+slice.map(item=>`<tr><td><code>${esc(item.path)}</code></td><td>${esc(item.kind)}</td></tr>`).join('')+'</tbody></table>';
  $('[data-count]').textContent=changes.length?`${offset+1}–${Math.min(offset+50,changes.length)} of ${changes.length}`:'No file changes to display';$('[data-prev]').disabled=offset===0;$('[data-next]').disabled=offset+50>=changes.length;
 }
 function draw(){const s=data.settings;
  $('[data-scope]').textContent='Monitored paths: '+data.roots.join(', ');
  $('[data-settings]').elements.enabled.checked=s.enabled;$('[data-settings]').elements.interval.value=String(s.interval);
  $('[data-baseline]').textContent=s.baseline?'Approve current files as new baseline':'Create trusted baseline';
  $('[data-baseline]').disabled=s.running;$('[data-scan]').disabled=s.running||!s.baseline;$('[data-settings] button').disabled=s.running;
  $('progress').hidden=!s.running;
  $('[data-status]').textContent=s.running?'Integrity check is running in the background.':s.error?'Check incomplete: '+s.error:s.baseline?'Last completed check: '+cp.formatDateTime(s.checkedAt):'No trusted baseline yet.';
  $('[data-summary]').textContent=s.baseline?`Baseline: ${cp.formatDateTime(s.baselineAt)}. Latest completed report: ${(s.changes||[]).length} file changes. ${s.enabled?'Scheduled monitoring enabled.':'Scheduled monitoring disabled.'} Previous baselines and reports are retained on the server.`:'';
  report();
 }
 async function load(){if(pending)return;pending=true;clearTimeout(timer);$('[data-refresh]').disabled=true;
  try{data=await cp.api('/api/security/integrity');offset=Math.min(offset,Math.max(0,Math.floor(((data.settings.changes||[]).length-1)/50)*50));draw();}catch(error){$('[data-status]').textContent='Unable to load monitoring status: '+error.message;}finally{pending=false;$('[data-refresh]').disabled=false;if(location.hash==='#file-integrity')timer=setTimeout(load,data?.settings.running?3000:30000);}
 }
 async function action(body){$('[data-baseline]').disabled=true;$('[data-scan]').disabled=true;try{await cp.api('/api/security/integrity',{method:'POST',body});await load();}catch(error){$('[data-status]').textContent=error.message;if(data)draw();$('[data-status]').textContent=error.message;}}
 $('[data-baseline]').onclick=async()=>{if(await window.LiteCPDialogs.confirm('Trust the current critical server files? Inspect unexpected changes first. A new baseline will approve their current contents and enable scheduled checks. Previous baselines remain retained.'))action({action:'baseline',confirm:true});};
 $('[data-scan]').onclick=()=>action({action:'scan'});
 $('[data-settings]').onsubmit=e=>{e.preventDefault();const form=e.currentTarget;action({action:'settings',enabled:form.elements.enabled.checked,interval:Number(form.elements.interval.value)});};
 $('[data-refresh]').onclick=load;$('[data-prev]').onclick=()=>{offset=Math.max(0,offset-50);report();};$('[data-next]').onclick=()=>{offset+=50;report();};
 const previous=window.LiteCPFeatures.navigate;window.LiteCPFeatures.navigate=id=>{previous?.(id);if(id==='file-integrity')load();else clearTimeout(timer);};
 window.addEventListener('hashchange',()=>{if(location.hash==='#file-integrity')load();else clearTimeout(timer);});
 if(location.hash==='#file-integrity')cp.navigate('file-integrity',false);
})();
