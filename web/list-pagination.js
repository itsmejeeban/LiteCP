/* Pagination for client-rendered lists. Existing server/native pagers retain ownership. */
(() => {
 "use strict";
 const native = '#sitesTable,#extensionsTable,[data-delivery-table],[data-page-view="system-updates"],#trashPanel';
 const cards = '[data-backup-list],[data-ftp-list],[data-cron-list],[data-snapshot-list],[data-schedule-list],#liveJobs,#migrationStages';
 const states = new WeakMap();
 const ranges = (count, page, size) => {
  const pages = Math.max(1, Math.ceil(count / size));
  page = Math.max(1, Math.min(page, pages));
  return {page, pages, start:(page-1)*size, end:Math.min(page*size,count)};
 };
 window.LiteCPListPagination = {ranges};
 function paginate(host, rows, anchor) {
  let state=states.get(host);
  if(!state){
   const nav=document.createElement('nav');nav.className='list-pagination';nav.setAttribute('aria-label','List pages');
   nav.innerHTML='<label>Per page <select aria-label="Items per page"><option>10</option><option>25</option><option>50</option><option>100</option></select></label><span role="status" aria-live="polite"></span><div><button type="button" class="button button-secondary" data-page-prev>Previous</button><button type="button" class="button button-secondary" data-page-next>Next</button></div>';
   state={page:1,size:10,nav};states.set(host,state);
   nav.querySelector('select').onchange=e=>{state.size=Number(e.target.value);state.page=1;scan();};
   nav.querySelector('[data-page-prev]').onclick=()=>{state.page--;scan();};
   nav.querySelector('[data-page-next]').onclick=()=>{state.page++;scan();};
  }
  const {nav}=state;
  if(anchor.nextElementSibling!==nav)anchor.after(nav);
  nav.hidden=!rows.length;
  const range=ranges(rows.length,state.page,state.size);state.page=range.page;
  rows.forEach((row,index)=>row.classList.toggle('list-page-hidden',index<range.start||index>=range.end));
  const label=`${rows.length?range.start+1:0}–${range.end} of ${rows.length} · Page ${range.page} of ${range.pages}`;
  const status=nav.querySelector('[role="status"]');if(status.textContent!==label)status.textContent=label;
  nav.querySelector('[data-page-prev]').disabled=range.page===1;
  nav.querySelector('[data-page-next]').disabled=range.page===range.pages;
 }
 function scan(){
  document.querySelectorAll('table.data-table,table.fm-table,table.fm-search-table').forEach(table=>{
   if(table.closest(native)||!table.tHead)return;
   const rows=Array.from(table.tBodies).flatMap(body=>Array.from(body.rows)).filter(row=>!(row.cells.length===1&&row.cells[0].colSpan>1));
   paginate(table.parentElement,rows,table);
  });
  document.querySelectorAll(cards).forEach(host=>{
   const rows=Array.from(host.children).filter(row=>!row.hidden&&!row.classList.contains('hidden')&&row.matches('.website-backup-row,.ftp-account,.website-cron-job,.snapshot-row,.panel,.live-job,button'));
   paginate(host,rows,host);
  });
 }
 let scheduled=false;
 new MutationObserver(records=>{
  if(!records.some(record=>!record.target.closest?.('.list-pagination')))return;
  if(scheduled)return;scheduled=true;
  requestAnimationFrame(()=>{scheduled=false;scan();});
 }).observe(document.body,{childList:true,subtree:true});
 document.addEventListener('input',event=>{
  if(!event.target.matches('input[type="search"]'))return;
  const panel=event.target.closest('.panel,.page,dialog');
  panel?.querySelectorAll('table.data-table,'+cards).forEach(node=>{
   const state=states.get(node.matches('table')?node.parentElement:node);if(state)state.page=1;
  });
  requestAnimationFrame(scan);
 });
 scan();
})();
