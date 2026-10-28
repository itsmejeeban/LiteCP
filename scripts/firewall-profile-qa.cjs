const {chromium}=require('playwright');
(async()=>{
 const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true});
 try{
  const context=await browser.newContext({ignoreHTTPSErrors:true,viewport:{width:1440,height:1000}}),page=await context.newPage();
  await page.goto('https://2.31.109.187:4321/',{waitUntil:'networkidle'});
  await page.locator('#loginForm [name=username]').fill('admin');await page.locator('#loginForm [name=password]').fill(process.env.LITECP_QA_PASSWORD);
  await page.locator('#loginForm [type=submit]').click();await page.locator('#appView').waitFor();
  await page.evaluate(()=>window.LiteCP.navigate('firewall'));
  await page.getByRole('button',{name:'Apply LiteCP Firewall',exact:true}).click();
  const response=page.waitForResponse(r=>r.url().endsWith('/api/firewall/apply-litecp'));
  await page.locator('[data-dialog-submit]').click();
  const result=await response;if(!result.ok())throw Error(await result.text());
  console.log('PASS firewall button applied:',await result.json());
 }finally{await browser.close()}
})().catch(e=>{console.error(e.message);process.exitCode=1});
