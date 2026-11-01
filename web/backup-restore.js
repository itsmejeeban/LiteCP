(() => {
 'use strict';
 const cp=window.LiteCP,esc=cp.escapeHTML,page=document.querySelector('[data-page-view="backups"]');
 const panel=document.createElement('article');panel.id='backupRestoreInventory';panel.className='panel';
 panel.innerHTML='<div class="panel-head"><div><h2>Available backups</h2><p>Verified local website archives and recorded remote backups.</p></div><button type="button" class="button button-secondary" data-restore-refresh>Refresh</button></div><div class="settings-body"><p role="status" data-restore-status>Open Restore to check available backups.</p><div data-restore-warnings></div><div class="table-scroll" data-restore-list></div></div>';
 page.append(panel);let items=[],loading=false;
 async function load(){if(loading)return;loading=true;const status=panel.querySelector('[data-restore-status]');status.textContent='Checking local and remote storage...';try{
 const data=await cp.api('/api/backups/restorable');items=data.items||[];
 panel.querySelector('[data-restore-list]').innerHTML=items.length?'<table class="data-table"><thead><tr><th>Website / backup</th><th>Created</th><th>Size</th><th>Storage</th><th>Action</th></tr></thead><tbody>'+items.map((b,i)=>`<tr><td><strong>${esc(b.domain)}</strong><br><small>${esc(b.name)}</small></td><td>${esc(cp.formatDateTime(b.createdAt))}</td><td>${cp.formatBytes(b.size)}</td><td>${b.local?'Local':''}${b.local&&b.remote?' + ':''}${b.remote?'Remote: '+esc(b.destination):''}</td><td><div class="backup-restore-action"><button type="button" class="button button-primary" data-restore-index="${i}" >Restore</button>${b.websiteExists?'':'<small>Website will be created automatically</small>'}</div></td></tr>`).join('')+'</tbody></table>':'<p>No available restorable backups found.</p>';
 panel.querySelector('[data-restore-warnings]').textContent=(data.warnings||[]).join(' | ');status.textContent=items.length+' available backup(s). Remote checks cover backups recorded by this LiteCP installation.';
 }catch(error){status.textContent='Could not check backups: '+error.message;}finally{loading=false;}}
 const progress=document.createElement('dialog');progress.className='fm-dialog';document.body.append(progress);
 let running=false;
 async function restore(b){if(running)return;
 if(!await window.LiteCPDialogs.confirm(b.websiteExists===false?`Create ${b.domain} and restore this backup? If this domain now exists, its current data will be replaced after creating a recovery backup.`:`Restore ${b.domain} from this backup? Current website files and supported application databases will be replaced. A recovery backup is created first.`,{title:'Restore website',confirmText:'Restore and replace',danger:true}))return;
 running=true;progress.innerHTML='<header><h2>Restore progress</h2></header><div class="fm-dialog-body"><p role="status" data-progress>Requesting restore...</p><progress aria-label="Restore in progress"></progress><p data-result></p><button type="button" class="button button-secondary" data-close disabled>Close</button></div>';progress.showModal();progress.querySelector('[data-close]').onclick=()=>progress.close();
 try{
 let job=await cp.api('/api/backups/restore-direct',{method:'POST',body:{id:b.id,siteID:b.siteId,kind:b.kind||'history',confirm:true,confirmDomain:b.domain}});
 while(['queued','running','review','restoring','committing'].includes(job.status)){
 progress.querySelector('[data-progress]').textContent=(job.stage||'restore')+': '+job.message;
 await new Promise(resolve=>setTimeout(resolve,2500));job=await cp.api('/api/backups/restore-direct?id='+encodeURIComponent(job.id));
 }
 progress.querySelector('[data-progress]').textContent=job.message;
 const complete=job.status==='restored-review';progress.querySelector('[data-result]').textContent=complete?'Restore finished. Verify the website before use.':'Restore did not complete. Existing data and recovery files were retained.';
 cp.showToast(complete?'Restore finished':'Restore requires attention',job.message);await cp.refresh();
 }catch(error){progress.querySelector('[data-result]').textContent=error.message;cp.handleError(error);}finally{running=false;progress.querySelector('progress').remove();progress.querySelector('[data-close]').disabled=false;}}
 progress.addEventListener('cancel',event=>{if(running)event.preventDefault();});
 panel.addEventListener('click',event=>{if(event.target.closest('[data-restore-refresh]'))load();const button=event.target.closest('[data-restore-index]');if(button&&items[Number(button.dataset.restoreIndex)])restore(items[Number(button.dataset.restoreIndex)]);});
 // History and website ZIP actions use the same focused restore flow.
 window.LiteCPBackupRestore=restore;
 const previous=window.LiteCPFeatures.navigate;window.LiteCPFeatures.navigate=function(...args){previous?.apply(this,args);if(location.hash==='#backups/restore')load();};
 window.addEventListener('hashchange',()=>{if(location.hash==='#backups/restore')load();});if(location.hash==='#backups/restore')load();
})();
