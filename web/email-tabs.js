(() => {
  "use strict";
  const cp=window.LiteCP,page=document.querySelector('[data-page-view="email"]');
  const forward=document.createElement('form');forward.id='emailForwardForm';forward.className='panel';
  forward.innerHTML='<div class="panel-head"><div><h2>Email Forward</h2><p>Forward incoming mail to another email address.</p></div></div><div class="settings-body"><label class="field"><span>Source email address</span><input type="email" name="source" placeholder="sales@example.com" required autocomplete="off"><small>Use a domain added to Mail Domains.</small></label><label class="field"><span>Forward to</span><input type="email" name="destination" placeholder="owner@example.net" required autocomplete="off"></label><div class="feature-actions"><button type="submit" class="button button-primary">Add forward</button><button type="button" class="button button-secondary" data-view-forwards>View forwards</button></div><p role="status" data-forward-status></p></div>';
  page.append(forward);
  forward.querySelector('[data-view-forwards]').onclick=()=>cp.navigate('email/aliases');
  forward.addEventListener('submit',async event=>{
    event.preventDefault();if(!forward.reportValidity())return;
    const button=forward.querySelector('[type="submit"]'),status=forward.querySelector('[data-forward-status]');
    if(button.disabled)return;button.disabled=true;status.textContent='Saving forward...';
    const data=new FormData(forward);
    try{
      await cp.api('/api/mail/aliases',{method:'POST',body:{source:String(data.get('source')).trim().toLowerCase(),destination:String(data.get('destination')).trim()}});
      forward.reset();status.textContent='Email forward created. View forwards to manage it.';
      await cp.refresh();cp.showToast('Email forward created','Incoming mail will be forwarded to the destination.');
    }catch(error){status.textContent='Forward could not be created: '+error.message;cp.handleError(error);}
    finally{button.disabled=false;}
  });
  const entries=[['create-mailbox','Create Mailbox','#mailboxForm'],['mailboxes','Mailboxes','#mailboxesTable'],['add-domain','Add Mail Domain','#mailDomainForm'],['create-alias','Create Alias','#mailAliasForm'],['forward','Email Forward','#emailForwardForm'],['domains','Mail Domains','#mailDomainsTable'],['aliases','Aliases','#mailAliasesTable'],['delivery','Email Delivery','#mailDeliveryPanel'],['smtp-port-check','SMTP Port Check','#smtpPortCheckPanel'],['configure-webmail','Configure Webmail','#webmailSetupPanel']];
  const parent=document.querySelector('.nav-item[data-page="email"]'),group=document.createElement('div'),toggle=document.createElement('button'),submenu=document.createElement('nav');
  group.className='sidebar-feature-group';parent.replaceWith(group);toggle.type='button';toggle.className='nav-item sidebar-group-toggle';toggle.setAttribute('aria-controls','emailSubmenu');toggle.innerHTML='<img src="assets/sidebar/email.svg" alt="" width="22" height="22"><span>Email</span><svg class="sidebar-group-chevron" viewBox="0 0 24 24" aria-hidden="true"><path d="m7 10 5 5 5-5"/></svg>';
  submenu.id='emailSubmenu';submenu.className='settings-submenu hidden';submenu.setAttribute('aria-label','Email sections');group.append(toggle,submenu);
  const expand=open=>{submenu.classList.toggle('hidden',!open);toggle.setAttribute('aria-expanded',String(open));};toggle.onclick=()=>expand(toggle.getAttribute('aria-expanded')!=='true');
  for(const entry of entries){
    const panel=document.createElement('section');panel.className='email-tab-panel hidden';panel.dataset.emailPanel=entry[0];
    if(entry[2]){const node=document.querySelector(entry[2]);panel.append(node.matches('form')?node:node.closest('.panel'));}
    page.append(panel);entry.panel=panel;
    const link=document.createElement('a');link.className='nav-item';link.href='#email/'+entry[0];link.textContent=entry[1];link.onclick=event=>{event.preventDefault();cp.navigate('email/'+entry[0]);};submenu.append(link);entry.link=link;
  }
  page.querySelector('.mail-form-grid').classList.add('hidden');page.querySelector('.stacked-panels').classList.add('email-dns-only');
  const setupNotice=document.createElement('article');setupNotice.className='panel email-setup-notice hidden';
  setupNotice.innerHTML='<div class="settings-body"><h2>Configure webmail first</h2><p data-email-setup-message>Complete webmail setup to use Email tools.</p><div class="feature-actions"><button class="button button-primary" type="button">Configure webmail</button></div></div>';
  page.append(setupNotice);
  let configured=null,prompting=false,promptedRoute='',pendingRoute='';
  setupNotice.querySelector('button').onclick=()=>cp.navigate('email/configure-webmail');
  async function requireSetup(route){
    if(prompting||promptedRoute===route)return;
    prompting=true;promptedRoute=route;
    try {
      const result=await cp.api('/api/mail/webmail-configure');configured=!!result.configured;
      if(location.hash.slice(1)!==route)return;
      if(configured){sync();return;}
      const accepted=await window.LiteCPDialogs.confirm('Configure webmail before using Email tools.',{title:'Webmail setup required',confirmText:'Configure webmail'});
      if(location.hash.slice(1)!==route)return;
      if(accepted){pendingRoute=route;cp.navigate('email/configure-webmail');}
      else cp.navigate('dashboard');
    } catch(error){
      if(location.hash.slice(1)===route)setupNotice.querySelector('[data-email-setup-message]').textContent='Unable to check webmail setup. Open Configure webmail to check its status. '+error.message;
    } finally {prompting=false;}
  }
  window.addEventListener('litecp:webmail-status',event=>{
    configured=!!event.detail.configured;
    if(configured&&pendingRoute&&location.hash==='#email/configure-webmail'){
      const target=pendingRoute;pendingRoute='';cp.navigate(target);
    }else sync();
  });
  let lastRoute='';function sync(){const route=location.hash.slice(1),active=route==='email'||route.startsWith('email/'),selected=entries.find(entry=>entry[0]===route.split('/')[1])||entries[0];
    const ready=configured??!!cp.state()?.system?.webmailAvailable;
    const blocked=active&&selected[0]!=='configure-webmail'&&!ready;
    setupNotice.classList.toggle('hidden',!blocked);
    if(!active)promptedRoute='';
    toggle.classList.toggle('active',active);for(const entry of entries){const current=active&&entry===selected&&!blocked;entry.panel.classList.toggle('hidden',!current);entry.link.classList.toggle('active',current);if(current)entry.link.setAttribute('aria-current','page');else entry.link.removeAttribute('aria-current');}
    if(active){page.querySelector('h1').textContent=selected[1];if(route!==lastRoute)expand(true);}
    if(route!==lastRoute)document.querySelector('#mailDNSPanel').classList.add('hidden');lastRoute=route;
    if(blocked)requireSetup(route);
  }
  const navigate=window.LiteCPFeatures.navigate;window.LiteCPFeatures.navigate=id=>{navigate?.(id);sync();};
  const render=window.LiteCPFeatures.render;window.LiteCPFeatures.render=state=>{render(state);sync();};
  window.addEventListener('hashchange',sync);expand(false);sync();
})();
