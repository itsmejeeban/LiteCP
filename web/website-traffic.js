(() => {
  'use strict';
  const cp = window.LiteCP, esc = cp.escapeHTML;
  const page = document.createElement('section');
  page.className = 'page website-traffic-page';
  page.dataset.pageView = 'website-traffic';
  page.innerHTML = `<div class="page-heading"><div><h1>Traffic Analytics</h1><p>Origin traffic from Nginx access logs. No website tracking script required.</p></div><button class="button button-primary" data-refresh>Refresh</button></div>
    <div class="panel traffic-controls"><label class="field"><span>Website</span><select data-site aria-label="Website"></select></label><label class="field"><span>Date range (UTC)</span><select data-range><option value="today">Today</option><option value="7" selected>Last 7 Days</option><option value="30">Last 30 Days</option><option value="custom">Custom dates</option></select></label><label class="field" data-custom hidden><span>From</span><input type="date" data-from></label><label class="field" data-custom hidden><span>Through</span><input type="date" data-to></label><label class="field"><span>Chart interval</span><select data-group><option value="hourly">Hourly</option><option value="daily" selected>Daily</option><option value="weekly">Weekly</option><option value="monthly">Monthly</option></select></label></div>
    <p data-status role="status"></p><p data-warning class="traffic-warning" role="status" hidden></p>
    <div class="traffic-cards" data-cards></div><div class="traffic-charts" data-charts></div>
    <article class="panel"><div class="panel-head"><div><h2>Traffic reports</h2><p>Device, browser and bot classifications are estimates. Breakdown counts include requests; pageviews are listed separately.</p></div><label class="field"><span>Report</span><select data-dimension>${[['pages','Popular pages'],['sources','Referral sources'],['devices','Devices'],['browsers','Browsers'],['systems','Operating systems'],['bots','Recognized bots'],['statuses','Response codes'],['downloads','Downloads'],['slow','Slow requests (1s or longer)'],['protocols','HTTP protocols'],['countries','Countries']].map(([v,l])=>`<option value="${v}">${l}</option>`).join('')}</select></label></div><div class="traffic-table-wrap"><table class="traffic-table"><thead><tr><th>Detail</th><th>Requests</th><th>Estimated pageviews</th><th>Bandwidth</th><th>Errors</th><th>Average request duration</th><th>Maximum duration</th></tr></thead><tbody data-rows></tbody></table></div><nav class="traffic-pagination" aria-label="Report pagination"><label>Per page <select data-size><option>10</option><option>25</option><option>50</option><option>100</option></select></label><span data-pagination role="status"></span><div><button class="button button-primary" data-prev>Previous</button><button class="button button-primary" data-next>Next</button></div></nav><p class="traffic-note" data-geo></p></article>
    <article class="panel"><div class="panel-head"><div><h2>Live Access</h2><p>Recent completed HTTP requests, refreshed every three seconds. This is not an active visitor count.</p></div><button class="button button-primary" data-pause>Pause</button></div><div class="traffic-live-controls"><label class="field"><span>Search in</span><select data-live-field aria-label="Request search field"><option value="all">All fields</option><option value="page">Page</option><option value="method">HTTP method</option><option value="referral">Referral</option><option value="address">IP address (masked)</option><option value="protocol">HTTP protocol</option><option value="cache">Origin cache status</option><option value="client">Browser, device or bot</option><option value="country">Country</option></select></label><label class="field"><span>Search requests</span><input type="search" data-live-filter maxlength="200" placeholder="Search recent requests"></label><label class="field"><span>Response status</span><input type="number" min="100" max="599" data-live-status placeholder="All statuses"></label></div><p class="traffic-note" data-live-info role="status"></p><div class="traffic-table-wrap"><table class="traffic-table"><thead><tr><th>Time</th><th>Page</th><th>Method</th><th>Status</th><th>Bytes sent / received</th><th>Request duration</th><th>Referral</th><th>Client (estimated)</th><th>Masked address</th><th>Country</th><th>HTTP / origin cache</th></tr></thead><tbody data-live-rows></tbody></table></div><nav class="traffic-pagination" aria-label="Live access pagination"><span data-live-pagination></span><div><button class="button button-primary" data-live-prev>Previous</button><button class="button button-primary" data-live-next>Next</button></div></nav></article>
    <details class="panel traffic-definitions"><summary>How to read these reports</summary><p>Estimated pageviews count successful HTML/XHTML GET responses, excluding recognized bots, common static assets, downloads and /health or /healthz. Requests include assets, bots and errors. Unknown bots may still be counted. Average request duration measures Nginx server processing and transfer, not browser page-load speed.</p><p>Missing referrals appear as Direct / Unknown; they do not prove a direct visit. Unique people, sessions, engagement and exact device models cannot be reliably determined from these logs. Responses served entirely by a CDN or browser cache are absent. Trusted proxy configuration is required for correct client addresses behind a proxy; arbitrary forwarded headers are never trusted.</p><p>Charts and date filters use UTC. Weekly buckets start Monday. History starts when analytics logging is installed; unavailable periods are not reconstructed. Each day retains up to 80 distinct details per report before grouping additional values as Other. Existing logs and history are not deleted automatically; capacity or collection problems are shown above. Raw analytics logs contain client addresses and user agents; the saved reports use masked addresses.</p></details>`;
  document.querySelector('.main-content').append(page);
  const link = document.createElement('a');
  link.className = 'nav-item'; link.dataset.page = 'website-traffic'; link.href = '#website-traffic';
  link.innerHTML = '<img src="assets/sidebar/analytics.svg" width="20" height="20" alt=""><span>Traffic Analytics</span>';
  link.onclick = event => { event.preventDefault(); cp.navigate('website-traffic'); };
  document.querySelector('.nav').append(link);
  const $ = selector => page.querySelector(selector);
  let reportPage = 1, livePage = 1, liveEntries = [], paused = false, generation = 0;
  let timer = null, reportRequest = 0, liveRequest = 0, nextReport = 0;
  let reportBusy = false, liveBusy = false, filterTimer = null;
  const active = () => location.hash === '#website-traffic' && !document.hidden;
  const bytes = value => { let n=Number(value)||0,i=0;const units=['B','KiB','MiB','GiB','TiB'];while(n>=1024&&i<units.length-1){n/=1024;i++;}return `${n.toFixed(i?1:0)} ${units[i]}`; };
  const count = value => Number(value||0).toLocaleString();
  const duration = value => `${(Number(value||0)*1000).toFixed(1)} ms`;
  const nonzeroTime = value => value && !value.startsWith('0001') ? new Date(value).toLocaleString() : 'Waiting for first completed request';
  const query = () => new URLSearchParams({siteId:$('[data-site]').value,range:$('[data-range]').value,from:$('[data-from]').value,to:$('[data-to]').value,group:$('[data-group]').value,dimension:$('[data-dimension]').value,page:String(reportPage),size:$('[data-size]').value});
  function clearViews() {
    $('[data-cards]').replaceChildren(); $('[data-charts]').replaceChildren(); $('[data-rows]').replaceChildren();
    $('[data-warning]').hidden=true; liveEntries=[];livePage=1;renderLive();
    $('[data-pagination]').textContent='';$('[data-prev]').disabled=true;$('[data-next]').disabled=true;
    $('[data-live-info]').textContent=paused?'Live access paused. Resume to load requests for the selected website and filters.':'';$('[data-geo]').textContent='';
  }
  function renderSites() {
    const select=$('[data-site]'),old=select.value,sites=cp.state()?.sites||[];
    const signature=JSON.stringify(sites.map(s=>[s.id,s.domain]));
    if(select.dataset.signature===signature)return;
    select.dataset.signature=signature;
    select.innerHTML=sites.map(s=>`<option value="${esc(s.id)}">${esc(s.domain)}</option>`).join('');
    if(sites.some(s=>s.id===old))select.value=old;
    if(old!==select.value){generation++;clearViews();nextReport=0;}
    $('[data-refresh]').disabled=!sites.length;
    if(!sites.length)$('[data-status]').textContent='Add a website to view its traffic analytics.';
  }
  function chart(points,key,title,formatter) {
    if(!points.length)return `<article class="panel traffic-chart"><h3>${title}</h3><p>No requests recorded for this date range.</p></article>`;
    const value=p=>key==='errorRate'?(p.requests?p.errors/p.requests*100:0):Number(p[key]||0);
    const peak=Math.max(...points.map(value),0),max=Math.max(peak,1),width=700,height=190,left=55,plot=620;
    const first=Date.parse(points[0].time),last=Date.parse(points.at(-1).time);
    const coords=points.map(p=>[left+(points.length===1?plot/2:(Date.parse(p.time)-first)/Math.max(1,last-first)*plot),155-value(p)/max*125]);
    return `<article class="panel traffic-chart"><h3>${title}</h3><svg viewBox="0 0 ${width} ${height}" role="img" aria-label="${title} by selected interval"><line x1="${left}" y1="155" x2="675" y2="155" stroke="#555"/><text x="${left}" y="18">${esc(formatter(peak))} peak</text><polyline points="${coords.map(p=>p.join(',')).join(' ')}" stroke="#3186ff" stroke-width="2" fill="none"/>${coords.map(([x,y],i)=>`<circle cx="${x}" cy="${y}" r="3" fill="#3186ff"><title>${esc(points[i].time)}: ${esc(formatter(value(points[i])))}</title></circle>`).join('')}<text x="${left}" y="180">${esc(points[0].time)}</text><text x="675" y="180" text-anchor="end">${esc(points.at(-1).time)}</text></svg></article>`;
  }
  async function loadReport() {
    if(!active()||!$('[data-site]').value||reportBusy)return;
    reportBusy=true;const token=++reportRequest,gen=generation;
    $('[data-status]').textContent='Loading traffic reports…';
    try {
      const result=await cp.api('/api/sites/traffic?'+query());
      if(token!==reportRequest||gen!==generation||!active())return;
      const m=result.summary;
      $('[data-cards]').innerHTML=[['Requests',count(m.requests)],['Estimated pageviews',count(m.pageviews)],['Bandwidth sent',bytes(m.bytes)],['Request bytes received',bytes(m.requestBytes)],['Error rate',`${(m.requests?m.errors/m.requests*100:0).toFixed(1)}%`],['Recognized bot requests',count(m.bots)],['Average request duration',duration(m.requests?m.duration/m.requests:0)]].map(([label,value])=>`<article class="panel traffic-card"><span>${label}</span><strong>${value}</strong></article>`).join('');
      $('[data-charts]').innerHTML=[['requests','Requests',count],['pageviews','Estimated pageviews',count],['bytes','Bandwidth sent',bytes],['errorRate','Error rate',v=>Number(v).toFixed(1)+'%']].map(([key,title,fmt])=>chart(result.points,key,title,fmt)).join('');
      $('[data-rows]').innerHTML=result.rows.length?result.rows.map(row=>`<tr><td>${esc(row.name)}</td><td>${count(row.requests)}</td><td>${count(row.pageviews)}</td><td>${bytes(row.bytes)}</td><td>${count(row.errors)}</td><td>${duration(row.requests?row.duration/row.requests:0)}</td><td>${duration(row.maxDuration)}</td></tr>`).join(''):'<tr><td colspan="7">No matching requests recorded.</td></tr>';
      reportPage=result.page;
      $('[data-pagination]').textContent=`${result.total?((result.page-1)*result.size+1):0}–${Math.min(result.page*result.size,result.total)} of ${result.total} · Page ${result.page} of ${result.pages}`;
      $('[data-prev]').disabled=result.page<=1;$('[data-next]').disabled=result.page>=result.pages;
      $('[data-geo]').textContent=result.geoAvailable?'Countries use the administrator-supplied local CIDR database and are approximate.':'Country/location: Unavailable. No supported local GeoIP database is configured; no external IP lookup is performed.';
      $('[data-warning]').textContent=result.warning||'';$('[data-warning]').hidden=!result.warning;
      $('[data-status]').textContent=`Last collection: ${nonzeroTime(result.updated)} · UTC reports · ${count(result.malformed)} malformed records skipped. Collection start: ${nonzeroTime(result.started)}.`;
      nextReport=Date.now()+30000;
    } catch(error) { if(gen===generation&&active()){$('[data-status]').textContent='Unable to load analytics. '+error.message;nextReport=Date.now()+30000;} }
    finally {reportBusy=false;if(gen!==generation&&active())loadReport();}
  }
  function renderLive() {
    const pages=Math.max(1,Math.ceil(liveEntries.length/20));livePage=Math.max(1,Math.min(livePage,pages));
    $('[data-live-rows]').innerHTML=liveEntries.length?liveEntries.slice((livePage-1)*20,livePage*20).map(e=>`<tr><td>${esc(new Date(e.time).toLocaleString())}</td><td>${esc(e.path)}</td><td>${esc(e.method)}</td><td><span class="traffic-code ${e.status>=400?'traffic-error':'traffic-ok'}">${e.status}</span></td><td>${bytes(e.bytes)} / ${bytes(e.requestBytes)}</td><td>${duration(e.duration)}</td><td>${esc(e.referrer)}</td><td>${esc([e.device,e.browser,e.os,e.bot].filter(Boolean).join(' · '))}</td><td>${esc(e.address)}</td><td>${esc(e.country)}</td><td>${esc(e.protocol||'Unknown')} / ${esc(e.cache&&e.cache!=='-'?e.cache:'Unavailable')}</td></tr>`).join(''):'<tr><td colspan="11">No recent completed requests match these filters.</td></tr>';
    $('[data-live-pagination]').textContent=`${liveEntries.length} recent requests · Page ${livePage} of ${pages}`;
    $('[data-live-prev]').disabled=livePage<=1;$('[data-live-next]').disabled=livePage>=pages;
  }
  async function loadLive() {
    if(!active()||paused||!$('[data-site]').value||liveBusy)return;
    liveBusy=true;const token=++liveRequest,gen=generation;
    const params=query();params.set('view','live');params.set('group','daily');params.set('field',$('[data-live-field]').value);params.set('filter',$('[data-live-filter]').value);params.set('status',$('[data-live-status]').value);
    try {
      const result=await cp.api('/api/sites/traffic?'+params);
      if(token!==liveRequest||gen!==generation||!active()||paused)return;
      liveEntries=result.entries;renderLive();
      $('[data-live-info]').textContent=`Up to ${result.limit} recent requests · Last collection: ${nonzeroTime(result.updated)}${result.warning?' · '+result.warning:''}`;
    } catch(error) {if(gen===generation&&active())$('[data-live-info]').textContent='Live access unavailable. '+error.message;}
    finally {liveBusy=false;if(gen!==generation&&active()&&!paused)loadLive();}
  }
  function reset() {generation++;reportPage=1;livePage=1;nextReport=0;clearViews();loadReport();loadLive();}
  function activate() {
    clearInterval(timer);timer=null;
    if(!active()){generation++;return;}
    renderSites();loadReport();loadLive();
    timer=setInterval(()=>{if(!active())return;loadLive();if(Date.now()>=nextReport)loadReport();},3000);
  }
  $('[data-site]').onchange=reset;
  $('[data-range]').onchange=()=>{page.querySelectorAll('[data-custom]').forEach(el=>el.hidden=$('[data-range]').value!=='custom');reset();};
  for(const key of ['from','to','group','dimension','size'])$(`[data-${key}]`).onchange=reset;
  $('[data-refresh]').onclick=()=>{nextReport=0;loadReport();loadLive();};
  $('[data-prev]').onclick=()=>{generation++;reportPage--;loadReport();};$('[data-next]').onclick=()=>{generation++;reportPage++;loadReport();};
  $('[data-live-prev]').onclick=()=>{livePage--;renderLive();};$('[data-live-next]').onclick=()=>{livePage++;renderLive();};
  $('[data-pause]').onclick=()=>{paused=!paused;$('[data-pause]').textContent=paused?'Resume':'Pause';$('[data-live-info]').textContent=paused?'Live access paused.': 'Resuming live access…';if(!paused)loadLive();};
  for(const key of ['live-filter','live-status','live-field'])$(`[data-${key}]`).oninput=()=>{generation++;livePage=1;liveEntries=[];renderLive();$('[data-live-info]').textContent=paused?'Live access paused. Resume to apply the new filters.':'Loading matching requests…';clearTimeout(filterTimer);filterTimer=setTimeout(loadLive,300);};
  const today=new Date().toISOString().slice(0,10);$('[data-from]').value=today;$('[data-to]').value=today;$('[data-from]').max=today;$('[data-to]').max=today;
  const navigate=window.LiteCPFeatures.navigate;window.LiteCPFeatures.navigate=id=>{navigate?.(id);activate();};
  const render=window.LiteCPFeatures.render;window.LiteCPFeatures.render=state=>{render(state);renderSites();if(active()&&!timer)activate();};
  window.addEventListener('hashchange',activate);document.addEventListener('visibilitychange',activate);
  renderSites();renderLive();
  if(location.hash==='#website-traffic'&&cp.state())cp.navigate('website-traffic',false);
})();
