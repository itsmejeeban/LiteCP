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
    page.innerHTML=`<div class="page-heading"><h1>${tool.title}</h1></div><article class="panel"><form class="settings-body website-tool-picker"><label class="field"><span>Website</span><select aria-label="Website for ${tool.title}" required></select></label><button type="submit" class="button button-primary">Open ${tool.title}</button></form></article><article class="panel" data-tool-all><div class="panel-head"><h2>All ${tool.title}</h2>${tool.route==='website-ftp'?'<button type="button" class="button button-primary" data-ftp-add-all>Add FTP account</button>':''}<button type="button" class="button button-secondary" data-tool-refresh>Refresh</button></div><div class="settings-body" data-tool-all-content role="status"></div></article><div data-tool-workspace></div>`;
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
    page.querySelector('select').addEventListener('change',event=>{
      const all=!event.currentTarget.value;
      page.querySelector('[data-tool-all]').hidden=!all;
      if(tool.dialog?.open)tool.dialog.close();
      if(all)showAll(tool);
      else tool.pending=null;
    });
    page.querySelector('[data-tool-refresh]').onclick=()=>showAll(tool);
    if(tool.route==='website-ftp'){
      page.querySelector('[data-ftp-add-all]').onclick=()=>{
        const chooser=document.createElement('dialog');chooser.className='fm-dialog';
        chooser.innerHTML='<header><h2>Add FTP account</h2></header><form class="settings-body"><label class="field"><span>Website</span><select required>'+[...(cp.state()?.sites||[])].sort((a,b)=>a.domain.localeCompare(b.domain)).map(site=>'<option value="'+esc(site.id)+'">'+esc(site.domain)+'</option>').join('')+'</select></label><div class="feature-actions"><button type="submit" class="button button-primary">Continue</button><button type="button" class="button button-secondary" data-cancel>Cancel</button></div></form>';
        document.body.append(chooser);chooser.showModal();chooser.querySelector('[data-cancel]').onclick=()=>chooser.close();
        chooser.querySelector('form').onsubmit=event=>{event.preventDefault();const id=chooser.querySelector('select').value;if(id){chooser.close();openFTPAccount(id);}};
      };
      page.addEventListener('click',async event=>{
        const button=event.target.closest('[data-all-ftp-edit],[data-all-ftp-remove]');if(!button||tool.pending)return;
        const row=tool.rows?.[Number(button.dataset.ftpRow)];if(!row)return;
        if(button.hasAttribute('data-all-ftp-edit')){openFTPAccount(row.site.id,row.item.id);return;}
        if(!await window.LiteCPDialogs.confirm('Remove FTP account '+row.item.username+' for '+row.site.domain+'? Website files will be kept. Existing connections may remain active until disconnected.',{title:'Remove FTP account',confirmText:'Remove',danger:true}))return;
        button.disabled=true;try{await cp.api('/api/website-ftp?siteId='+encodeURIComponent(row.site.id),{method:'DELETE',body:{id:row.item.id,confirm:true}});await showAll(tool);await cp.refresh();}catch(error){cp.handleError(error);}finally{button.disabled=false;}
      });
    }

  }
  document.querySelector('#sitesTable').addEventListener('click',event=>{
    for(const tool of tools){if(event.target.closest(`[${tool.attribute}]`)&&tool.dialog){if(tool.dialog.open)tool.dialog.close();tool.dialog.classList.remove('website-tool-inline');document.body.append(tool.dialog);}}
  },true);
  function openFTPAccount(siteId,accountId=''){
    const trigger=document.createElement('button');trigger.hidden=true;trigger.setAttribute('data-site-ftp',siteId);if(accountId)trigger.setAttribute('data-ftp-initial-edit',accountId);const table=document.querySelector('#sitesTable');table.append(trigger);trigger.click();trigger.remove();
  }
  async function showAll(tool){
    if(tool.pending)return;
    if(tool.page.querySelector('select').value)return;
    const request=Symbol();
    tool.pending=request;
    const content=tool.page.querySelector('[data-tool-all-content]');content.textContent='Loading all websites...';
    const sites=[...(cp.state()?.sites||[])].sort((a,b)=>a.domain.localeCompare(b.domain)),rows=[],errors=[];
    let next=0;
    const endpoint=tool.route==='website-ftp'?'/api/website-ftp':tool.route==='website-cron'?'/api/website-cron':'/api/website-backups';
    try{
      await Promise.all(Array.from({length:Math.min(4,sites.length)},async()=>{
        while(tool.pending===request&&next<sites.length){const site=sites[next++];try{
          const data=await cp.api(endpoint+'?siteId='+encodeURIComponent(site.id));
          for(const item of data.items||[])rows.push({site,item,data});
        }catch(error){errors.push(site.domain+': '+error.message);}}
      }));
      if(tool.pending!==request||tool.page.querySelector('select').value)return;
      rows.sort((a,b)=>a.site.domain.localeCompare(b.site.domain));
      tool.rows=rows;
      const headings=tool.route==='website-ftp'?['Website','Username','Connection','Directory','Actions']:tool.route==='website-cron'?['Website','Name','Schedule','Command','Status']:['Website','Backup','Created','Size','Storage'];
      const cells=({site,item,data})=>tool.route==='website-ftp'?[site.domain,item.username,(data.host||'')+':'+(data.port||21)+'  |  '+(data.protocol||'FTPS'),data.directory||'']:tool.route==='website-cron'?[site.domain,item.name,item.schedule,item.command,item.enabled?'Enabled':'Disabled']:[site.domain,(item.domain||site.domain)+'-'+item.id+'.zip',cp.formatDateTime(item.createdAt),cp.formatBytes(item.size),item.remoteOnly?'Remote only':item.remotePath?'Local + remote':'Local'];
      content.innerHTML=(errors.length?'<p>'+esc(errors.join(' | '))+'</p>':'')+(rows.length?'<div class="table-scroll"><table class="data-table"><thead><tr>'+headings.map(h=>'<th>'+esc(h)+'</th>').join('')+'</tr></thead><tbody>'+rows.map((row,index)=>'<tr>'+cells(row).map(value=>'<td>'+esc(String(value??''))+'</td>').join('')+(tool.route==='website-ftp'?'<td><div class="feature-actions"><button type="button" class="button button-secondary" data-all-ftp-edit data-ftp-row="'+index+'">Edit</button><button type="button" class="button button-danger" data-all-ftp-remove data-ftp-row="'+index+'">Remove</button></div></td>':'')+'</tr>').join('')+'</tbody></table></div>':'<p>No '+esc(tool.title.toLowerCase())+' found.</p>');
    }finally{if(tool.pending===request)tool.pending=null;}
  }
  function showCurrent(){const tool=tools.find(item=>location.hash==='#'+item.route);if(tool&&!tool.page.querySelector('select').value)showAll(tool);}
  window.addEventListener('hashchange',showCurrent);
  function render(state){for(const tool of tools){const select=tool.page.querySelector('select'),selected=select.value;select.innerHTML='<option value="">All websites</option>'+(state?.sites||[]).map(site=>`<option value="${esc(site.id)}">${esc(site.domain)}</option>`).join('');if([...select.options].some(option=>option.value===selected))select.value=selected;tool.page.querySelector('[type="submit"]').disabled=!(state?.sites||[]).length;tool.page.querySelector('[data-tool-all]').hidden=!!select.value;}}
  const previous=window.LiteCPFeatures.render;window.LiteCPFeatures.render=state=>{previous(state);render(state);showCurrent();};render(cp.state());
  cp.navigate(location.hash.slice(1)||'dashboard',false);showCurrent();
})();
