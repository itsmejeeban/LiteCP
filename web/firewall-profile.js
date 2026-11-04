(() => {
  const cp=window.LiteCP,heading=document.querySelector('[data-page-view="firewall"] .page-heading');
  const button=document.createElement('button');button.type='button';button.className='button button-primary';button.textContent='Checking firewall…';button.disabled=true;
  heading.insertBefore(button,document.getElementById('firewallBackend'));
  const panel=document.createElement('article');panel.className='panel';heading.after(panel);
  let enabled=false,busy=false,verified=false;
  function show(state){
    enabled=state.enabled;verified=true;button.textContent=enabled?'Disable LiteCP Firewall':'Apply LiteCP Firewall';button.disabled=false;
    panel.replaceChildren();const body=document.createElement('div');body.className='settings-body';const title=document.createElement('h2');title.textContent='LiteCP required-port rules';body.append(title);
    const note=document.createElement('p');note.textContent=enabled?'Active rules verified on the server. Custom rules are listed separately below.':'LiteCP profile is disabled. Installer access and custom rules remain in place.';body.append(note);
    if(state.rules.length){const table=document.createElement('table');table.className='data-table';const head=document.createElement('thead');head.innerHTML='<tr><th>Direction</th><th>Action</th><th>Port</th><th>Port type</th><th>Zone</th><th>Remarks</th><th>Actions</th></tr>';table.append(head);const tbody=document.createElement('tbody');for(const rule of state.rules){const row=document.createElement('tr');const match=String(rule.port||'').match(/^(.*)\/(tcp|udp)$/i);const port=match?match[1]:rule.port;const portType=match?match[2].toUpperCase():'Unspecified';for(const [index,text] of [rule.direction,'Allow',port,portType,rule.zone||'Host',rule.remarks||''].entries()){const cell=document.createElement('td');if(index===0){const badge=document.createElement('span');const direction=rule.direction==='outbound'?'outbound':'inbound';badge.className='firewall-direction firewall-direction-'+direction;badge.textContent=direction==='inbound'?'Inbound':'Outbound';cell.append(badge);}else{cell.textContent=text;if(index===5)cell.className='firewall-remarks';}row.append(cell);}const actions=document.createElement('td'),remove=document.createElement('button');remove.type='button';remove.className='button button-danger';remove.textContent='Remove';remove.onclick=async()=>{
 if(busy)return;
 if(!await window.LiteCPDialogs.confirm(`Remove the ${rule.direction} allow rule for ${rule.port}? Connections using this port may stop working. Other rules may still allow access. Current SSH and panel ports cannot be removed here.`,{title:'Remove firewall rule',confirmText:'Remove',danger:true}))return;
 busy=true;remove.disabled=true;
 try{const params=new URLSearchParams({port:rule.port,direction:rule.direction,zone:rule.zone||''});show(await cp.api('/api/firewall/apply-litecp?'+params,{method:'DELETE',body:{}}));cp.showToast('Firewall rule removed',rule.port);}catch(error){cp.handleError(error);}finally{busy=false;await refresh();}
 };actions.append(remove);row.append(actions);tbody.append(row);}table.append(tbody);body.append(table);}panel.append(body);
  }
  async function refresh(){if(busy)return;try{show(await cp.api('/api/firewall/apply-litecp'));return true;}catch(error){verified=false;button.disabled=false;button.textContent='Retry firewall check';panel.textContent='Cannot verify firewall state: '+error.message;return false;}}
  button.onclick=async()=>{
    if(!verified){button.disabled=true;await refresh();return;}
    const disable=enabled;
    if(!await window.LiteCPDialogs.confirm(disable?'Disable the LiteCP required-port profile? SSH, web and panel access and unrelated rules stay enabled.':'Allow the ports required by LiteCP and installed services? External provider firewalls are configured separately.',{title:disable?'Disable LiteCP Firewall':'Apply LiteCP Firewall',confirmText:disable?'Disable profile':'Apply firewall',danger:disable}))return;
    busy=true;button.disabled=true;button.textContent=disable?'Disabling…':'Applying…';
    try{await cp.api('/api/firewall/apply-litecp',{method:disable?'DELETE':'POST',body:{}});busy=false;if(!await refresh())throw Error('Firewall status could not be verified. Refresh and check the rules.');if(enabled===disable)throw Error('Firewall state does not match the requested change. Review the rules.');cp.showToast(disable?'LiteCP firewall disabled':'LiteCP firewall applied','Server rules refreshed.');}catch(error){cp.handleError(error);}finally{busy=false;await refresh();}
  };
  window.addEventListener('hashchange',()=>{if(location.hash==='#firewall')refresh();});
  const render=window.LiteCPFeatures.render;window.LiteCPFeatures.render=state=>{render?.(state);if(location.hash==='#firewall')refresh();};
  refresh();
})();
