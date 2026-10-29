/* Render the repository's demo interface for documentation; no live server access. */
const http = require('node:http');
const fs = require('node:fs');
const path = require('node:path');
const {chromium} = require('playwright');
const root = path.resolve(__dirname, '../web');
const destination = path.resolve(__dirname, '../docs/dashboard-v2-demo-ready.png');
const mime = {'.html':'text/html','.js':'application/javascript','.css':'text/css','.svg':'image/svg+xml','.png':'image/png'};
(async () => {
  if (fs.existsSync(destination)) throw new Error('Screenshot already exists; choose a new output name to preserve it.');
  const server = http.createServer((request, response) => {
    const relative = decodeURIComponent(new URL(request.url, 'http://localhost').pathname).replace(/^\/+/, '') || 'index.html';
    const target = path.resolve(root, relative);
    if (!target.startsWith(root + path.sep) || !fs.existsSync(target) || !fs.statSync(target).isFile()) { response.writeHead(404); response.end(); return; }
    response.setHeader('Content-Type',mime[path.extname(target)] || 'application/octet-stream');
    fs.createReadStream(target).pipe(response);
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  let browser;
  try {
    browser = await chromium.launch({headless:true,executablePath:process.env.LITECP_SCREENSHOT_CHROME || 'C:/Program Files/Google/Chrome/Application/chrome.exe'});
    const page = await browser.newPage({viewport:{width:1600,height:1200},deviceScaleFactor:1});
    const errors=[];page.on('pageerror',error=>errors.push(error.message));
    await page.goto(`http://127.0.0.1:${server.address().port}/?demo=1#dashboard`);
    await page.waitForFunction(()=>window.LiteCP?.state()?.version === '2.0.0');
    await page.locator('[data-page-view="dashboard"].active').waitFor();
    await page.evaluate(()=>document.fonts.ready);
    await page.waitForTimeout(700);
    if(await page.getByText('Unable to check installed software.',{exact:false}).count())throw new Error('Environment inventory did not render correctly.');
    await page.screenshot({path:destination});
    console.log('Screenshot: '+destination);
    console.log('Browser errors: '+JSON.stringify(errors));
    if(errors.length)process.exitCode=1;
  } finally { if(browser)await browser.close();await new Promise(resolve=>server.close(resolve)); }
})().catch(error=>{console.error(error.message);process.exitCode=1;});
