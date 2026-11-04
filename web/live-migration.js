(() => {
  "use strict";
  const cp=window.LiteCP,$=s=>document.querySelector(s),esc=cp.escapeHTML;
  const page=$('[data-page-view="migrations"]');
  page.querySelector('.page-heading p').textContent='Connect to a live cPanel/Plesk Linux server with root SSH, or upload an archive. Review every restore before changing DNS.';
  page.querySelector('.page-heading').insertAdjacentHTML('afterend',`<div class="info-callout"><div><strong>Full backup capture is separate from activating services.</strong><p>Native backups retain source account data. Website files and compatible SQL can be restored below. cPanel mailbox accounts, messages and compatible cron jobs are imported automatically. DNS, SSL, forwarders and reseller/package settings require separate review. Unsupported cron syntax is imported disabled. Keep the source online until every required service is verified.</p></div></div>
    <form class="panel live-source-form" id="liveSourceForm"><div class="panel-head"><div><h2>Connect to live source</h2><p>Step 1: Connect to your old server</p></div></div><div class="settings-body"><div class="live-source-fields">
    <label class="field"><span>Source panel</span><select name="platform"><option value="cpanel">cPanel / WHM</option><option value="plesk">Plesk for Linux</option><option value="litecp">LiteCP</option></select></label>
    <label class="field"><span>Old server IP or hostname</span><input name="host" required autocomplete="off" placeholder="source.example.com"></label>
    <label class="field"><span>SSH port</span><input name="port" type="number" min="1" max="65535" value="22" required></label>
    <label class="field" id="liveFingerprintField"><span>SSH host-key fingerprint</span><input id="liveFingerprint" name="fingerprint" required autocomplete="off" placeholder="SHA256:…"><small>Verify independently on the source: ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub -E sha256</small></label>
    <label class="field"><span>Authentication</span><select name="authMethod" id="liveAuthMethod"><option value="key">SSH private key</option><option value="password">Root password</option></select></label><label class="field hidden" id="livePasswordField"><span>Root SSH password</span><input id="liveRootPassword" name="password" type="password" maxlength="1024" autocomplete="off"><small>The SSH host key is detected automatically and shown for confirmation before login. The source must allow root password SSH login; interactive MFA is not supported.</small></label><label class="field" id="liveKeyField"><span>Root SSH private key file</span><input id="livePrivateKey" type="file" required><small>Use an unencrypted, dedicated migration key. Plesk for Windows is not supported.</small></label></div>
    <p class="feature-note">The key is stored with root-only permissions; passwords are encrypted on LiteCP, never in the browser’s saved settings. Use “Forget source credentials” after capture. Python 3 and the panel’s native backup CLI must be installed on the source. Direct transfer is capped at 8 GiB per archive and checked against available disk space.</p>
    <button class="button button-primary" type="submit">Connect and find websites</button><p id="liveConnectStatus" role="status"></p></div></form>
    <article class="panel live-jobs-panel"><div class="panel-head"><div><h2>Live migration jobs</h2><p>Native backup, checksum verification and compatible restore review</p></div><div class="feature-actions"><button class="button button-danger" id="liveJobsCancelAll" type="button" hidden>Cancel all</button><button class="button button-secondary" id="liveJobsRefresh" type="button">Refresh jobs</button></div></div><div id="liveJobs"><div class="empty-state"><p>No source connected yet.</p></div></div></article>
    <form class="panel hidden live-database-form" id="liveDatabaseForm"><div class="panel-head"><div><h2>Restore additional databases</h2><p>Use one empty managed target per source database. Application credentials must be updated separately.</p></div></div><div class="settings-body"><div class="live-source-fields"><label class="field"><span>Source SQL dump</span><select name="sql" id="liveSQL" required></select></label><label class="field"><span>Empty target database</span><select name="databaseID" id="liveTargetDatabase" required></select></label><label class="field"><span>Target database password</span><input name="password" type="password" autocomplete="off" required></label></div><label class="live-ack"><input type="checkbox" name="confirm" required> I confirm this target database is empty. If import fails, only that previously empty target may be reset.</label><button class="button button-secondary" type="submit">Restore selected database</button><p id="liveDatabaseStatus" role="status"></p></div></form>`);
  $('#liveAuthMethod').addEventListener('change',()=>{const password=$('#liveAuthMethod').value==='password';$('#livePasswordField').classList.toggle('hidden',!password);$('#liveKeyField').classList.toggle('hidden',password);$('#livePrivateKey').required=!password;$('#liveFingerprintField').classList.toggle('hidden',password);$('#liveFingerprint').required=!password;$('#liveRootPassword').required=password;$('#liveRootPassword').value='';$('#livePrivateKey').value='';});
  const help=document.createElement('details');help.className='migration-connection-help';help.innerHTML='<summary>Connection help and transfer limits</summary>';const note=$('#liveSourceForm .feature-note');note.before(help);help.append(note);
  $('#liveAuthMethod').value='password';$('#liveAuthMethod').dispatchEvent(new Event('change'));
  const jobsLink=document.createElement('button');jobsLink.type='button';jobsLink.className='button button-secondary';jobsLink.textContent='View migration jobs';jobsLink.onclick=()=>cp.navigate('migrations/jobs');$('#liveSourceForm .settings-body').append(jobsLink);
  let jobs=[],selectedJob=null,initialized=false,pollTimer=null;const accountSelections=new Map(),expandedJobs=new Set();
  const active=job=>job.status==='running'||job.status==='connecting'||job.status==='restoring';
  const cancellable=job=>['queued','running','connecting','restoring','review','awaiting-overwrite','restore-review','captured','interrupted','restored-review'].includes(job.status);
  const migrationStages=['connection','selection','capture','transfer','verify','extract','preflight','restore','result'];
  function stageProgress(job){
    let index=migrationStages.indexOf(job.stage);if(index<0)index=job.status==='queued'?1:0;
    return `<ol class="migration-stage-list" aria-label="Migration progress">${migrationStages.map((stage,i)=>`<li class="${i<index?'done':i===index?'current':''}"><span>${i+1}</span>${esc(stage==='selection'?'website':stage)}</li>`).join('')}</ol>`;
  }
  // Rank the complete pending queue before pagination or page filtering.
  function migrationQueuePositions(items) {
    const timestamp = value => String(value || '').replace(/^(.*T\d{2}:\d{2}:\d{2})(?:\.(\d+))?Z$/, (_, base, digits) => base + '.' + (digits || '').padEnd(9, '0') + 'Z');
    return new Map(items.filter(job => job.status === 'queued').slice().sort((a,b) => timestamp(a.createdAt).localeCompare(timestamp(b.createdAt)) || a.id.localeCompare(b.id)).map((job,index) => [job.id,index+1]));
  }
  function migrationRowProgress(job) {
    if (retryingJobs.has(job.id)) return '<div class="migration-row-progress migration-retry-progress" role="progressbar" aria-label="Requesting migration retry" aria-valuetext="Waiting for the server to accept this retry"><span class="migration-row-track"><span></span></span><small>Retrying...</small></div>';
    if (!active(job)) return '';
    const measured = job.progress && Number.isFinite(Number(job.progress.percent));
    const stageIndex = Math.max(0, migrationStages.indexOf(job.stage));
    const percent = measured ? Math.max(0, Math.min(100, Number(job.progress.percent))) : Math.min(99, Math.floor(stageIndex / migrationStages.length * 100));
    const label = measured ? 'Current transfer' : 'Workflow stages completed';
    return `<div class="migration-row-progress" title="${esc(label)}" role="progressbar" aria-label="${esc(label)}" aria-valuemin="0" aria-valuemax="100" aria-valuenow="${percent}"><span class="migration-row-track"><span style="width:${percent}%"></span></span><small>${percent}% <span>${measured ? 'transfer' : 'stages'}</span></small></div>`;
  }
  function migrationReplacements(items) {
    const replacements=new Map(),latestBySource=new Map();
    const sourceKey=job=>JSON.stringify([job.source?.platform,job.source?.host,job.source?.port,job.source?.fingerprint,job.source?.authMethod,job.account]);
    for(const job of items){
      if(!job.account||!Number.isFinite(Date.parse(job.createdAt))||['connected','connecting','cancelled','canceled'].includes(job.status))continue;
      const key=sourceKey(job),latest=latestBySource.get(key);
      if(!latest||Date.parse(job.createdAt)>Date.parse(latest.createdAt))latestBySource.set(key,job);
    }
    for(const job of items){
      if(!job.account||!['failed','interrupted'].includes(job.status))continue;
      const newer=latestBySource.get(sourceKey(job));
      if(newer&&Date.parse(newer.createdAt)>Date.parse(job.createdAt))replacements.set(job.id,newer);
    }
    return replacements;
  }
  let replacedAttempts=new Map();
  const pendingMigration = job => !replacedAttempts.has(job.id) && !active(job) && !['queued','connected','cancelled','canceled','complete','completed','restored'].includes(job.status);
  function migrationJobNotice(job){
    const replacement=replacedAttempts.get(job.id);
    if(replacement)return `<p class="feature-note">Previous attempt. A newer migration is ${esc(replacement.status)}. <button type="button" class="button button-secondary" data-live-open-job="${esc(replacement.id)}">View latest attempt</button></p>`;
    if(job.status==='awaiting-overwrite')return `<div class="feature-actions"><p class="feature-note">Backup copied and verified. This website already exists; confirm overwrite to continue restoring.</p><button type="button" class="button button-danger" data-live-overwrite="${esc(job.id)}">Review overwrite</button></div>`;
    return '';
  }
  function pendingRetryAction(job){
    if(!pendingMigration(job)||job.status==='awaiting-overwrite')return null;
    if(job.status==='restored-review')return !job.artifactsCleaned && job.source.platform==='cpanel' && job.cpanelWebsiteId && (job.nativeReady||job.preview) ? 'restore' : null;
    if(!job.artifactsCleaned&&(job.nativeReady||job.preview)&&['review','captured','restore-review'].includes(job.status))return 'restore';
    if(job.credentialsRetained&&(job.artifactsCleaned||['failed','interrupted','captured','restore-review'].includes(job.status)))return 'capture';
    return null;
  }
  const retryingJobs=new Set();
  let retryPendingBusy=false;
  function renderJobs(){
    replacedAttempts=migrationReplacements(jobs);
    const queuePositions = migrationQueuePositions(jobs);
    const section=location.hash.split('/')[1],allJobs=jobs,visibleJobs=section==='jobs'?jobs.filter(job=>active(job)||job.status==='queued'):section==='pending'?jobs.filter(pendingMigration):section==='completed'?jobs.filter(job=>!active(job)&&job.status!=='queued'&&job.status!=='connected'&&!pendingMigration(job)):jobs;
    const heading = page.querySelector('.live-jobs-panel h2');
    if (section === 'jobs') {
      const total = visibleJobs.filter(job => job.account).length;
      heading.innerHTML = `Live Migration Jobs <span class="badge migration-total" aria-label="${total} migrating websites">${total}</span>`;
    }
    // Sort before pagination so active work stays on the first page.
    if (section !== 'completed') visibleJobs.sort((a,b) => Number(active(b)) - Number(active(a)) || (a.status === 'queued' && b.status === 'queued' ? queuePositions.get(a.id) - queuePositions.get(b.id) : 0));
    jobs=visibleJobs;
    $('#liveJobs').innerHTML=jobs.length?jobs.map(job=>{const account=job.accounts?.find(item=>item.id===job.account),domain=account?.domain||job.account||'Connection';return `<section class="live-job" data-job-status="${esc(job.status)}" data-job-platform="${esc(job.source.platform)}" data-job-id="${esc(job.id)}"><div class="live-job-heading ${active(job)||retryingJobs.has(job.id)?'has-progress':''}"><strong>${esc(job.source.platform)} · ${esc(domain)} · ${esc(job.source.host)}</strong>${migrationRowProgress(job)}<span class="badge migration-status">${job.status === 'queued' ? `Queued #${queuePositions.get(job.id)}` : esc(job.status)}</span><button type="button" class="button button-secondary live-job-view" data-live-view="${esc(job.id)}" aria-expanded="${expandedJobs.has(job.id)}" aria-controls="migration-details-${esc(job.id)}">${expandedJobs.has(job.id)?"Hide":"View"}</button></div>${migrationJobNotice(job)}<div class="live-job-details" id="migration-details-${esc(job.id)}" ${expandedJobs.has(job.id)?"":"hidden"}>${stageProgress(job)}<p>${esc(job.message)}</p>${active(job)?`<div class="live-migration-progress ${job.progress?'is-measured':''}" role="progressbar" aria-label="Current file transfer progress" ${job.progress?`aria-valuemin="0" aria-valuemax="100" aria-valuenow="${job.progress.percent}"`:''} aria-valuetext="${esc(job.message)}"><span ${job.progress?`style="width:${job.progress.percent}%"`:''}></span></div><small class="live-progress-caption">${job.progress?`${job.progress.percent}% of ${esc(job.progress.file==='portable-backup'?'website archive':'native backup')} transferred · ${cp.formatBytes(job.progress.bytes)} / ${cp.formatBytes(job.progress.total)} — this percentage covers only the current file transfer`:'Working on '+esc(job.stage||'migration')+' — no honest percentage is available for this stage'}</small>`:''}${job.sourceExportRetained?`<small>Source export retained securely for recovery review.</small>`:''}${job.nativeReady?`<small>Verified native archive · ${cp.formatBytes(job.nativeBytes)}<br>SHA256: <code>${esc(job.nativeSha256)}</code></small>`:''}${job.warnings?.length?`<ul class="live-warnings">${job.warnings.map(message=>`<li>${esc(message)}</li>`).join('')}</ul>`:''}${job.status==='connected'&&job.credentialsRetained?`<form class="live-capture-form" id="migration-capture-${esc(job.id)}" data-live-capture="${esc(job.id)}"><div class="migration-website-picker" data-website-picker><section><h3>2. Choose source websites</h3><input type="search" data-account-search placeholder="Search websites" aria-label="Search source websites"><button type="button" class="button button-secondary" data-select-accounts>Select all visible</button><div data-source-websites>${job.accounts.map(account=>`<div class="migration-source-website" draggable="true" data-account="${esc(account.id)}"><span>${esc(account.domain||account.id)}<small>${esc(account.id)}</small></span><button type="button" class="button button-secondary" data-pick-account="${esc(account.id)}">Add →</button></div>`).join('')}</div></section><section data-migration-drop tabindex="0" aria-label="Selected websites; drop source websites here"><h3>3. Review migration</h3><p data-selected-website>Add or drag accounts here.</p><button type="button" class="button button-secondary" data-clear-account hidden>Remove selection</button><input type="hidden" name="account" required></section></div><label class="live-ack"><input type="checkbox" name="confirm" required> I understand LiteCP will create and verify a source backup before restoring. Existing domains stop for separate overwrite confirmation. DNS is never changed automatically.</label></form>`:''}<div class="feature-actions live-capture-actions">${job.status==='connected'&&job.credentialsRetained?`<button class="button button-primary" type="submit" form="migration-capture-${esc(job.id)}">Start migration</button>`:''}${!job.artifactsCleaned&&(job.nativeReady||job.preview)&&['review','captured','restore-review','restored-review'].includes(job.status)&&(job.status!=='restored-review'||pendingRetryAction(job)==='restore')?`<button class="button button-primary" data-live-auto="${esc(job.id)}" ${['captured','restore-review'].includes(job.status)?'data-live-retry-restore':''}>${job.status==='restored-review'?'Retry checks':['captured','restore-review'].includes(job.status)?'Retry restore':'Continue restore'}</button>`:''}${(job.artifactsCleaned||['interrupted','failed','restore-review','restored-review','captured'].includes(job.status))&&job.credentialsRetained?`<button class="button button-primary" data-live-retry="${esc(job.id)}">Retry capture</button>`:''}${job.nativeReady?`<a class="button button-secondary" href="/api/live-migrations/download?id=${encodeURIComponent(job.id)}">Download native backup</a>`:''}${job.preview?`<button class="button button-secondary" data-live-review="${esc(job.id)}">Review staged data</button>`:''}${job.credentialsRetained&&!active(job)&&job.status!=='queued'?`<button class="button button-ghost" data-live-forget="${esc(job.id)}">Forget source credentials</button>`:''}</div><small>Job ${esc(job.id)}</small></div></section>`}).join(''):'<div class="empty-state"><p>No live migration jobs. Connect a source server to begin.</p></div>';
    jobs=allJobs;
    $('#liveJobsCancelAll').hidden=!allJobs.some(cancellable);
    const retryPending=$('#liveJobsRetryPending');
    retryPending.hidden=section!=='pending';
    retryPending.disabled=retryPendingBusy||!allJobs.some(job=>pendingRetryAction(job));
    if(!visibleJobs.length&&section==='pending')$('#liveJobs p').textContent='No migrations need review or further action.';
    if(!visibleJobs.length&&section==='completed')$('#liveJobs p').textContent='No completed migration jobs yet.';
    if(!visibleJobs.length&&section==='jobs')$('#liveJobs p').textContent='No queued or running migration jobs.';
    document.querySelectorAll('[data-live-auto]:not([data-live-retry-restore]),a[href^="/api/live-migrations/download"]').forEach(control=>control.remove());
    document.querySelectorAll('.live-job').forEach(section=>{const job=allJobs.find(item=>item.id===section.dataset.jobId);if(job&&cancellable(job)){const actions=section.querySelector('.live-job-heading');actions?.insertAdjacentHTML('beforeend',`<button class="button button-danger" type="button" data-live-cancel="${esc(job.id)}" aria-label="Cancel migration for ${esc(job.accounts?.find(account=>account.id===job.account)?.domain||job.account||job.id)}">Cancel</button>`);}});
    document.querySelectorAll('.live-job[data-job-status="review"] [data-live-review]').forEach(control=>control.remove());
    document.querySelectorAll('[data-live-capture]').forEach(form=>{const selected=accountSelections.get(form.dataset.liveCapture);if(selected)selected.forEach(id=>pickAccount(form,id));});
  }
  async function loadJobs(){
    if(cp.demo){$('#liveJobs').innerHTML='<div class="empty-state"><p>Live migration requires the installed Linux server.</p></div>';return;}
    try{const result=await cp.api('/api/live-migrations');jobs=result.items;for(const job of jobs){if(active(job)){const current=await cp.api('/api/live-migrations?id='+encodeURIComponent(job.id));Object.assign(job,current);}}renderJobs();clearTimeout(pollTimer);if(jobs.some(job=>active(job)||job.status==='queued'||job.status==='review'))pollTimer=setTimeout(loadJobs,5000);}
    catch(error){$('#liveJobs').textContent=error.message;}
  }
  async function revealMigrationJob(job){
    expandedJobs.add(job.id);
    cp.navigate(active(job)||job.status==='queued'?'migrations/jobs':pendingMigration(job)?'migrations/pending':'migrations/completed');
    await loadJobs();
  }
  function pickAccount(form,id){const job=jobs.find(item=>item.id===form.dataset.liveCapture),account=job?.accounts.find(item=>item.id===id);if(!account)return;const selected=accountSelections.get(form.dataset.liveCapture)||[];if(!selected.includes(id))selected.push(id);accountSelections.set(form.dataset.liveCapture,selected);form.elements.account.value=JSON.stringify(selected);const box=form.querySelector('[data-selected-website]');box.replaceChildren();for(const key of selected){const value=job.accounts.find(x=>x.id===key);const line=document.createElement('div');line.textContent=value?.domain||key;const remove=document.createElement('button');remove.type='button';remove.className='button button-secondary';remove.textContent='Remove';remove.onclick=()=>{const remaining=selected.filter(x=>x!==key);accountSelections.set(form.dataset.liveCapture,remaining);form.querySelector('[data-clear-account]').click();remaining.forEach(x=>pickAccount(form,x));};line.append(remove);box.append(line)}form.querySelector('[data-clear-account]').hidden=false;form.querySelectorAll('[data-account]').forEach(row=>{const chosen=selected.includes(row.dataset.account);row.classList.toggle('selected',chosen);row.querySelector('[data-pick-account]').disabled=chosen;});}
  $('#liveJobs').addEventListener('dragstart',event=>{const row=event.target.closest('[data-account]');if(!row)return;event.dataTransfer.setData('text/plain',JSON.stringify({job:row.closest('form').dataset.liveCapture,account:row.dataset.account}));event.dataTransfer.effectAllowed='copy';});
  $('#liveJobs').addEventListener('dragover',event=>{if(event.target.closest('[data-migration-drop]'))event.preventDefault();});
  $('#liveJobs').addEventListener('drop',event=>{const drop=event.target.closest('[data-migration-drop]');if(!drop)return;event.preventDefault();try{const value=JSON.parse(event.dataTransfer.getData('text/plain')),form=drop.closest('form');if(value.job===form.dataset.liveCapture)pickAccount(form,value.account);}catch{}});
  $('#liveJobs').addEventListener('click',event=>{const all=event.target.closest('[data-select-accounts]');if(all){const form=all.closest('form');form.querySelectorAll('[data-account]:not([hidden])').forEach(row=>pickAccount(form,row.dataset.account));return;}const add=event.target.closest('[data-pick-account]'),clear=event.target.closest('[data-clear-account]');if(add)pickAccount(add.closest('form'),add.dataset.pickAccount);if(clear){const form=clear.closest('form');accountSelections.delete(form.dataset.liveCapture);form.elements.account.value='';form.querySelector('[data-selected-website]').textContent='Add or drag accounts here.';clear.hidden=true;form.querySelectorAll('[data-account]').forEach(row=>{row.classList.remove('selected');row.querySelector('[data-pick-account]').disabled=false;});}});
  $('#liveJobs').addEventListener('input',event=>{if(!event.target.matches('[data-account-search]'))return;const value=event.target.value.toLowerCase();event.target.closest('[data-website-picker]').querySelectorAll('[data-account]').forEach(row=>row.hidden=!row.textContent.toLowerCase().includes(value));});
  $('#liveJobsRefresh').onclick=loadJobs;
  const retryPendingButton=document.createElement('button');retryPendingButton.id='liveJobsRetryPending';retryPendingButton.type='button';retryPendingButton.className='button button-primary';retryPendingButton.textContent='Retry pending';retryPendingButton.hidden=true;$('#liveJobsCancelAll').before(retryPendingButton);
  retryPendingButton.onclick=async()=>{
    if(retryPendingBusy)return;
    if(!await window.LiteCPDialogs.confirm('Retry pending migrations using retained files where available, or capture again when source credentials are available? Existing websites still require separate overwrite confirmation.'))return;
    retryPendingBusy=true;retryPendingButton.disabled=true;
    let retried=0,skipped=0;const errors=[];
    try{
      const latest=await cp.api('/api/live-migrations');
      const pending=(latest.items||[]).filter(pendingMigration);
      for(let i=0;i<pending.length;i++){
        const job=pending[i];
        retryPendingButton.textContent=`Retrying ${i+1} of ${pending.length}`;
        retryingJobs.add(job.id);renderJobs();
        try{
          Object.assign(job,await cp.api('/api/live-migrations?id='+encodeURIComponent(job.id)));
          const action=pendingRetryAction(job);
          if(!action){skipped++;continue;}
          if(action==='restore')await cp.api('/api/live-migrations/auto-restore',{method:'POST',body:{id:job.id,overwrite:false,background:true}});
          else await cp.api('/api/live-migrations',{method:'POST',body:{action:'retry',id:job.id,confirm:true}});
          retried++;
          await loadJobs();
        }catch(error){errors.push(`${job.accounts?.find(account=>account.id===job.account)?.domain||job.account||job.id}: ${error.message}`);}finally{retryingJobs.delete(job.id);renderJobs();}
      }
      await loadJobs();
      cp.showToast('Pending retries requested',`${retried} retry request(s) accepted; ${skipped} require individual review. Check each job for its result.`);
      if(errors.length)cp.handleError(new Error(errors.join('; ')));
    }catch(error){cp.handleError(error);}
    finally{retryPendingBusy=false;retryPendingButton.textContent='Retry pending';await loadJobs();}
  };
  $('#liveJobsCancelAll').onclick=async event=>{const button=event.currentTarget;if(!await window.LiteCPDialogs.confirm('Cancel all queued, running and pending-review migrations? Captured files and source credentials will be kept.'))return;button.disabled=true;try{const result=await cp.api('/api/live-migrations/cancel',{method:'POST',body:{action:'cancel-all',confirm:true}});await loadJobs();if(result.errors?.length)throw new Error(`${result.cancelled} cancelled. Some jobs could not be cancelled: ${result.errors.join('; ')}`);cp.showToast('Migrations cancelled',`${result.cancelled} migration job(s) cancelled. Running workers are stopping.`);}catch(error){cp.handleError(error);}finally{button.disabled=false;}};
  const cleanupButton=document.createElement('button');cleanupButton.type='button';cleanupButton.className='button button-danger';cleanupButton.textContent='Clean migration temporary files';$('#liveJobsRefresh').before(cleanupButton);
  cleanupButton.onclick=async()=>{
    if(!await window.LiteCPDialogs.confirm('Delete temporary capture archives, extracted folders for stopped migrations, and unused uploaded migration stages? Restored websites, databases, backups, history, credentials and active migrations are kept.'))return;
    cleanupButton.disabled=true;
    try{
      const result=await cp.api('/api/live-migrations',{method:'POST',body:{action:'cleanup',confirm:true}});
      await loadJobs();
      const summary=result.removedPaths+' temporary paths removed; '+result.cleaned+' stopped jobs cleaned; '+result.stages+' additional staging folders cleaned. '+result.skipped+' protected or unrecognized entries kept.';
      cp.showToast(result.issues?.length?'Migration cleanup needs attention':'Migration cleanup complete',summary+(result.issues?.length?' '+result.issues.join(' | '):' Refresh Disk Usages to rescan storage.'),result.issues?.length?'error':'success');
    }catch(error){cp.handleError(error);}finally{cleanupButton.disabled=false;}
  };
  document.addEventListener('migration-section-change',loadJobs);
  $('#liveSourceForm').onsubmit=async event=>{
    event.preventDefault();const form=event.currentTarget,button=form.querySelector('button[type="submit"]'),status=$('#liveConnectStatus');
    if(cp.demo){status.textContent='Live migration requires the installed Linux server.';return;}
    const passwordMode=$('#liveAuthMethod').value==='password',key=$('#livePrivateKey').files[0];if(!passwordMode&&(!key||key.size>65536)){status.textContent='Choose a private key file up to 64 KiB.';return;}
    button.disabled=true;status.textContent='Verifying host identity and discovering accounts…';
    try{const values=new FormData(form);const source={authMethod:values.get('authMethod'),platform:values.get('platform'),host:String(values.get('host')).trim(),port:Number(values.get('port')),fingerprint:String(values.get('fingerprint')||'').trim()};if(passwordMode){status.textContent='Detecting source SSH host key…';const detected=await cp.api('/api/live-migrations',{method:'POST',body:{action:'fingerprint',source}});if(!await window.LiteCPDialogs.confirm(`Trust the SSH host key for ${source.host}:${source.port}?\n\n${detected.fingerprint}\n\nVerify this identity independently when possible. Your root password is sent only after you confirm.`)){status.textContent='Connection cancelled. No password was sent.';return;}source.fingerprint=detected.fingerprint;}await cp.api('/api/live-migrations',{method:'POST',body:{action:'connect',source,privateKey:passwordMode?'':await key.text(),password:passwordMode?String(values.get('password')):''}});$('#livePrivateKey').value='';$('#liveRootPassword').value='';status.textContent='Connection verified. Review the discovered account below.';await loadJobs();}
    catch(error){status.textContent=error.message;await loadJobs();}finally{$('#liveRootPassword').value='';button.disabled=false;}
  };
  $('#liveJobs').addEventListener('submit',async event=>{
    const form=event.target.closest('[data-live-capture]');if(!form)return;event.preventDefault();if(!form.reportValidity())return;
    if(!form.elements.account.value){cp.showToast('Select a website','Drag a source website to the right or use Add.');return;}const button=document.querySelector(`button[type="submit"][form="${CSS.escape(form.id)}"]`);button.disabled=true;
    try{await cp.api('/api/live-migrations',{method:'POST',body:{action:'queue',id:form.dataset.liveCapture,accounts:JSON.parse(form.elements.account.value),confirm:true}});await loadJobs();cp.navigate('migrations/jobs');}
    catch(error){cp.handleError(error);button.disabled=false;}
  });
  $('#liveJobs').addEventListener('click',async event=>{
    const view=event.target.closest('[data-live-view]');if(view){const id=view.dataset.liveView,open=!expandedJobs.has(id);if(open)expandedJobs.add(id);else expandedJobs.delete(id);document.getElementById(view.getAttribute('aria-controls')).hidden=!open;view.setAttribute('aria-expanded',String(open));view.textContent=open?'Hide':'View';return;}
    const cancelJob=event.target.closest('[data-live-cancel]');if(cancelJob){if(!await window.LiteCPDialogs.confirm('Cancel this migration? Captured files and source credentials will be kept.'))return;cancelJob.disabled=true;try{await cp.api('/api/live-migrations/cancel',{method:'POST',body:{action:'cancel',id:cancelJob.dataset.liveCancel,confirm:true}});await loadJobs();cp.showToast('Migration cancelled','The migration stopped without deleting retained data.');}catch(error){cp.handleError(error);cancelJob.disabled=false;}return;}
    const automatic=event.target.closest('[data-live-auto],[data-live-overwrite]');if(automatic){const id=automatic.dataset.liveAuto||automatic.dataset.liveOverwrite,job=jobs.find(x=>x.id===id),domain=job?.accounts.find(x=>x.id===job.account)?.domain;const overwrite=!!automatic.dataset.liveOverwrite;let confirmed='';if(overwrite){confirmed=await window.LiteCPDialogs.prompt(`Overwrite ${domain}? A recovery backup will be created first. Existing files will be retained separately and existing databases will not be deleted. Type the domain to confirm.`, '', {title:'Overwrite existing website',label:'Domain',confirmText:'Back up and overwrite',danger:true});if(confirmed!==domain)return;}automatic.disabled=true;try{await cp.api('/api/live-migrations/auto-restore',{method:'POST',body:{id,overwrite,confirmDomain:confirmed}});await loadJobs();await cp.refresh();}catch(error){cp.handleError(error);automatic.disabled=false;}return;}
    const openJob=event.target.closest('[data-live-open-job]');if(openJob){const job=jobs.find(item=>item.id===openJob.dataset.liveOpenJob);if(job)await revealMigrationJob(job);return;}
    const retry=event.target.closest('[data-live-retry]');if(retry){retry.disabled=true;try{const job=await cp.api('/api/live-migrations',{method:'POST',body:{action:'retry',id:retry.dataset.liveRetry,confirm:true}});await revealMigrationJob(job);if(job.status==='awaiting-overwrite')cp.showToast('Backup ready for restore','This website already exists. Use Review overwrite to confirm replacement.');}catch(error){cp.handleError(error);retry.disabled=false;}return;}
    const review=event.target.closest('[data-live-review]'),forget=event.target.closest('[data-live-forget]');
    if(review){document.dispatchEvent(new Event('migration-show-review'));selectedJob=jobs.find(job=>job.id===review.dataset.liveReview);if(!selectedJob?.preview)return;window.LiteCPFeatures.reviewMigration(selectedJob.preview);$('#liveSQL').innerHTML=selectedJob.preview.databases.map(value=>`<option>${esc(value)}</option>`).join('');$('#liveTargetDatabase').innerHTML=(cp.state().databases||[]).map(value=>`<option value="${esc(value.id)}">${esc(value.name)}</option>`).join('');$('#liveDatabaseForm').classList.toggle('hidden',!selectedJob.preview.databases.length);$('#liveDatabaseForm').reset();$('#migrationImportForm').scrollIntoView({behavior:'smooth',block:'start'});}
    if(forget){if(!await window.LiteCPDialogs.confirm('Remove this job’s stored SSH key/password and pinned-host file from LiteCP? Native backups and source data will be kept.'))return;try{await cp.api('/api/live-migrations',{method:'POST',body:{action:'forget',id:forget.dataset.liveForget,confirm:true}});await loadJobs();}catch(error){cp.handleError(error);}}
  });
  $('#liveDatabaseForm').onsubmit=async event=>{
    event.preventDefault();const form=event.currentTarget;if(!selectedJob||!form.reportValidity())return;const data=new FormData(form),button=form.querySelector('button[type="submit"]');button.disabled=true;$('#liveDatabaseStatus').textContent='Restoring the selected database…';
    try{await cp.api('/api/migrations/database',{method:'POST',body:{id:selectedJob.preview.id,sql:data.get('sql'),databaseID:data.get('databaseID'),password:data.get('password'),confirm:data.has('confirm')}});form.elements.password.value='';$('#liveDatabaseStatus').textContent='Database restored. Verify its application configuration before cutover.';await cp.refresh();}
    catch(error){$('#liveDatabaseStatus').textContent=error.message;}finally{button.disabled=false;}
  };
  const previous=window.LiteCPFeatures.render;
  window.LiteCPFeatures.render=state=>{previous(state);if(!initialized){initialized=true;loadJobs();}};
  if(cp.state()){initialized=true;loadJobs();}
})();
