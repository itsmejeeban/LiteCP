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
    page.innerHTML=`<div class="page-heading"><h1>${tool.title}</h1></div><article class="panel"><form class="settings-body website-tool-picker"><label class="field"><span>Website</span><select aria-label="Website for ${tool.title}" required></select></label><button type="submit" class="button button-primary">Open ${tool.title}</button></form></article><div data-tool-workspace></div>`;
    document.querySelector('.main-content').append(page);tool.page=page;
    const link=document.createElement('a');link.className='nav-item';link.dataset.page=tool.route;link.href='#'+tool.route;
    link.innerHTML=`<img src="assets/sidebar/${tool.icon}.svg" alt="" width="18" height="18"><span>${tool.title}</span>`;
    link.onclick=event=>{event.preventDefault();cp.navigate(tool.route);};document.querySelector('.nav').append(link);
    page.querySelector('form').onsubmit=event=>{
      event.preventDefault();const id=page.querySelector('select').value;if(!id)return;
      if(tool.dialog?.open)tool.dialog.close();
      const trigger=document.createElement('button');trigger.type='button';trigger.hidden=true;trigger.setAttribute(tool.attribute,id);
      const table=document.querySelector('#sitesTable');table.append(trigger);trigger.click();trigger.remove();
      const dialog=[...document.querySelectorAll('dialog[open]')].find(node=>node.querySelector(tool.marker));if(!dialog)return;
      dialog.close();dialog.classList.add('website-tool-inline');page.querySelector('[data-tool-workspace]').append(dialog);dialog.show();tool.dialog=dialog;
    };
  }
  document.querySelector('#sitesTable').addEventListener('click',event=>{
    for(const tool of tools){if(event.target.closest(`[${tool.attribute}]`)&&tool.dialog){if(tool.dialog.open)tool.dialog.close();tool.dialog.classList.remove('website-tool-inline');document.body.append(tool.dialog);}}
  },true);
  function render(state){for(const tool of tools){const select=tool.page.querySelector('select'),selected=select.value;select.innerHTML=(state?.sites||[]).map(site=>`<option value="${esc(site.id)}">${esc(site.domain)}</option>`).join('');if([...select.options].some(option=>option.value===selected))select.value=selected;tool.page.querySelector('[type="submit"]').disabled=!select.options.length;}}
  const previous=window.LiteCPFeatures.render;window.LiteCPFeatures.render=state=>{previous(state);render(state);};render(cp.state());
  cp.navigate(location.hash.slice(1)||'dashboard',false);
})();
