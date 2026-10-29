/* Live test-server workflows. Never run against production. Credentials are environment-only. */
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const {spawnSync}=require('node:child_process');
const {chromium} = require('playwright');
const output = path.resolve(process.env.LITECP_QA_OUTPUT || 'docs/customer-qa-2026-10-28');
const base = process.env.LITECP_QA_URL || 'http://127.0.0.1:19190';
const domain = 'litimcsellcop.com';
const prefix = 'qa20261028';
const runID=Date.now().toString(36);
if (process.env.LITECP_QA_ALLOW_MUTATIONS !== 'test-server-2.31.109.187') throw Error('Explicit test-server authorization is required');
fs.mkdirSync(output,{recursive:true});
const report={startedAt:new Date().toISOString(),tests:[],errors:[]};
const password=crypto.randomBytes(24).toString('base64url');
function save(){fs.writeFileSync(path.join(output,'results.json'),JSON.stringify(report,null,2));}
async function test(name,work){try{const detail=await work();report.tests.push({name,status:'PASS',detail:detail||''});console.log('PASS '+name);}catch(error){const message=String(error.message).replaceAll(password,'[redacted]').replaceAll(process.env.LITECP_QA_PASSWORD||'\0','[redacted]');report.tests.push({name,status:'FAIL',detail:message.slice(0,600)});console.log('FAIL '+name+': '+message.slice(0,300));}save();}
(async()=>{
 const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true});
 const context=await browser.newContext({viewport:{width:1440,height:1000},acceptDownloads:true});
 const page=await context.newPage();page.setDefaultTimeout(20000);page.on('pageerror',e=>report.errors.push(e.message));
 const api=(url,method='GET',body)=>page.evaluate(async({url,method,body})=>window.LiteCP.api(url,{method,body}),{url,method,body});
 const navigate=async route=>{await page.evaluate(route=>{document.querySelectorAll('dialog[open]').forEach(d=>d.close());window.LiteCP.navigate(route);},route);await page.waitForTimeout(150);};
 async function response(url,work){const pending=page.waitForResponse(r=>new URL(r.url()).pathname===url&&r.request().method()!=='GET',{timeout:180000});await work();const r=await pending;const value=await r.json();if(!r.ok())throw Error(url+' '+r.status()+': '+value.error);return value;}
 async function closeCredentials(){await page.locator('#closeCredentials').waitFor({state:'visible'});await page.locator('#closeCredentials').click();await page.locator('#credentialsModal').waitFor({state:'hidden'});await page.evaluate(()=>document.querySelectorAll('dialog[open]').forEach(d=>d.close()));}
 const sites={};
 try{
  await page.goto(base,{waitUntil:'networkidle'});
  await test('Administrator login through browser',async()=>{await page.locator('#loginForm [name="username"]').fill('admin');await page.locator('#loginForm [name="password"]').fill(process.env.LITECP_QA_PASSWORD);await response('/api/login',()=>page.locator('#loginForm [type="submit"]').click());await page.locator('#appView').waitFor({state:'visible'});});
  for(const name of [domain,`${prefix}-a.${domain}`,`${prefix}-b.${domain}`]){
   await test('Website creation UI: '+name,async()=>{
    let state=await api('/api/state');let site=state.sites.find(s=>s.domain===name);
    if(site){sites[name]={site};if(name.startsWith(prefix+'-')){const db=state.databases.find(d=>d.siteId===site.id);if(db){await api('/api/databases/password','PUT',{id:db.id,password});sites[name].database={name:db.name,username:db.username,password};}}return 'Existing website preserved';}
    await navigate('websites');await page.locator('[data-page-view="websites"] [data-open-site-modal]').click();
    await page.locator('#siteAppType').selectOption('regular');await page.locator('#siteForm [name="domain"]').fill(name);await page.locator('#sitePHPVersion').selectOption('8.3');await page.locator('#siteNextStep').click();
    await page.locator('#siteForm [name="createDatabase"]').check();await page.locator('#siteForm [name="enableCron"]').uncheck();
    const result=await response('/api/sites',()=>page.locator('#siteForm [type="submit"]').click());sites[name]=result;await closeCredentials();
    state=await api('/api/state');if(!state.sites.some(s=>s.id===result.site.id))throw Error('Site missing after refresh');return 'Stored website and associated database';
   });
  }
  const a=sites[`${prefix}-a.${domain}`]?.site,b=sites[`${prefix}-b.${domain}`]?.site;
  if(!a||!b)throw Error('Independent test websites unavailable');
  const sid=a.id;
  await test('Duplicate domain rejected',async()=>{try{await api('/api/sites','POST',{domain:a.domain,appType:'regular',phpVersion:'8.3'});}catch(e){return 'Duplicate rejected';}throw Error('Duplicate accepted');});
  await test('File Manager website picker and workspace',async()=>{await navigate('file-manager');await page.locator('[data-page-view="file-manager"] select').selectOption(sid);await page.locator('[data-page-view="file-manager"] [type="submit"]').click();await page.locator('.cpfm-header').waitFor({state:'visible'});await page.waitForLoadState('networkidle');await page.evaluate(()=>window.LiteCP.goToFilePath('public_html'));});
  async function fileDialog(action,values){await page.locator(`.file-toolbar [data-fm-action="${action}"]`).click();for(const [key,value]of Object.entries(values))await page.locator(`#fmActionDialog [name="${key}"]`).fill(value);await page.locator('#fmActionDialog [type="submit"]').click();await page.waitForFunction(()=>!document.querySelector('#fmActionDialog').open||!document.querySelector('#fmActionDialog .fm-error').classList.contains('hidden'));if(await page.locator('#fmActionDialog').isVisible()){const error=await page.locator('#fmActionDialog .fm-error').textContent();await page.keyboard.press('Escape');throw Error(error);}}
  await test('Create directory through File Manager',async()=>{await fileDialog('new-folder',{name:prefix+'-'+runID,destination:a.root});const r=await api(`/api/files?siteId=${sid}&path=public_html`);if(!r.entries.some(e=>e.name===prefix+'-'+runID))throw Error('Directory missing');});
  await test('Create file through File Manager',async()=>{await page.evaluate(p=>window.LiteCP.goToFilePath(p),'public_html/'+prefix+'-'+runID);await fileDialog('new-file',{name:'hello.txt'});const r=await api(`/api/files?siteId=${sid}&path=public_html/${prefix}-${runID}`);if(!r.entries.some(e=>e.name==='hello.txt'))throw Error('File missing');});
  await test('Edit text and verify saved content',async()=>{await api('/api/files/content','PUT',{siteId:sid,path:`public_html/${prefix}-${runID}/hello.txt`,content:'LiteCP customer QA original\n',encoding:'UTF-8'});const r=await api(`/api/files/content?siteId=${sid}&path=public_html/${prefix}-${runID}/hello.txt`);if(r.content!=='LiteCP customer QA original\n')throw Error('Saved content differs');});
  await test('Directory traversal rejected',async()=>{try{await api(`/api/files/content?siteId=${sid}&path=../../etc/shadow`);}catch(e){return 'Traversal rejected';}throw Error('Traversal accepted');});
  await test('Cross-website path rejected',async()=>{try{await api(`/api/files/content?siteId=${sid}&path=${encodeURIComponent(b.root+'/index.php')}`);}catch(e){return 'Other website absolute path rejected';}throw Error('Other website path accepted');});
  await test('Upload HTML and PHP through File Manager',async()=>{
   await page.evaluate(()=>window.LiteCP.goToFilePath('public_html'));
   const db=sites[a.domain].database;
   const php=db?`<?php $m=new mysqli('localhost',${JSON.stringify(db.username)},${JSON.stringify(db.password)},${JSON.stringify(db.name)});$m->query('CREATE TABLE IF NOT EXISTS qa_probe (id INT PRIMARY KEY, value VARCHAR(80))');$m->query("REPLACE INTO qa_probe VALUES(1,'customer-qa')");echo $m->query('SELECT value FROM qa_probe WHERE id=1')->fetch_row()[0];`:'<?php echo "php-runtime-ok";';
   await page.locator('#fileUploadInput').setInputFiles([{name:`qa-index-${runID}.html`,mimeType:'text/html',buffer:Buffer.from('<!doctype html><title>LiteCP QA</title><h1>HTML customer QA</h1>')},{name:`qa-database-${runID}.php`,mimeType:'text/plain',buffer:Buffer.from(php)}]);
   await page.locator('#fmActionDialog [type="submit"]').click();await page.locator('#fmActionDialog').waitFor({state:'hidden'});
   await page.waitForTimeout(1000);const r=await api(`/api/files?siteId=${sid}&path=public_html`);if(!r.entries.some(e=>e.name===`qa-database-${runID}.php`))throw Error('PHP upload missing');
  });
  await test('PHP HTTP response executes database query',async()=>{const r=await context.request.get(`http://${a.domain}/qa-database-${runID}.php`);if(r.status()!==200||!(await r.text()).includes('customer-qa'))throw Error('PHP/database response failed: HTTP '+r.status());return 'Real HTTP request returned database row';});
  await test('HTML browser rendering',async()=>{const sitePage=await context.newPage();await sitePage.goto(`http://${a.domain}/qa-index-${runID}.html`);if(await sitePage.locator('h1').textContent()!=='HTML customer QA')throw Error('HTML content mismatch');await sitePage.screenshot({path:path.join(output,'html-site.png')});await sitePage.close();});
  await page.reload({waitUntil:'networkidle'});await test('Session persists across refresh',async()=>{if(!await page.locator('#appView').isVisible())throw Error('Session lost');});
  await test('Unauthenticated API denied',async()=>{const fresh=await browser.newContext();const r=await fresh.request.get(base+'/api/state');await fresh.close();if(r.status()!==401)throw Error('Expected 401, got '+r.status());});
  await test('Cross-origin mutation rejected',async()=>{const r=await context.request.post(base+'/api/sites',{headers:{Origin:'https://untrusted.example'},data:{domain:'invalid.example',appType:'regular',phpVersion:'8.3'}});if(r.status()!==403)throw Error('Expected origin rejection, got '+r.status());});
  await test('Independent runtime identities',async()=>{if(a.runtimeUid===b.runtimeUid||a.runtimeUid<1000||b.runtimeUid<1000)throw Error('Runtime identities are not isolated');return 'Distinct website runtime UIDs';});
  await test('Rename, copy, move and file permissions',async()=>{
   const root=`public_html/${prefix}-${runID}`;
   await api('/api/files/rename','POST',{siteId:sid,path:root+'/hello.txt',destination:root+'/renamed.txt'});
   await api('/api/files/directory','POST',{siteId:sid,path:root,name:'copy'});
   await api('/api/files/transfer','POST',{siteId:sid,paths:[root+'/renamed.txt'],destination:root+'/copy',operation:'copy'});
   await api('/api/files/directory','POST',{siteId:sid,path:root,name:'moved'});
   await api('/api/files/transfer','POST',{siteId:sid,paths:[root+'/copy/renamed.txt'],destination:root+'/moved',operation:'move'});
   await api('/api/files/permissions','PUT',{siteId:sid,path:root+'/moved/renamed.txt',mode:'0600'});
   const files=await api(`/api/files?siteId=${sid}&path=${root}/moved`);if(files.entries[0]?.mode!=='0600')throw Error('Permissions did not persist');
  });
  await test('Compress, extract and download integrity',async()=>{
   const root=`public_html/${prefix}-${runID}`;
   await api('/api/files/archive','POST',{siteId:sid,paths:[root+'/renamed.txt'],destination:root+'/sample.zip'});
   await api('/api/files/directory','POST',{siteId:sid,path:root,name:'extracted'});
   await api('/api/files/extract','POST',{siteId:sid,path:root+'/sample.zip',destination:root+'/extracted'});
   const download=await context.request.get(base+`/api/files/download?siteId=${sid}&path=${root}/renamed.txt`);
   if((await download.text())!=='LiteCP customer QA original\n')throw Error('Download content differs');
   const extracted=await api(`/api/files?siteId=${sid}&path=${root}/extracted`);if(!extracted.entries.length)throw Error('Archive extraction empty');
  });
  await test('Trash and restore only the newly created QA file',async()=>{
   const target=`public_html/${prefix}-${runID}/renamed.txt`;
   const entry=await api('/api/files','DELETE',{siteId:sid,path:target});
   await api('/api/files/trash','POST',{siteId:sid,id:entry.id});
   const result=await api(`/api/files/content?siteId=${sid}&path=${target}`);if(result.content!=='LiteCP customer QA original\n')throw Error('Restored content differs');
  });
  await test('Cron creation through browser and Run test',async()=>{
   await navigate('websites');await page.locator(`[data-site-cron-jobs="${sid}"]`).click();
   await page.locator('dialog.website-cron-dialog textarea[name="command"]').fill(`printf customer-cron > qa-cron-${runID}.txt`);
   await page.locator('dialog.website-cron-dialog [data-cron-save]').click();
   await page.waitForFunction(()=>document.querySelector('[data-cron-status]')?.textContent.includes('created'));
   const result=await api(`/api/website-cron?siteId=${sid}`),job=result.items.find(j=>j.command.includes(runID));if(!job)throw Error('Cron not stored');
   const execution=await api(`/api/website-cron/run?siteId=${sid}`,'POST',{id:job.id});if(!execution.success)throw Error('Cron process: '+execution.output);
   const text=await api(`/api/files/content?siteId=${sid}&path=public_html/qa-cron-${runID}.txt`);if(text.content!=='customer-cron')throw Error('Cron produced no expected output');
   await api(`/api/website-cron?siteId=${sid}`,'PUT',{id:job.id,name:job.name,schedule:job.schedule,command:job.command,enabled:false});await page.keyboard.press('Escape');
  });
  await test('Create mailbox through browser',async()=>{
   await navigate('email/create-mailbox');await page.locator('#mailboxDomain').selectOption(domain);
   await page.locator('#mailboxForm [name="localPart"]').fill('qa'+runID);await page.locator('#mailboxForm [name="password"]').fill(password);
   await response('/api/mail/mailboxes',()=>page.locator('#mailboxForm [type="submit"]').click());
   const state=await api('/api/state');if(!state.mailboxes.some(m=>m.address==='qa'+runID+'@'+domain))throw Error('Mailbox missing');
  });
  await test('Create FTPS account through browser',async()=>{
   await navigate('websites');await page.locator(`[data-site-ftp="${sid}"]`).click();const dialog=page.locator('dialog[open]').filter({hasText:'Website FTP accounts'});
   await dialog.locator('[name="username"]').fill('qa_'+runID);await dialog.locator('[name="password"]').fill(password);
   await response('/api/website-ftp',()=>dialog.locator('[type="submit"]').click());
   await page.waitForTimeout(300);await page.evaluate(()=>document.querySelectorAll('dialog[open]').forEach(d=>d.close()));
  });
  await test('Real FTPS upload/download and internal SMTP/IMAP delivery',async()=>{
   const python=`import sys,json,ssl,ftplib,io,smtplib,imaplib,time\nfrom email.message import EmailMessage\nx=json.load(sys.stdin)\nc=ssl.create_default_context();c.load_verify_locations(cafile='/etc/litecp/tls/litecp.crt');c.check_hostname=False\nf=ftplib.FTP_TLS(context=c);f.connect('127.0.0.1',21,timeout=15);f.login(x['ftp'],x['password']);f.prot_p();f.cwd('public_html');f.storbinary('STOR '+x['file'],io.BytesIO(b'ftps-integrity'));o=io.BytesIO();f.retrbinary('RETR '+x['file'],o.write);assert o.getvalue()==b'ftps-integrity';f.quit()\nm=EmailMessage();m['From']=x['email'];m['To']=x['email'];m['Subject']=x['token'];m.set_content('LiteCP internal delivery test')\ns=smtplib.SMTP('127.0.0.1',587,timeout=15);s.starttls(context=c);s.login(x['email'],x['password']);s.send_message(m);s.quit()\ni=imaplib.IMAP4_SSL('127.0.0.1',993,ssl_context=c);i.login(x['email'],x['password']);found=False\nfor n in range(15):\n i.select('INBOX');status,data=i.search(None,'SUBJECT',x['token']);found=bool(data[0]);\n if found:break\n time.sleep(1)\ni.logout();assert found,'SMTP accepted but IMAP message not delivered';print('FTPS integrity and SMTP-to-IMAP delivery passed')`;
   const command="python3 -c '"+python.replaceAll("'","'\"'\"'")+"'";
   const result=spawnSync('C:/Program Files/PuTTY/plink.exe',['-batch','-pw',process.env.LITECP_SSH_PASSWORD,'root@2.31.109.187',command],{input:JSON.stringify({ftp:'qa_'+runID,password,file:'qa-ftps-'+runID+'.txt',email:'qa'+runID+'@'+domain,token:'LiteCPQA'+runID}),encoding:'utf8',timeout:90000});
   if(result.status!==0)throw Error((result.stderr||'Protocol test failed').slice(-600));return result.stdout.trim();
  });
  await navigate('websites');await page.screenshot({path:path.join(output,'websites.png')});
  await test('Backup and restore QA website files and SQL',async()=>{
   await api(`/api/website-backups?siteId=${sid}`,'POST',{});const list=await api(`/api/website-backups?siteId=${sid}`);const item=list.items.sort((a,b)=>Date.parse(b.createdAt)-Date.parse(a.createdAt))[0];if(!item)throw Error('Backup not listed');
   const preview=await api(`/api/website-backups/restore?siteId=${sid}`,'POST',{action:'inspect',id:item.id});
   const target=await api(`/api/files?siteId=${b.id}&path=public_html`);for(const entry of target.entries)await api('/api/files','DELETE',{siteId:b.id,path:entry.path});
   await api(`/api/website-backups/restore?siteId=${sid}`,'POST',{action:'files',stageID:preview.id,targetSiteID:b.id,confirm:true});
   const restored=await api(`/api/files/content?siteId=${b.id}&path=public_html/${prefix}-${runID}/renamed.txt`);if(restored.content!=='LiteCP customer QA original\n')throw Error('Restored file mismatch');
   const db=(await api('/api/databases','POST',{name:'qa_restore_'+runID,username:'qa_restore_'+runID,password})).database;const sql=preview.databases.find(x=>x.includes(sites[a.domain].database.name));if(!sql)throw Error('SQL dump absent');
   await api(`/api/website-backups/restore?siteId=${sid}`,'POST',{action:'database',stageID:preview.id,sql,databaseID:db.id,password,confirm:true});return 'File content verified; SQL restored into a fresh separate target database';
  });
  await test('Issue SSL and verify trusted HTTPS',async()=>{await api('/api/sites/ssl','POST',{id:sites[domain].site.id,email:'itsmejeeban@gmail.com'});const r=await context.request.get('https://'+domain+'/');if(r.status()!==200)throw Error('HTTPS response '+r.status());});
 }finally{report.completedAt=new Date().toISOString();save();await context.close();await browser.close();}
})().catch(error=>{report.fatal=String(error.message).replaceAll(password,'[redacted]');save();console.error('QA stopped: '+report.fatal);process.exitCode=1;});
