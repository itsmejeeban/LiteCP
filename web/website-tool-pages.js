(() => {
  "use strict";
  const cp=window.LiteCP,esc=cp.escapeHTML;
  const tools=[
    {route:"website-backups",title:"Website Backups",attribute:"data-site-backup",marker:"[data-backup-list]",icon:"backups"},
    {route:"website-cron",title:"Cron Jobs",attribute:"data-site-cron-jobs",marker:"[data-cron-context]",icon:"activities"},
    {route:"website-ftp",title:"FTP",attribute:"data-site-ftp",marker:"[data-ftp-list]",icon:"migrations"}
  ];
  for(const tool of tools){
    const page=document.createElement('section');page.className='page website-tool-page';page.dataset.pageView=tool.route;
    page.innerHTML=`<div class="page-heading"><h1>${tool.title}</h1></div><article class="panel"><form class="settings-body website-tool-picker"><label class="field"><span>Website</span><select aria-label="Website for ${tool.title}" required></select></label><button type="submit" class="button button-primary">Open ${tool.title}</button></form></article><article class="panel" data-tool-all><div class="panel-head"><h2>All ${tool.title}</h2><button type="button" class="button button-secondary" data-tool-refresh>Refresh</button></div><div class="settings-body" data-tool-all-content role="status"></div></article><div data-tool-workspace></div>`;
    document.querySelector('.main-content').append(page);tool.page=page;
    const link=document.createElement('a');link.className='nav-item';link.dataset.page=tool.route;link.href='#'+tool.route;
    link.innerHTML=`<img src="assets/sidebar/${tool.icon}.svg" alt="" width="18" height="18"><span>${tool.title}</span>`;
    link.onclick=event=>{event.preventDefault();cp.navigate(tool.route);};document.querySelector('.nav').append(link);
    page.querySelector('form').onsubmit=event=>{
      event.preventDefault();const id=page.querySelector('select').value;if(!id){showAll(tool);return;}
      if(tool.dialog?.open)tool.dialog.close();
      const trigger=document.createElement('button');trigger.type='button';trigger.hidden=true;trigger.setAttribute(tool.attribute,id);
      const table=document.querySelector('#sitesTable');table.append(trigger);trigger.click();trigger.remove();
      const dialog=[...document.querySelectorAll('dialog[open]')].find(node=>node.querySelector(tool.marker));if(!dialog)return;
      dialog.close();dialog.classList.add('website-tool-inline');page.querySelector('[data-tool-workspace]').append(dialog);dialog.show();tool.dialog=dialog;
    };
    page.querySelector('select').addEventListener('change',()=>{const all=!page.querySelector('select').value;page.querySelector('[data-tool-all]').hidden=!all;if(tool.dialog?.open)tool.dialog.close();if(all)showAll(tool);});
    page.querySelector('[data-tool-refresh]').onclick=()=>showAll(tool);
  }
  document.querySelector('#sitesTable').addEventListener('click',event=>{
    for(const tool of tools){if(event.target.closest(`[${tool.attribute}]`)&&tool.dialog){if(tool.dialog.open)tool.dialog.close();tool.dialog.classList.remove('website-tool-inline');document.body.append(tool.dialog);}}
  },true);
  async function showAll(tool){
    if(tool.pending)return;
    if(tool.page.querySelector('select').value)return;
    tool.pending=true;
    const content=tool.page.querySelector('[data-tool-all-content]');content.textContent='Loading all websites...';
    const sites=[...(cp.state()?.sites||[])].sort((a,b)=>a.domain.localeCompare(b.domain)),rows=[],errors=[];
    let next=0;
    const endpoint=tool.route==='website-ftp'?'/api/website-ftp':tool.route==='website-cron'?'/api/website-cron':'/api/website-backups';
    try{
      await Promise.all(Array.from({length:Math.min(4,sites.length)},async()=>{
        while(next<sites.length){const site=sites[next++];try{
          const data=await cp.api(endpoint+'?siteId='+encodeURIComponent(site.id));
          for(const item of data.items||[])rows.push({site,item,data});
        }catch(error){errors.push(site.domain+': '+error.message);}}
      }));
      rows.sort((a,b)=>a.site.domain.localeCompare(b.site.domain));
      const headings=tool.route==='website-ftp'?['Website','Username','Connection','Directory']:tool.route==='website-cron'?['Website','Name','Schedule','Command','Status']:['Website','Backup','Created','Size','Storage'];
      const cells=({site,item,data})=>tool.route==='website-ftp'?[site.domain,item.username,(data.host||'')+':'+(data.port||21)+'  |  '+(data.protocol||'FTPS'),data.directory||'']:tool.route==='website-cron'?[site.domain,item.name,item.schedule,item.command,item.enabled?'Enabled':'Disabled']:[site.domain,(item.domain||site.domain)+'-'+item.id+'.zip',cp.formatDateTime(item.createdAt),cp.formatBytes(item.size),item.remoteOnly?'Remote only':item.remotePath?'Local + remote':'Local'];
      content.innerHTML=(errors.length?'<p>'+esc(errors.join(' | '))+'</p>':'')+(rows.length?'<div class="table-scroll"><table class="data-table"><thead><tr>'+headings.map(h=>'<th>'+esc(h)+'</th>').join('')+'</tr></thead><tbody>'+rows.map(row=>'<tr>'+cells(row).map(value=>'<td>'+esc(String(value??''))+'</td>').join('')+'</tr>').join('')+'</tbody></table></div>':'<p>No '+esc(tool.title.toLowerCase())+' found.</p>');
    }finally{tool.pending=false;}
  }
  function showCurrent(){const tool=tools.find(item=>location.hash==='#'+item.route);if(tool&&!tool.page.querySelector('select').value)showAll(tool);}
  window.addEventListener('hashchange',showCurrent);
  function render(state){for(const tool of tools){const select=tool.page.querySelector('select'),selected=select.value;select.innerHTML='<option value="">All websites</option>'+(state?.sites||[]).map(site=>`<option value="${esc(site.id)}">${esc(site.domain)}</option>`).join('');if([...select.options].some(option=>option.value===selected))select.value=selected;tool.page.querySelector('[type="submit"]').disabled=!(state?.sites||[]).length;tool.page.querySelector('[data-tool-all]').hidden=!!select.value;}}
  const previous=window.LiteCPFeatures.render;window.LiteCPFeatures.render=state=>{previous(state);render(state);showCurrent();};render(cp.state());
  cp.navigate(location.hash.slice(1)||'dashboard',false);showCurrent();
})();
