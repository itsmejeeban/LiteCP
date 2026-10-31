(() => {
  "use strict";
  const cp=window.LiteCP, panel=document.createElement("article");
  panel.className="panel"; panel.id="webmailSetupPanel";
  panel.innerHTML=`<div class="panel-head"><div><h2>First-time webmail setup</h2><p>Connect webmail to this server’s existing mailboxes.</p></div></div><form class="settings-body" id="webmailSetupForm"><div class="form-grid"><label>Public mail hostname<input name="hostname" placeholder="mail.example.com" required></label><label>Webmail HTTPS port<input name="port" type="number" min="1024" max="65535" value="8444" required></label><label>TLS certificate path<input name="certificate" placeholder="/etc/letsencrypt/live/mail.example.com/fullchain.pem" required></label><label>Private key path<input name="privateKey" placeholder="/etc/letsencrypt/live/mail.example.com/privkey.pem" required></label></div><p>Point the hostname to this server and install a trusted certificate for it first. IMAP (993) and SMTP (465) use the local mail server. Mailbox passwords stay unchanged.</p><p role="status" data-setup-status>Loading configuration…</p><div class="row-actions"><button type="submit" class="button button-primary">Configure webmail</button><button type="button" class="button button-secondary" data-setup-refresh>Refresh status</button><a class="button button-secondary hidden" data-setup-open target="_blank" rel="noopener">Open webmail</a></div></form>`;
  document.querySelector('[data-page-view="email"]').append(panel);
  const form=panel.querySelector("form"),status=panel.querySelector('[data-setup-status]'),submit=form.querySelector('[type="submit"]');
  let populated=false,timer;
  async function refresh(){
    clearTimeout(timer);
    try{
      const result=await cp.api('/api/mail/webmail-configure');
      if(!populated){for(const key of ['hostname','port','certificate','privateKey'])form.elements[key].value=result[key]||'';populated=true;}
      const running=result.status==='running',configured=result.configured;
      window.dispatchEvent(new CustomEvent('litecp:webmail-status',{detail:{configured:!!configured,status:result.status}}));
      status.textContent=running?'Configuring webmail in the background. You can close this page.':configured?'Webmail is configured.':result.status==='failed'||result.status==='interrupted'?'Setup did not finish. Review '+result.logPath+' on the server, correct the details, then retry.':'Enter the details above to configure webmail.';
      submit.disabled=running||configured;for(const field of form.querySelectorAll('input'))field.disabled=running||configured;
      const open=panel.querySelector('[data-setup-open]');open.classList.toggle('hidden',!configured||!result.url);if(result.url&&/^https:\/\//.test(result.url))open.href=result.url;
      if(running&&!panel.closest('.email-tab-panel')?.classList.contains('hidden'))timer=setTimeout(refresh,4000);
    }catch(error){status.textContent=error.message;}
  }
  form.onsubmit=async event=>{
    event.preventDefault();if(!form.reportValidity())return;
    if(!await window.LiteCPDialogs.confirm('Configure Roundcube webmail and its local mail connections? Required packages will be installed, the webmail port opened, and mail/PHP/Nginx services restarted. Existing mailboxes are kept.',{title:'Configure webmail',confirmText:'Configure webmail'}))return;
    submit.disabled=true;status.textContent='Starting webmail setup…';
    try{const body=Object.fromEntries(new FormData(form));body.port=Number(body.port);await cp.api('/api/mail/webmail-configure',{method:'POST',body});await refresh();}catch(error){status.textContent=error.message;submit.disabled=false;}
  };
  panel.querySelector('[data-setup-refresh]').onclick=refresh;
  if(location.hash==='#email/configure-webmail')refresh();
  window.addEventListener('hashchange',()=>{if(location.hash==='#email/configure-webmail')refresh();else clearTimeout(timer);});
})();
