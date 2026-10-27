(() => {
  "use strict";
  const cp=window.LiteCP,esc=cp.escapeHTML,page=document.createElement('section');
  page.className='page';page.dataset.pageView='website-ssl';
  page.innerHTML='<div class="page-heading"><div><h1>SSL certificates</h1><p>Install HTTPS certificates for your hosted domains.</p></div></div><article class="panel"><div class="panel-head"><h2>Website SSL status</h2><label class="search-box"><input type="search" data-ssl-search placeholder="Search domains" aria-label="Search SSL domains"></label></div><div data-ssl-table></div></article><p data-ssl-status role="status" class="feature-note"></p><p class="feature-note">Before installing SSL, point the domain’s DNS to this server and allow HTTP access on port 80. Certificates are issued by Let’s Encrypt.</p>';
  document.querySelector('.main-content').append(page);
  const link=document.createElement('a');link.className='nav-item';link.dataset.page='website-ssl';link.href='#website-ssl';link.innerHTML='<img src="assets/sidebar/security.svg" alt="" width="20" height="20"><span>SSL</span>';link.onclick=event=>{event.preventDefault();cp.navigate('website-ssl');};document.querySelector('.nav').append(link);
  const busy=new Set(),table=page.querySelector('[data-ssl-table]'),status=page.querySelector('[data-ssl-status]');
  function render(state){const query=page.querySelector('[data-ssl-search]').value.trim().toLowerCase(),sites=(state?.sites||[]).filter(site=>site.domain.toLowerCase().includes(query));table.innerHTML=sites.length?'<table class="data-table"><thead><tr><th>DOMAIN</th><th>SSL STATUS</th><th>ACTION</th></tr></thead><tbody>'+sites.map(site=>`<tr><td>${esc(site.domain)}</td><td><span class="badge ${site.ssl?'success':''}">${site.ssl?'Active':'Not installed'}</span></td><td>${site.ssl?'<span class="feature-note">HTTPS enabled</span>':`<button type="button" class="button button-primary" data-install-ssl="${esc(site.id)}" ${busy.has(site.id)?'disabled':''}>${busy.has(site.id)?'Installing…':'Install SSL'}</button>`}</td></tr>`).join('')+'</tbody></table>':'<div class="empty-state"><p>No matching websites.</p></div>';}
  page.querySelector('[data-ssl-search]').oninput=()=>render(cp.state());
  page.addEventListener('click',async event=>{
    const button=event.target.closest('[data-install-ssl]');if(!button)return;const id=button.dataset.installSsl,site=cp.state()?.sites.find(site=>site.id===id);if(!site||site.ssl||busy.has(id))return;
    const email=await window.LiteCPDialogs.prompt(`Install HTTPS for ${site.domain}. The domain must point to this server.`,cp.state()?.settings?.email||'',{title:'Install SSL certificate',label:'Renewal email address',type:'email',required:true,confirmText:'Install SSL'});if(!email)return;
    busy.add(id);render(cp.state());status.textContent=`Installing SSL for ${site.domain}…`;
    try{await cp.api('/api/sites/ssl',{method:'POST',body:{id,email}});await cp.refresh();status.textContent=`SSL installed for ${site.domain}. HTTPS is enabled.`;cp.showToast('SSL installed',`${site.domain} now uses HTTPS.`);}catch(error){status.textContent=`SSL installation failed: ${error.message}`;cp.handleError(error);}finally{busy.delete(id);render(cp.state());}
  });
  const previous=window.LiteCPFeatures.render;window.LiteCPFeatures.render=state=>{previous(state);render(state);};render(cp.state());
})();
