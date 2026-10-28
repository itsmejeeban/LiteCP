const {chromium}=require('playwright');
(async()=>{
 const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true});
 try{
  const context=await browser.newContext({ignoreHTTPSErrors:true});const page=await context.newPage();
  await page.goto('https://2.31.109.187:4321/',{waitUntil:'networkidle'});
  await page.locator('#loginForm [name=username]').fill('admin');await page.locator('#loginForm [name=password]').fill(process.env.LITECP_QA_PASSWORD);
  await page.locator('#loginForm [type=submit]').click();await page.locator('#appView').waitFor();
  await page.evaluate(()=>window.LiteCP.navigate('email/mailboxes'));await page.locator('[data-webmail-login]').first().waitFor();
  for(let i=0;i<2;i++){
   const button=page.locator('[data-webmail-login]').nth(i),address=(await button.getAttribute('aria-label')).replace('Log in to ','');
   const response=page.waitForResponse(r=>r.url().endsWith('/api/mail/webmail-login')&&r.request().method()==='POST');
   const popup=context.waitForEvent('page');await button.click();const mailbox=await popup;const launch=await (await response).json();
   await mailbox.waitForURL(/_mbox=INBOX/,{timeout:30000});await mailbox.waitForLoadState('networkidle');
   let body=await mailbox.locator('body').innerText();if(!body.includes('Inbox')||!body.includes(address))throw Error('Selected mailbox inbox did not open');
   await mailbox.reload({waitUntil:'networkidle'});body=await mailbox.locator('body').innerText();if(!body.includes('Inbox')||!body.includes(address))throw Error('Mailbox session did not persist');
   const replay=await context.request.post(launch.url,{form:{litecp_ticket:launch.ticket}});if(replay.status()!==403)throw Error('Ticket replay accepted');
   console.log('PASS: selected mailbox opens without a password, refresh persists, replay rejected');await mailbox.close();
  }
  const aligned=await page.locator('[data-mail-action=mailbox-delete]').first().evaluate(e=>({align:getComputedStyle(e).alignItems,justify:getComputedStyle(e).justifyContent}));
  if(aligned.align!=='center'||aligned.justify!=='center')throw Error('Delete icon not centered');console.log('PASS: delete icon centered');
 }finally{await browser.close();}
})().catch(e=>{console.error(e.message);process.exitCode=1});
