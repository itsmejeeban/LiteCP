(() => {
  "use strict";
  const cp=window.LiteCP,$=selector=>document.querySelector(selector);
  const page=$('[data-page-view="settings"]'),grid=page.querySelector('.settings-grid');
  const entries=[
    {id:"password",title:"Administrator password",description:"Update your administrator sign-in password.",nodes:[$('#passwordForm')]},
    {id:"email",title:"Administrator email",description:"Set the email used for certificate renewal notices.",nodes:[$('#settingsForm')]},
    {id:"timezone",title:"Time zone",description:"Choose the time zone used throughout the panel.",nodes:[$('#timezoneForm')]},
    {id:"backups",title:"Backup settings",description:"Configure retention and remote backup destinations.",nodes:[$('#backupSettingsForm'),$('#backupDestinationForm'),$('#backupDestinationsTable').closest('.panel')]},
    {id:"hostname",title:"Server hostname",description:"View and change the server hostname.",nodes:[$('#hostnameForm'),grid.querySelector('.environment-card')]},
    {id:"dns",title:"DNS resolver",description:"View the current DNS servers and configure resolvers.",nodes:[$('#resolversForm')]},
    {id:"reboot",title:"Reboot server",description:"Reboot only after reviewing the confirmation.",nodes:[$('#rebootServer').closest('.panel')]}
  ];
  const parent=$('[data-page="settings"]'),group=document.createElement('div'),toggle=document.createElement('button'),submenu=document.createElement('nav');
  group.className='settings-nav-group';parent.before(group);group.append(parent);
  toggle.type='button';toggle.className='settings-nav-toggle';toggle.setAttribute('aria-controls','settingsSubmenu');toggle.innerHTML='<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m7 10 5 5 5-5"/></svg>';
  submenu.id='settingsSubmenu';submenu.className='settings-submenu hidden';submenu.setAttribute('aria-label','Settings sections');group.append(toggle,submenu);
  function expand(open){submenu.classList.toggle('hidden',!open);toggle.setAttribute('aria-expanded',String(open));toggle.setAttribute('aria-label',open?'Collapse settings':'Expand settings');}
  toggle.addEventListener('click',()=>expand(toggle.getAttribute('aria-expanded')!=='true'));
  parent.addEventListener('click',event=>{event.preventDefault();expand(true);});
  for(const entry of entries){
    const panel=document.createElement('section');panel.dataset.settingsPanel=entry.id;panel.className='settings-tab-panel hidden';panel.setAttribute('aria-label',entry.title);
    for(const node of entry.nodes)if(node)panel.append(node);
    grid.append(panel);entry.panel=panel;
    const link=document.createElement('a');link.href='#settings/'+entry.id;link.textContent=entry.title;link.className='settings-subtab';link.dataset.settingsSection=entry.id;
    link.addEventListener('click',event=>{event.preventDefault();cp.navigate('settings/'+entry.id);});submenu.append(link);entry.link=link;
  }
  $('#backupDestinationsTable').closest('.panel').classList.add('settings-backup-destinations');
  $('.backup-management-grid').classList.add('backup-run-layout');
  const backupLink=document.createElement('a');backupLink.href='#settings/backups';backupLink.className='button button-secondary';backupLink.textContent='Backup settings';backupLink.addEventListener('click',event=>{event.preventDefault();cp.navigate('settings/backups');});
  $('[data-page-view="backups"] .page-heading').append(backupLink);
  let lastRoute='';
  function activate(){
    const route=location.hash.slice(1),active=route==='settings'||route.startsWith('settings/');
    const part=route.split('/')[1],selected=entries.find(entry=>entry.id===part)||entries[0];
    for(const entry of entries){const current=active&&entry===selected;entry.panel.classList.toggle('hidden',!current);entry.link.classList.toggle('active',current);if(current)entry.link.setAttribute('aria-current','page');else entry.link.removeAttribute('aria-current');}
    if(active){page.querySelector('.page-heading h1').textContent=selected.title;page.querySelector('.page-heading p').textContent=selected.description;if(route!==lastRoute)expand(true);}
    lastRoute=route;
  }
  const previous=window.LiteCPFeatures.navigate;window.LiteCPFeatures.navigate=id=>{previous?.(id);activate();};
  window.addEventListener('hashchange',activate);expand(false);activate();
})();
