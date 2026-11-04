(() => {
  'use strict';
  const cp=window.LiteCP, esc=cp.escapeHTML;
  const page=document.createElement('section');page.className='page website-dns-page';page.dataset.pageView='website-dns';
  page.innerHTML=`<div class="page-heading"><div><h1>Website DNS</h1><p>Manage authoritative DNS records or export a zone for another provider.</p></div></div>
  <article class="panel"><div class="settings-body"><label class="field"><span>Website</span><select data-site aria-label="DNS website"></select></label><p data-status role="status"></p>
  <p class="feature-note">Public DNS changes only after your registrar delegates the domain to this server. Review nameservers and glue records before switching.</p>
  <div class="dns-actions"><button class="button button-primary" data-load>Refresh</button><button class="button button-primary" data-export>Export zone</button></div></div></article>
  <article class="panel" data-editor hidden><div class="panel-head"><h2>DNS records</h2><button class="button button-primary" data-add>Add record</button></div><div class="dns-records" data-records></div><nav class="dns-pagination" aria-label="DNS record pages"><label>Per page <select data-page-size aria-label="DNS records per page"><option>10</option><option>25</option><option>50</option><option>100</option></select></label><span data-page-status role="status"></span><div><button type="button" class="button button-secondary" data-prev>Previous</button><button type="button" class="button button-secondary" data-next>Next</button></div></nav>
  <div class="settings-body"><div class="dns-actions"><button class="button button-primary" data-save>Save zone</button></div><details><summary>Import zone text</summary><label class="field"><span>BIND zone file</span><textarea data-import rows="8" spellcheck="false"></textarea></label><div class="dns-actions"><button class="button button-primary" data-import-save>Validate and save imported zone</button></div></details></div></article>`;
  document.querySelector('.main-content').append(page);
  const link=document.createElement('a');link.className='nav-item';link.dataset.page='website-dns';link.href='#website-dns';link.innerHTML='<img src="assets/sidebar/dns.svg" width="20" height="20" alt=""><span>DNS</span>';link.onclick=e=>{e.preventDefault();cp.navigate('website-dns');};document.querySelector('.nav').append(link);
  const $=s=>page.querySelector(s), picker=$('[data-site]');let records=[],generation=0,busy=false,recordPage=1,pageSize=10,loadedSite='';
  const endpoint=()=>'/api/sites/dns?site='+encodeURIComponent(picker.value);
  const active=()=>location.hash==='#website-dns';
  function buttons(){
    const unavailable=busy||!picker.value,pages=Math.max(1,Math.ceil(records.length/pageSize));
    page.querySelectorAll('button').forEach(b=>b.disabled=unavailable);
    page.querySelectorAll('[data-records] input,[data-import]').forEach(input=>input.disabled=busy);
    $('[data-prev]').disabled=unavailable||recordPage<=1;
    $('[data-next]').disabled=unavailable||recordPage>=pages;
    $('[data-page-size]').disabled=unavailable;picker.disabled=busy;
  }
  function render(){
    const pages=Math.max(1,Math.ceil(records.length/pageSize));recordPage=Math.max(1,Math.min(recordPage,pages));
    const start=(recordPage-1)*pageSize,end=Math.min(start+pageSize,records.length);
    $('[data-records]').innerHTML=records.slice(start,end).map((r,offset)=>{const i=start+offset;return `<div class="dns-record" data-index="${i}"><label class="field"><span>Name</span><input data-key="name" value="${esc(r.name)}" maxlength="253"></label><label class="field"><span>Type</span><input data-key="type" value="${esc(r.type)}" maxlength="16"></label><label class="field"><span>TTL (seconds)</span><input data-key="ttl" type="number" min="0" max="2147483647" value="${Number(r.ttl)}"></label><label class="field"><span>Value</span><input data-key="value" value="${esc(r.value)}"></label><button type="button" class="button button-danger" data-remove="${i}" aria-label="Remove record ${i+1}">Remove</button></div>`;}).join('');buttons();
    $('[data-page-status]').textContent=`${records.length?start+1:0}-${end} of ${records.length} records | Page ${recordPage} of ${pages}`;
    $('[data-prev]').disabled=busy||recordPage===1;$('[data-next]').disabled=busy||recordPage===pages;
  }
  function read(){page.querySelectorAll('.dns-record').forEach(row=>{const r={};row.querySelectorAll('[data-key]').forEach(x=>r[x.dataset.key]=x.dataset.key==='ttl'?Number(x.value):x.value.trim());records[Number(row.dataset.index)]=r;});return records;}
  async function load(){const id=++generation;if(loadedSite!==picker.value){loadedSite=picker.value;records=[];$('[data-import]').value='';$('[data-editor]').hidden=true;}if(!picker.value){$('[data-editor]').hidden=true;$('[data-status]').textContent='Select a website.';buttons();return;}busy=true;buttons();$('[data-status]').textContent='Loading DNS records...';try{const result=await cp.api(endpoint());if(id!==generation)return;records=result.zone.records;recordPage=1;$('[data-editor]').hidden=false;$('[data-status]').textContent=result.installed?(result.active?(result.saved?'DNS service running. Saved zone loaded.':'DNS service running. Save this zone to publish it.'):'DNS service is stopped. Saving will validate the zone; start litecp-dns before publishing.'):'DNS service is not installed. Records can be saved as a draft and exported. Install scripts/install-dns.sh on the server to publish authoritative zones.';render();}catch(e){if(id===generation){$('[data-status]').textContent=e.message;$('[data-editor]').hidden=true;}}finally{if(id===generation){busy=false;buttons();}}}
  async function save(imported=false){if(busy)return;busy=true;buttons();$('[data-status]').textContent='Validating and saving DNS zone...';try{const data=imported?{zoneText:$('[data-import]').value}:{records:read()};const result=await cp.api(endpoint(),{method:'PUT',body:data});await load();$('[data-status]').textContent=result.published?'DNS zone validated and published.':'DNS draft saved. Install the DNS service, then save again to publish.';}catch(e){$('[data-status]').textContent=e.message;}finally{busy=false;buttons();}}
  $('[data-prev]').onclick=()=>{read();recordPage--;render();};$('[data-next]').onclick=()=>{read();recordPage++;render();};
  $('[data-page-size]').onchange=e=>{read();pageSize=Number(e.target.value);recordPage=1;render();};
  picker.onchange=load;$('[data-load]').onclick=load;$('[data-save]').onclick=()=>save();$('[data-import-save]').onclick=()=>save(true);
  $('[data-export]').onclick=()=>{const a=document.createElement('a');a.href=endpoint()+'&export=1';a.download='';document.body.append(a);a.click();a.remove();};
  $('[data-add]').onclick=()=>{records=read();records.push({name:'@',type:'A',ttl:3600,value:''});recordPage=Math.ceil(records.length/pageSize);render();};
  $('[data-records]').onclick=async e=>{const b=e.target.closest('[data-remove]');if(!b)return;if(!await window.LiteCPDialogs.confirm('Remove this record from the draft? Changes take effect after Save zone.',{title:'Remove DNS record',confirmText:'Remove'}))return;records=read();records.splice(Number(b.dataset.remove),1);render();};
  function fill(state){const old=picker.value;picker.innerHTML='<option value="">Select website</option>'+(state?.sites||[]).map(s=>`<option value="${esc(s.id)}">${esc(s.domain)}</option>`).join('');if([...picker.options].some(x=>x.value===old))picker.value=old;buttons();}
  // The Websites row uses a dedicated data attribute so its existing action handler is unchanged.
  function decorate(){document.querySelectorAll('#sitesTable [data-site-action="files"]').forEach(file=>{const row=file.closest('tr'),actions=file.parentElement;if(!row||actions.querySelector('[data-site-dns]'))return;const id=file.dataset.id||file.dataset.siteId||file.dataset.site||row.querySelector('[data-site-backup]')?.dataset.siteBackup;if(!id)return;const b=document.createElement('button');b.type='button';b.dataset.siteDns=id;b.title='Manage website DNS';b.setAttribute('aria-label','Manage website DNS');b.innerHTML='<img src="assets/sidebar/dns.svg" alt="" width="28" height="28"><span class="site-action-label">DNS</span>';const db=actions.querySelector('[data-site-action="databases"]');(db||file).after(b);});}
  document.addEventListener('click',e=>{const b=e.target.closest('[data-site-dns]');if(!b)return;fill(cp.state());picker.value=b.dataset.siteDns;cp.navigate('website-dns');load();});
  const oldRender=window.LiteCPFeatures.render;window.LiteCPFeatures.render=state=>{oldRender(state);fill(state);decorate();};
  const oldNavigate=window.LiteCPFeatures.navigate;window.LiteCPFeatures.navigate=id=>{oldNavigate?.(id);if(active())load();};
  window.addEventListener('hashchange',()=>{if(active())load();});fill(cp.state());decorate();if(active())cp.navigate('website-dns',false);
})();
