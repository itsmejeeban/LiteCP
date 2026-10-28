/* Read-only live UI audit. Credentials stay in the process environment. */
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require('playwright');

const base = process.env.LITECP_AUDIT_URL || 'http://127.0.0.1:19190';
const output = path.resolve(process.env.LITECP_AUDIT_OUTPUT || 'docs/ui-audit-2026-10-28');
const widths = (process.env.LITECP_AUDIT_WIDTHS || '320,375,425,768,1024,1280,1440,1920').split(',').map(Number);
fs.mkdirSync(output, { recursive: true });

(async () => {
  const browser = await chromium.launch({ executablePath: process.env.LITECP_CHROME || 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const page = await context.newPage();
  const report = { startedAt: new Date().toISOString(), routes: [], checks: [], dialogs: [], errors: [], failedRequests: [], blockedMutations: [] };
  const requests = new Set();
  page.on('request', request => requests.add(request));
  page.on('requestfinished', request => requests.delete(request));
  page.on('requestfailed', request => requests.delete(request));
  async function settle() {
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    await new Promise((resolve, reject) => {
      const started = Date.now();
      const check = () => requests.size ? (Date.now() - started > 45000 ? reject(Error('Requests did not finish')) : setTimeout(check, 50)) : resolve();
      check();
    });
    await page.evaluate(() => Promise.all(document.getAnimations().filter(animation => Number.isFinite(animation.effect.getComputedTiming().endTime)).map(animation => animation.finished.catch(() => {}))));
  }
  page.on('pageerror', error => report.errors.push(error.message));
  page.on('response', response => { if (response.status() >= 400 && !response.url().endsWith('/api/state')) report.failedRequests.push({ url: new URL(response.url()).pathname, status: response.status() }); });
  await context.route('**/api/**', route => {
    const request = route.request(), url = new URL(request.url());
    if (!['GET', 'HEAD'].includes(request.method()) && !['/api/login', '/api/logout'].includes(url.pathname)) {
      report.blockedMutations.push({ path: url.pathname, method: request.method() });
      return route.abort('blockedbyclient');
    }
    return route.continue();
  });
    for (const file of (process.env.LITECP_AUDIT_LOCAL_ASSETS || '').split(',').filter(Boolean)) {
    await context.route('**/' + file + '*', route => route.fulfill({ path: path.resolve('web', file) }));
  }
  try {
    await page.goto(base, { waitUntil: 'networkidle' });
    if (await page.locator('#loginView').isVisible()) {
      if (!process.env.LITECP_AUDIT_PASSWORD) throw Error('LITECP_AUDIT_PASSWORD is required');
      await page.locator('#loginForm [name="username"]').fill('admin');
      await page.locator('#loginPassword').fill(process.env.LITECP_AUDIT_PASSWORD);
      await page.locator('#loginForm [type="submit"]').click();
    }
    await page.locator('#appView:not(.hidden)').waitFor();
    await page.waitForLoadState('networkidle');
    report.routes = await page.locator('#sidebar a[href^="#"], .documentation-tab').evaluateAll(nodes => [...new Set(nodes.map(node => node.hash.slice(1)).filter(Boolean))]);
    if (!report.routes.includes('documentation')) report.routes.push('documentation');
    report.routes.push('files');
    if (process.env.LITECP_AUDIT_ROUTES) report.routes = report.routes.filter(route => process.env.LITECP_AUDIT_ROUTES.split(',').includes(route));
    console.log('Discovered ' + report.routes.length + ' routes');
    for (const width of widths) {
      await page.setViewportSize({ width, height: 1000 });
      for (const route of report.routes) {
        await page.evaluate(route => window.LiteCP.navigate(route), route);
        if (['website-backups', 'website-cron', 'website-ftp'].includes(route)) {
          const select = page.locator('[data-page-view].active .website-tool-picker select');
          const options = await select.locator('option').evaluateAll(nodes => nodes.map(node => node.value).filter(Boolean));
          if (options.length) { await select.selectOption(options[0]); await page.locator('[data-page-view].active .website-tool-picker button').click(); }
        }
        await settle();
        const check = await page.evaluate(() => {
          const active = [...document.querySelectorAll('[data-page-view].active')];
          const scope = active[0];
          const visible = node => !!(node.getClientRects().length && getComputedStyle(node).visibility !== 'hidden');
          const label = node => (node.getAttribute('aria-label') || node.innerText || node.getAttribute('title') || node.name || node.id || node.tagName).trim().slice(0, 100);
          const clips = [];
          const controls = scope ? [...scope.querySelectorAll('button,a,input,select,textarea')].filter(visible) : [];
          for (const control of controls.slice(0, 100)) {
            const rect = control.getBoundingClientRect();
            if (rect.width < 1 || rect.height < 1) continue;
            let reachableX = false, reachableY = false;
            for (let ancestor = control.parentElement; ancestor && ancestor !== document.body; ancestor = ancestor.parentElement) {
              const style = getComputedStyle(ancestor), box = ancestor.getBoundingClientRect();
              if (['auto', 'scroll'].includes(style.overflowX) && ancestor.scrollWidth > ancestor.clientWidth && rect.width <= ancestor.clientWidth) reachableX = true;
              if (['auto', 'scroll'].includes(style.overflowY) && ancestor.scrollHeight > ancestor.clientHeight && rect.height <= ancestor.clientHeight) reachableY = true;
              const badX = !reachableX && ['hidden', 'clip'].includes(style.overflowX) && (rect.left < box.left - 2 || rect.right > box.right + 2);
              const badY = !reachableY && ['hidden', 'clip'].includes(style.overflowY) && (rect.top < box.top - 2 || rect.bottom > box.bottom + 2);
              if (badX || badY) { clips.push({ control: label(control), ancestor: ancestor.id || ancestor.className, axis: badX ? 'x' : 'y' }); break; }
            }
          }
          return { activePages: active.map(node => node.dataset.pageView), pageOverflow: document.documentElement.scrollWidth - innerWidth, clips, controls: controls.length, unnamedButtons: controls.filter(node => node.tagName === 'BUTTON' && !label(node).replace(/^BUTTON$/, '')).length, errors: scope ? [...scope.querySelectorAll('[role="alert"],.form-error')].filter(visible).map(node => node.textContent.trim()).filter(Boolean) : [] };
        });
        report.checks.push({ width, route, ...check });
        if (check.pageOverflow > 2 || check.clips.length || check.activePages.length !== 1) console.log('Layout finding: ' + JSON.stringify({ width, route, overflow: check.pageOverflow, clips: check.clips.slice(0, 3), active: check.activePages }));
        if ([320, 768, 1440, 1920].includes(width) && ['dashboard', 'websites', 'email/mailboxes', 'settings/password', 'security-dashboard', 'extensions', 'analytics', 'migrations/upload'].includes(route)) await page.screenshot({ path: path.join(output, `${width}-${route.replaceAll('/', '-')}.png`) });
      }
      console.log('Completed width ' + width);
      fs.writeFileSync(path.join(output, 'results.json'), JSON.stringify(report, null, 2));
    }
    await page.evaluate(() => document.querySelectorAll('dialog[open]').forEach(dialog => dialog.close()));
    await page.setViewportSize({ width: 1440, height: 1000 });
    for (const [name, route, selector] of [
      ['create-website', 'websites', '[data-open-site-modal]'],
      ['create-database', 'databases', '.page-heading .button-primary'],
      ['website-cron', 'website-cron', '.website-tool-picker button'],
      ['website-ftp', 'website-ftp', '.website-tool-picker button'],
      ['website-backups', 'website-backups', '.website-tool-picker button']
    ]) {
      await page.evaluate(route => window.LiteCP.navigate(route), route);
      await settle();
      const trigger = page.locator(`[data-page-view].active ${selector}`).first();
      if (!await trigger.count()) { report.dialogs.push({ name, error: 'Trigger not found' }); continue; }
      await trigger.click();
      await settle();
      await page.screenshot({ path: path.join(output, `1440-${name}.png`) });
      report.dialogs.push({ name, open: await page.locator('dialog[open],#siteModal:not(.hidden)').count() });
      await page.keyboard.press('Escape');
      await page.evaluate(() => document.querySelectorAll('dialog[open]').forEach(dialog => dialog.close()));
    }
    report.interactions=[];
    await page.evaluate(() => window.LiteCP.navigate('files'));
    await settle();
    for (const width of widths) {
      await page.setViewportSize({width,height:1000});
      for (const action of ['new-file','new-folder','upload','settings']) {
        await page.locator(`.file-toolbar [data-fm-action="${action}"],.cpfm-header [data-fm-action="${action}"]`).first().click();
        await settle();
        const dialog=page.locator('#fmActionDialog');
        const geometry=await dialog.evaluate(node=>{const box=node.getBoundingClientRect();return {width:box.width,height:box.height,overflow:node.scrollWidth-node.clientWidth,left:box.left,right:box.right,viewport:innerWidth,background:getComputedStyle(node).backgroundColor};});
        report.interactions.push({name:'file-'+action,width,open:await dialog.isVisible(),...geometry});
        if ([320,1024,1440].includes(width)) await page.screenshot({path:path.join(output,`${width}-file-${action}.png`)});
        await page.keyboard.press('Escape');
      }
      await page.screenshot({path:path.join(output,`${width}-file-workspace.png`)});
    }
    await page.setViewportSize({width:1440,height:1000});
    await page.evaluate(()=>window.LiteCP.goToFilePath('public_html'));
    await settle();
    const license=page.locator('[data-fm-row]').filter({hasText:'license.txt'}).first();
    if(await license.count()){
      await license.click({button:'right'});
      report.interactions.push({name:'file-context-edit',visible:await page.locator('#fmContextMenu [data-fm-action="edit"]').isVisible()});
      await page.keyboard.press('Escape');
      for(const action of ['copy','move','rename','chmod','archive']){
        await license.click();await page.locator(`.file-toolbar [data-fm-action="${action}"]`).click();await settle();
        report.interactions.push({name:'file-'+action,open:await page.locator('#fmActionDialog').isVisible()});
        await page.screenshot({path:path.join(output,`1440-file-${action}.png`)});await page.keyboard.press('Escape');
      }
    }
    await page.setViewportSize({width:375,height:1000});
    await page.evaluate(()=>window.LiteCP.navigate('websites'));
    await page.locator('.mobile-navigation-toggle').click();
    report.interactions.push({name:'mobile-navigation-open',open:await page.locator('#sidebar').evaluate(node=>node.classList.contains('open')&&!node.inert)});
    await page.locator('#sidebar a[data-page="databases"]').click();await settle();
    report.interactions.push({name:'mobile-navigation-route',active:await page.locator('[data-page-view="databases"]').evaluate(node=>node.classList.contains('active'))});
    report.completedAt = new Date().toISOString();
    fs.writeFileSync(path.join(output, 'results.json'), JSON.stringify(report, null, 2));
    console.log('Audit saved: ' + output);
    console.log(JSON.stringify({ checks: report.checks.length, errors: [...new Set(report.errors)], failedRequests: report.failedRequests, blockedMutations: report.blockedMutations, dialogs: report.dialogs }));
  } finally { await context.close(); await browser.close(); }
})().catch(error => { console.error(error.message); process.exitCode = 1; });
