(() => {
 'use strict';
 const cp=window.LiteCP, esc=cp.escapeHTML;
 const container=document.createElement('section');
 container.className='overview-running-jobs';container.hidden=true;
 container.setAttribute('aria-label','Running background jobs');
 document.querySelector('[data-page-view="dashboard"] .metrics-grid').after(container);
 let pending=false;
 function render(items){
  container.hidden=!items.length;
  container.innerHTML=items.map(job=>{
   const measured=Number.isFinite(job.percent),percent=measured?Math.min(99,Math.max(0,job.percent)):null;
   return `<article class="panel overview-running-job"><div class="overview-job-heading"><h2>${esc(job.title)}</h2><a class="button button-secondary" href="#${esc(job.route)}">View details</a></div><p>${esc(job.message||job.stage||'Processing')}</p><div class="overview-job-progress${measured?'':' indeterminate'}" role="progressbar" aria-label="${esc(job.title)}" ${measured?`aria-valuenow="${percent}" aria-valuemin="0" aria-valuemax="100"`:'aria-valuetext="Processing; percentage unavailable"'}><span style="width:${measured?percent+'%':'35%'}"></span></div><small>${measured?`${esc(job.stage||'Transfer')}: ${percent}%`:`${esc(job.stage||'Processing')} — percentage unavailable for this stage`}</small></article>`;
  }).join('');
 }
 async function refresh(){
  if(pending||document.hidden||(location.hash&&location.hash!=='#dashboard'))return;
  pending=true;
  try{const result=await cp.api('/api/overview/jobs');render(result.items||[]);}
  catch{container.hidden=true;container.innerHTML='';}
  finally{pending=false;}
 }
 window.addEventListener('hashchange',refresh);
 document.addEventListener('visibilitychange',refresh);
 setInterval(refresh,5000);refresh();
})();
