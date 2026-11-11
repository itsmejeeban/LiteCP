(() => {
 'use strict';
 const cp=window.LiteCP,esc=cp.escapeHTML,page=document.querySelector('[data-page-view="migrations"]');
 const panel=document.createElement('article');panel.className='panel';panel.hidden=true;
 panel.innerHTML='<div class="panel-head"><div><h2>Retained restore stages</h2><p>Inspected archives awaiting manual import or removal. These are separate from server-to-server migration jobs.</p></div><button type="button" class="button button-secondary" data-stage-refresh>Refresh stages</button></div><div class="settings-body" data-stage-list role="status"></div>';
 page.append(panel);let stages=[],pending=false;
 async function load(){if(pending)return;pending=true;const list=panel.querySelector('[data-stage-list]');list.textContent='Loading retained stages...';try{
  const result=await cp.api('/api/migrations');if(!Array.isArray(result.items))throw new Error('Invalid stages response. Refresh stages to retry.');stages=result.items;
  list.innerHTML=stages.length?stages.map((stage,index)=>'<div class="snapshot-row"><div><strong>'+esc(stage.platform)+' — '+esc(stage.id)+'</strong><p>'+esc(cp.formatDateTime(stage.createdAt))+' | '+esc(cp.formatBytes(stage.bytes))+' | '+esc(String(stage.files))+' files</p><small>'+esc((stage.roots||[]).join(', '))+'</small></div><div class="feature-actions"><button type="button" class="button button-secondary" data-stage-review="'+index+'">Review and import</button><button type="button" class="button button-danger" data-stage-remove="'+index+'">Delete stage</button></div></div>').join(''):'No retained restore stages.';
 }catch(error){list.textContent='Could not load retained stages: '+error.message;}finally{pending=false;}}
 panel.addEventListener('click',async event=>{
  if(event.target.closest('[data-stage-refresh]'))return load();
  const review=event.target.closest('[data-stage-review]'),remove=event.target.closest('[data-stage-remove]');if(pending||(!review&&!remove))return;
  const stage=stages[Number(review?review.dataset.stageReview:remove.dataset.stageRemove)];if(!stage)return;
  if(review){window.LiteCPFeatures.reviewMigration(stage);document.dispatchEvent(new Event('migration-show-review'));document.querySelector('#migrationImportForm').scrollIntoView({behavior:'smooth',block:'start'});return;}
  pending=true;try{
   if(!await window.LiteCPDialogs.confirm('Delete retained stage '+stage.id+' ('+stage.platform+')? Only this inspected archive and extracted staging files are removed. The original folder backup and website data are kept.',{title:'Delete retained stage',confirmText:'Delete stage',danger:true}))return;
   await cp.api('/api/migrations/'+encodeURIComponent(stage.id),{method:'DELETE'});
  }catch(error){cp.handleError(error);}finally{pending=false;await load();}
 });
 function sync(){panel.hidden=location.hash!=='#migrations/pending';if(!panel.hidden)load();}
 document.addEventListener('migration-section-change',sync);window.addEventListener('hashchange',sync);sync();
})();
