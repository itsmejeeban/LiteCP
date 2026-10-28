(() => {
  "use strict";
  const cp=window.LiteCP,page=document.querySelector('[data-page-view="backups"]');
  const entries=[
    ['create','Create Backup','#backupRunForm','backups'],
    ['add-destination','Add Remote Destination','#backupDestinationForm','add-mail-domain'],
    ['settings','Backup Settings','#backupSettingsForm','settings'],
    ['destinations','Remote Destinations','#backupDestinationsTable','services-control'],
    ['history','Backup History','#backupsTable','activities']
  ];
  const parent=document.querySelector('.nav-item[data-page="backups"]'),group=document.createElement('div'),toggle=document.createElement('button'),submenu=document.createElement('nav');
  group.className='sidebar-feature-group';parent.replaceWith(group);toggle.type='button';toggle.className='nav-item sidebar-group-toggle';toggle.setAttribute('aria-controls','backupsSubmenu');toggle.innerHTML='<img src="assets/sidebar/backup-vault.svg" alt="" width="22" height="22"><span>Backups</span><svg class="sidebar-group-chevron" viewBox="0 0 24 24" aria-hidden="true"><path d="m7 10 5 5 5-5"/></svg>';
  submenu.id='backupsSubmenu';submenu.className='settings-submenu hidden';submenu.setAttribute('aria-label','Backup sections');group.append(toggle,submenu);
  const expand=open=>{submenu.classList.toggle('hidden',!open);toggle.setAttribute('aria-expanded',String(open));};toggle.onclick=()=>expand(toggle.getAttribute('aria-expanded')!=='true');
  for(const entry of entries){const panel=document.createElement('section');panel.className='backup-tab-panel hidden';panel.dataset.backupPanel=entry[0];const node=document.querySelector(entry[2]);panel.append(node.matches('form')?node:node.closest('.panel'));page.append(panel);entry.panel=panel;
    const link=document.createElement('a');link.className='nav-item';link.href='#backups/'+entry[0];link.innerHTML=`<img src="assets/sidebar/${entry[3]}.svg" alt="" width="20" height="20"><span>${entry[1]}</span>`;link.onclick=event=>{event.preventDefault();cp.navigate('backups/'+entry[0]);};submenu.append(link);entry.link=link;
  }
  page.querySelector('.backup-management-grid').classList.add('hidden');
  let lastRoute='';function sync(){const route=location.hash.slice(1),active=route==='backups'||route.startsWith('backups/'),selected=entries.find(entry=>entry[0]===route.split('/')[1])||entries[0];
    toggle.classList.toggle('active',active);for(const entry of entries){const current=active&&entry===selected;entry.panel.classList.toggle('hidden',!current);entry.link.classList.toggle('active',current);if(current)entry.link.setAttribute('aria-current','page');else entry.link.removeAttribute('aria-current');}
    page.querySelector('.backup-note').classList.toggle('hidden',selected!==entries[0]);if(active){page.querySelector('h1').textContent=selected[1];if(route!==lastRoute)expand(true);}lastRoute=route;
  }
  const navigate=window.LiteCPFeatures.navigate;window.LiteCPFeatures.navigate=id=>{navigate?.(id);sync();};
  window.addEventListener('hashchange',sync);expand(false);sync();
})();
