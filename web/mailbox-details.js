(() => {
  'use strict';
  const cp=window.LiteCP;
  const dialog=document.createElement('dialog');
  dialog.className='mailbox-details-dialog';
  dialog.setAttribute('aria-labelledby','mailboxDetailsTitle');
  dialog.innerHTML='<div class="mailbox-details-head"><h2 id="mailboxDetailsTitle">Mailbox Details</h2><button type="button" class="button danger" data-close aria-label="Close mailbox details">&times;</button></div><div class="mailbox-details-body" data-body></div><div class="mailbox-details-actions"><button type="button" class="button button-secondary" data-copy>Copy settings</button><button type="button" class="button button-secondary" data-close>Close</button></div>';
  document.body.append(dialog);
  let request=0,settings='';
  dialog.addEventListener('click',async event=>{
    if(event.target.closest('[data-close]')){request++;dialog.close();return;}
    if(event.target.closest('[data-copy]')){
      try{await navigator.clipboard.writeText(settings);cp.showToast('Copied','Mailbox settings copied.');}
      catch{cp.showToast('Copy unavailable','Select and copy the settings in this dialog.','error');}
    }
  });
  dialog.addEventListener('cancel',()=>request++);
  document.querySelector('[data-page-view="email"]').addEventListener('click',async event=>{
    const button=event.target.closest('[data-mailbox-details]');if(!button)return;
    const mailbox=cp.state()?.mailboxes?.find(item=>item.id===button.dataset.mailboxDetails);if(!mailbox)return;
    const current=++request,body=dialog.querySelector('[data-body]'),copy=dialog.querySelector('[data-copy]');
    settings='';copy.disabled=true;body.textContent='Loading mailbox settings...';dialog.showModal();
    try{
      const data=await cp.api('/api/mail/dns?domain='+encodeURIComponent(mailbox.domain));
      if(current!==request||!dialog.open)return;
      const rows=window.LiteCPMailboxDetails.rows(mailbox.address,data.hostname);
      settings=rows.map(([name,value])=>name+': '+value).join('\n');
      body.replaceChildren();const table=document.createElement('table');table.className='data-table';const tbody=document.createElement('tbody');
      for(const [name,value] of rows){const row=document.createElement('tr'),label=document.createElement('th'),cell=document.createElement('td');label.scope='row';label.textContent=name;cell.textContent=value;row.append(label,cell);tbody.append(row);}table.append(tbody);body.append(table);
      const note=document.createElement('p');note.textContent='Use manual account setup in a mail client that supports IMAP/SMTP. Use the mailbox password, not your LiteCP administrator password. The mail hostname needs a matching trusted TLS certificate.';body.append(note);copy.disabled=false;
    }catch(error){if(current===request&&dialog.open)body.textContent='Cannot load mailbox settings: '+error.message;}
  });
})();
