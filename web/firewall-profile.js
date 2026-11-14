(() => {
  const cp=window.LiteCP,heading=document.querySelector('[data-page-view="firewall"] .page-heading');
  const button=document.createElement('button');button.type='button';button.className='button button-primary';button.textContent='Checking firewall…';button.disabled=true;
  heading.insertBefore(button,document.getElementById('firewallBackend'));
  const panel=document.createElement('article');panel.className='panel';heading.after(panel);
  let enabled=false,busy=false,verified=false,inspection;
  function show(state){
    enabled=state.enabled;verified=true;button.textContent=enabled?'Disable LiteCP Firewall':'Apply LiteCP Firewall';button.disabled=false;
    panel.replaceChildren();const body=document.createElement('div');body.className='settings-body';const title=document.createElement('h2');title.textContent='LiteCP required-port rules';body.append(title);
    const note=document.createElement('p');note.textContent=enabled?'Active rules verified on the server. Custom rules are listed separately below.':'LiteCP profile is disabled. Installer access and custom rules remain in place.';body.append(note);
    if(state.rules.length){const table=document.createElement('table');table.className='data-table';const head=document.createElement('thead');head.innerHTML='<tr><th>Direction</th><th>Action</th><th>Port</th><th>Port type</th><th>Zone</th><th>Address</th><th>Remarks</th><th>Actions</th></tr>';table.append(head);const tbody=document.createElement('tbody');for(const rule of state.rules){const row=document.createElement('tr');const match=String(rule.port||'').match(/^(.*)\/(tcp|udp)$/i);const port=match?match[1]:rule.port;const portType=match?match[2].toUpperCase():'Unspecified';for(const [index,text] of [rule.direction,'Allow',port,portType,rule.zone||'Host',rule.address||'Any',rule.remarks||''].entries()){const cell=document.createElement('td');if(index===0){const badge=document.createElement('span');const direction=rule.direction==='outbound'?'outbound':'inbound';badge.className='firewall-direction firewall-direction-'+direction;badge.textContent=direction==='inbound'?'Inbound':'Outbound';cell.append(badge);}else{cell.textContent=text;if(index===6)cell.className='firewall-remarks';}row.append(cell);}const actions=document.createElement('td'),remove=document.createElement('button');remove.type='button';remove.className='button button-danger';remove.textContent='Remove';remove.onclick=async()=>{
 if(busy)return;
 if(!await window.LiteCPDialogs.confirm(`Remove the ${rule.direction} allow rule for ${rule.port}? Connections using this port may stop working. Other rules may still allow access. Current SSH and panel ports cannot be removed here.`,{title:'Remove firewall rule',confirmText:'Remove',danger:true}))return;
 busy=true;remove.disabled=true;
 try{const params=new URLSearchParams({port:rule.port,direction:rule.direction,zone:rule.zone||'',address:rule.address||''});show(await cp.api('/api/firewall/apply-litecp?'+params,{method:'DELETE',body:{}}));cp.showToast('Firewall rule removed',rule.port);}catch(error){cp.handleError(error);}finally{busy=false;await refresh();}
 };const edit=document.createElement('button');edit.type='button';edit.className='button button-secondary';edit.textContent='Edit';edit.onclick=()=>editRule(rule);const group=document.createElement('div');group.className='feature-actions';group.append(edit,remove);actions.append(group);row.append(actions);tbody.append(row);}table.append(tbody);body.append(table);}panel.append(body);
  }
  function editRule(rule){
    if(busy)return;
    const match=String(rule.port).match(/^(.*)\/(tcp|udp)$/);
    const dialog = document.createElement('dialog');
    dialog.className = 'fm-dialog';
    dialog.setAttribute('aria-labelledby', 'firewallEditTitle');
    dialog.innerHTML = `
      <form class="settings-body">
        <h2 id="firewallEditTitle">Edit firewall rule</h2>
        <label class="field"><span>Direction / zone</span><input data-scope readonly></label>
        <label class="field"><span>Action</span><input value="Allow" readonly></label>
        <label class="field"><span>Allowed port or range</span><input name="port" required pattern="[0-9]+(?:&#92;-[0-9]+)?" maxlength="11" inputmode="numeric"></label>
        <label class="field"><span>Protocol</span><select name="protocol"><option value="tcp">TCP</option><option value="udp">UDP</option></select></label>
        <label class="field"><span data-address-label></span><textarea name="addresses" maxlength="2048" rows="2" placeholder="any" required spellcheck="false"></textarea><small>Enter up to 32 IP addresses or CIDR networks separated by commas or new lines. Use any for all addresses.</small></label>
        <label class="field"><span>Remarks</span><textarea name="remarks" maxlength="500"></textarea></label>
        <p data-error role="alert" hidden></p>
        <div class="form-actions">
          <button type="submit" class="button button-primary">Save changes</button>
          <button type="button" class="button button-secondary" data-cancel>Cancel</button>
        </div>
      </form>`;
    const form = dialog.querySelector('form');
    const direction = rule.direction === 'outbound' ? 'Outbound' : 'Inbound';
    dialog.querySelector('[data-scope]').value = direction + ' / ' + (rule.zone || 'Host');
    dialog.querySelector('[data-address-label]').textContent = direction === 'Outbound' ? 'Destination IP / CIDR' : 'Source IP / CIDR';
    form.elements.port.value = match ? match[1] : rule.port;
    form.elements.protocol.value = match ? match[2] : 'tcp';
    form.elements.addresses.value = rule.address || 'any';
    form.elements.remarks.value = rule.remarks || '';
    dialog.querySelector('[data-cancel]').onclick = () => dialog.close();
    dialog.addEventListener('close', () => dialog.remove());
    dialog.addEventListener('cancel', event => { if (busy) event.preventDefault(); });
    form.onsubmit = async event => {
      event.preventDefault();
      if (busy || !form.reportValidity()) return;
      const newPort = form.elements.port.value + '/' + form.elements.protocol.value;
      const addresses = form.elements.addresses.value.trim();
      const errorMessage = dialog.querySelector('[data-error]');
      const fields = form.querySelectorAll('button,input,select,textarea');
      busy = true;
      errorMessage.hidden = true;
      fields.forEach(node => node.disabled = true);
      try {
        if ((newPort !== rule.port || (addresses || 'any') !== (rule.address || 'any')) && !await window.LiteCPDialogs.confirm(
          `Replace the ${rule.port} allow rule with ${newPort} for ${addresses || 'any'}? This changes firewall access only. Other firewall rules may still allow connections.`,
          {title:'Edit firewall rule',confirmText:'Save changes'}
        )) return;
        const state = await cp.api('/api/firewall/apply-litecp', {
          method:'PUT',
          body:{port:rule.port,direction:rule.direction,zone:rule.zone||'',address:rule.address||'',newPort,addresses,remarks:form.elements.remarks.value}
        });
        show(state);
        dialog.close();
        cp.showToast('Firewall rule updated', newPort);
      } catch (error) {
        errorMessage.textContent = error.message;
        errorMessage.hidden = false;
      } finally {
        busy = false;
        if (dialog.isConnected) fields.forEach(node => node.disabled = false);
      }
    };
    document.body.append(dialog);
    dialog.showModal();
    form.elements.port.focus();
  }
  function refresh(){
    if(busy||!cp.state())return;
    if(inspection)return inspection;
    button.disabled=true;
    button.textContent='Checking firewall…';
    panel.inert=true;
    inspection=cp.api('/api/firewall/apply-litecp').then(state=>{
      show(state);
      return true;
    }).catch(error=>{
      verified=false;
      button.disabled=false;
      button.textContent='Retry firewall check';
      panel.textContent='Cannot verify firewall state: '+error.message;
      return false;
    }).finally(()=>{
      panel.inert=false;
      inspection=null;
    });
    return inspection;
  }
  button.onclick=async()=>{
    if(!verified){button.disabled=true;await refresh();return;}
    const disable=enabled;
    if(!await window.LiteCPDialogs.confirm(disable?'Disable the LiteCP required-port profile? SSH, web and panel access and unrelated rules stay enabled.':'Allow the ports required by LiteCP and installed services? External provider firewalls are configured separately.',{title:disable?'Disable LiteCP Firewall':'Apply LiteCP Firewall',confirmText:disable?'Disable profile':'Apply firewall',danger:disable}))return;
    busy=true;button.disabled=true;button.textContent=disable?'Disabling…':'Applying…';
    try{await cp.api('/api/firewall/apply-litecp',{method:disable?'DELETE':'POST',body:{}});busy=false;if(!await refresh())throw Error('Firewall status could not be verified. Refresh and check the rules.');if(enabled===disable)throw Error('Firewall state does not match the requested change. Review the rules.');cp.showToast(disable?'LiteCP firewall disabled':'LiteCP firewall applied','Server rules refreshed.');}catch(error){cp.handleError(error);}finally{busy=false;await refresh();}
  };
  const navigate=window.LiteCPFeatures.navigate;
  window.LiteCPFeatures.navigate=page=>{
    navigate?.(page);
    if(page==='firewall')refresh();
  };
  if(heading.closest('[data-page-view="firewall"]').classList.contains('active'))refresh();
})();
