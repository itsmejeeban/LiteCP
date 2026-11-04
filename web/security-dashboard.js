(() => {
 'use strict';
 const cp=window.LiteCP,esc=cp.escapeHTML,page=document.createElement('section');
 page.className='page';page.dataset.pageView='security-dashboard';
 page.innerHTML='<div class="page-heading"><div><h1>Security Dashboard</h1><p>Observed protections and items that need attention.</p></div><button class="button button-secondary" data-security-refresh>Refresh</button></div><p class="feature-note">An active service is not proof that the server is secure. Unknown checks need verification.</p><p role="status" data-security-status></p><div class="security-posture-grid" data-security-checks></div>';
 document.querySelector('.main-content').append(page);
 const link=document.createElement('a');link.className='nav-item';link.dataset.page='security-dashboard';link.href='#security-dashboard';
 link.innerHTML='<img src="assets/sidebar/security.svg" alt="" width="20" height="20"><span>Security Dashboard</span>';
 document.querySelector('[data-page="firewall"]').before(link);link.onclick=event=>{event.preventDefault();cp.navigate('security-dashboard');};
 let repair=null,repairTimer=null,checksCache=[];
 function repairHTML(value){
  if(!value)return '<p>SELinux repair status is unavailable. Refresh to check again.</p>';
  const running=value.state==='running';
  const waiting=value.retryAt&&!value.canEnable&&!running;
  const seconds=waiting?Math.max(0,Math.ceil((new Date(value.retryAt).getTime()-Date.now())/1000)):0;
  const retryLabel=`Retry available in ${Math.floor(seconds/60)}m ${seconds%60}s`;
  const note=value.mode==='Disabled'?'SELinux is disabled at boot. Enable it and relabel the filesystem through OS maintenance first. This button will not reboot the server.':value.mode==='Unknown'?'Unable to verify SELinux mode. Inspect the server before changing enforcement.':'';
  return `<div class="selinux-repair"><p>Current mode: ${esc(value.mode)}</p>${value.canEnable?'<button class="button button-primary" data-selinux-enable>Enable SELinux</button>':waiting?`<button class="button button-primary" disabled>${esc(retryLabel)}</button>`:''}${running?'<progress aria-label="Enabling SELinux"></progress>':''}<p role="status">${esc(waiting?'Waiting for the previous rollback safety window to finish. The retry button will become available automatically.':note||value.message||'Guarded enablement backs up the configuration and rolls back if service checks fail.')}</p>${value.log?`<details><summary>View repair log</summary><pre>${esc(value.log)}</pre></details>`:''}</div>`;
 }
 function render(){page.querySelector('[data-security-checks]').innerHTML=checksCache.map(check=>{const type=['critical','warning','good','info','unknown'].includes(check.status)?check.status:'unknown';return `<article class="panel security-check security-check-${type}"><div class="settings-body"><div class="security-check-heading"><h2>${esc(check.label)}</h2><span class="badge">${esc({critical:'Action required',warning:'Needs review',good:'Observed',info:'Information',unknown:'Not verified'}[type])}</span></div><p>${esc(check.detail)}</p>${check.id==='selinux'?repairHTML(repair):''}</div></article>`;}).join('');
  const enable=page.querySelector('[data-selinux-enable]');if(enable)enable.onclick=enableSELinux;
 }
 function scheduleRepair(){clearTimeout(repairTimer);if((repair?.state==='running'||repair?.retryAt)&&location.hash==='#security-dashboard')repairTimer=setTimeout(pollRepair,3000);}
 async function pollRepair(){try{repair=await cp.api('/api/security/selinux');render();if(repair.state!=='running'&&!repair.retryAt){await load();return;}}catch(error){page.querySelector('[data-security-status]').textContent='Unable to refresh repair status. '+error.message;}scheduleRepair();}
 async function enableSELinux(){
  if(!await window.LiteCPDialogs.confirm('Enable SELinux enforcement? LiteCP will back up the configuration, check active services and roll back if verification fails. Application-specific policy compatibility still requires testing.'))return;
  const button=page.querySelector('[data-selinux-enable]');if(button)button.disabled=true;
  try{repair=await cp.api('/api/security/selinux',{method:'POST',body:{confirm:true}});render();scheduleRepair();}catch(error){page.querySelector('[data-security-status]').textContent='SELinux enablement failed. '+error.message;if(button)button.disabled=false;}
 }
 let pending=null,generation=0;
 function load(){if(pending)return pending;const request=++generation,status=page.querySelector('[data-security-status]'),button=page.querySelector('button');status.textContent='Checking security status…';button.disabled=true;page.querySelector('[data-security-checks]').replaceChildren();
  pending=(async()=>{try{const value=await cp.api('/api/security/posture');if(request!==generation)return;
   checksCache=value.checks;try{repair=await cp.api('/api/security/selinux');}catch{repair=null;}render();scheduleRepair();status.textContent='Checked '+cp.formatDateTime(value.checkedAt);
  }catch(error){status.textContent='Unable to check security status. '+error.message;}finally{button.disabled=false;pending=null;}})();return pending;
 }
 page.querySelector('button').onclick=load;
 const previous=window.LiteCPFeatures.navigate;window.LiteCPFeatures.navigate=id=>{previous?.(id);if(id==='security-dashboard')load();};
 window.addEventListener('hashchange',()=>{if(location.hash==='#security-dashboard')load();});
 if(location.hash==='#security-dashboard')cp.navigate('security-dashboard',false);
})();
