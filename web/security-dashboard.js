(() => {
 'use strict';
 const cp=window.LiteCP,esc=cp.escapeHTML,page=document.createElement('section');
 page.className='page';page.dataset.pageView='security-dashboard';
 page.innerHTML='<div class="page-heading"><div><h1>Security Dashboard</h1><p>Observed protections and items that need attention.</p></div><button class="button button-secondary" data-security-refresh>Refresh</button></div><p class="feature-note">An active service is not proof that the server is secure. Unknown checks need verification.</p><p role="status" data-security-status></p><div class="security-posture-grid" data-security-checks></div>';
 document.querySelector('.main-content').append(page);
 const link=document.createElement('a');link.className='nav-item';link.dataset.page='security-dashboard';link.href='#security-dashboard';
 link.innerHTML='<img src="assets/sidebar/security.svg" alt="" width="20" height="20"><span>Security Dashboard</span>';
 document.querySelector('[data-page="firewall"]').before(link);link.onclick=event=>{event.preventDefault();cp.navigate('security-dashboard');};
 let pending=null,generation=0;
 function load(){if(pending)return pending;const request=++generation,status=page.querySelector('[data-security-status]'),button=page.querySelector('button');status.textContent='Checking security status…';button.disabled=true;
  pending=(async()=>{try{const value=await cp.api('/api/security/posture');if(request!==generation)return;
   page.querySelector('[data-security-checks]').innerHTML=value.checks.map(check=>{const type=['critical','warning','good','info','unknown'].includes(check.status)?check.status:'unknown';return `<article class="panel security-check security-check-${type}"><div class="settings-body"><div class="security-check-heading"><h2>${esc(check.label)}</h2><span class="badge">${esc({critical:'Action required',warning:'Needs review',good:'Observed',info:'Information',unknown:'Not verified'}[type])}</span></div><p>${esc(check.detail)}</p></div></article>`;}).join('');status.textContent='Checked '+cp.formatDateTime(value.checkedAt);
  }catch(error){status.textContent='Unable to check security status. '+error.message;}finally{button.disabled=false;pending=null;}})();return pending;
 }
 page.querySelector('button').onclick=load;
 const previous=window.LiteCPFeatures.navigate;window.LiteCPFeatures.navigate=id=>{previous?.(id);if(id==='security-dashboard')load();};
 window.addEventListener('hashchange',()=>{if(location.hash==='#security-dashboard')load();});
 if(location.hash==='#security-dashboard')cp.navigate('security-dashboard',false);
})();
