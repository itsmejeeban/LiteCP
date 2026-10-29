(() => {
  "use strict";
  const cp=window.LiteCP,esc=cp.escapeHTML,page=document.createElement('section');page.className='page';page.dataset.pageView='analytics';
  page.innerHTML='<div class="page-heading"><div><h1>Usage graphs</h1><p>CPU, memory and disk usage over time.</p></div><button class="button button-secondary" data-usage-refresh>Refresh</button></div><div class="panel analytics-controls"><label class="field"><span>View</span><select id="usageView"><option value="live">Live · last hour</option><option value="hourly">Hourly · last 24 hours</option><option value="daily">Daily · last 30 days</option><option value="weekly">Weekly · last 12 weeks</option><option value="monthly">Monthly · last 12 months</option></select></label><p data-usage-status role="status"></p></div><div class="usage-graphs"></div><p class="feature-note">Live updates every 15 seconds. History saves every minute and survives panel updates and server reboots. Historical values are averages in UTC; times are displayed in your browser time zone. History starts when monitoring is installed.</p>';
  const diskPage=document.createElement('section');diskPage.className='page';diskPage.dataset.pageView='disk-usage';
  diskPage.innerHTML='<div class="page-heading"><div><h1>Disk Usages</h1><p>Explore server disk usage by path. Open a directory to see its contents.</p></div><button class="button button-secondary" data-disk-refresh>Refresh</button></div><div class="disk-usage-layout"><article class="panel disk-chart-panel"><div class="panel-head"><div><h2>Directory usage</h2><p data-disk-location></p></div><strong data-disk-total>—</strong></div><div class="disk-pie" role="img" aria-label="Directory disk usage chart"><span>Loading…</span></div></article><article class="panel disk-directory-panel"><div class="panel-head"><h2>Directories</h2><button type="button" class="button button-secondary hidden" data-disk-up>Up one level</button></div><div class="disk-directory-list" data-disk-list></div><p class="feature-note" data-disk-status role="status"></p></article></div>';
  document.querySelector('.main-content').append(page,diskPage);
  const storageControls=document.createElement('div');storageControls.className='panel analytics-controls';storageControls.innerHTML='<label class="field"><span>Directory view</span><select data-storage-scope><option value="server">All server directories</option><option value="litecp">LiteCP storage directories</option></select></label><p>See website, software, backup and migration storage.</p>';diskPage.querySelector('.page-heading').after(storageControls);
  const storagePanel=document.createElement('article');storagePanel.className='panel litecp-storage-panel';storagePanel.hidden=true;storagePanel.innerHTML='<div class="panel-head"><h2>LiteCP storage directories</h2></div><p class="feature-note" data-storage-note></p><div class="litecp-storage-list"></div>';diskPage.append(storagePanel);
  const storageScope=storageControls.querySelector('select');
  storageScope.onchange=()=>loadDisk('');
  const group=document.createElement('div');group.className='sidebar-feature-group';group.innerHTML='<button type="button" class="nav-item sidebar-group-toggle" aria-controls="analyticsSubmenu" aria-expanded="false"><img src="assets/sidebar/activities.svg" alt="" width="22" height="22"><span>Analytics</span><svg class="sidebar-group-chevron" viewBox="0 0 24 24" aria-hidden="true"><path d="m7 10 5 5 5-5"/></svg></button><nav id="analyticsSubmenu" class="settings-submenu hidden" aria-label="Analytics"><a class="nav-item" data-page="analytics" href="#analytics"><img src="assets/sidebar/cpu.svg" alt="" width="20" height="20"><span>Graph</span></a><a class="nav-item" data-page="disk-usage" href="#disk-usage"><img src="assets/sidebar/backups.svg" alt="" width="20" height="20"><span>Disk Usages</span></a></nav>';
  document.querySelector('.nav-item[data-page="migrations"]').after(group);
  const toggle=group.querySelector('button'),submenu=group.querySelector('nav');toggle.onclick=()=>{const open=toggle.getAttribute('aria-expanded')!=='true';toggle.setAttribute('aria-expanded',String(open));submenu.classList.toggle('hidden',!open);};group.querySelectorAll('a').forEach(link=>link.onclick=event=>{event.preventDefault();cp.navigate(link.dataset.page);});
  const view=page.querySelector('select'),status=page.querySelector('[data-usage-status]');let timer=null,request=0;
  function graph(points,key,title,color,latest){
    const width=640,height=210,left=42,top=18,plotW=580,plotH=155;
    const first=points[0]?.time,last=points.at(-1)?.time,start=Date.parse(first),end=Date.parse(last);
    const coords=points.map((point,index)=>[points.length===1?left+plotW/2:left+(Date.parse(point.time)-start)/Math.max(1,end-start)*plotW,top+(100-Math.max(0,Math.min(100,point[key])))/100*plotH]);
    const grid=[0,25,50,75,100].map(value=>`<line x1="${left}" y1="${top+(100-value)/100*plotH}" x2="${left+plotW}" y2="${top+(100-value)/100*plotH}" stroke="#333"/><text x="${left-7}" y="${top+(100-value)/100*plotH+4}" text-anchor="end">${value}%</text>`).join('');
    const date=value=>new Date(value).toLocaleString(undefined,{month:'short',day:'numeric',hour:'2-digit',minute:'2-digit'});
    return `<article class="panel usage-chart"><div class="panel-head"><h2>${title}</h2><strong style="color:${color}">${latest?Number(latest[key]).toFixed(1)+'%':'—'}</strong></div>${points.length?`<svg viewBox="0 0 ${width} ${height}" role="img" aria-label="${title} graph, ${points.length} recorded points"><g fill="#aaa" font-size="11">${grid}<text x="${left}" y="202">${esc(date(first))}</text><text x="${left+plotW}" y="202" text-anchor="end">${esc(date(last))}</text></g><polyline points="${coords.map(point=>point.join(',')).join(' ')}" fill="none" stroke="${color}" stroke-width="2.5" stroke-linejoin="round"/>${coords.map(([x,y],index)=>`<circle cx="${x}" cy="${y}" r="3" fill="${color}"><title>${esc(date(points[index].time))}: ${Number(points[index][key]).toFixed(1)}%</title></circle>`).join('')}</svg>`:'<p class="usage-empty">No history yet. The first sample will appear shortly.</p>'}</article>`;
  }
  async function load(){const token=++request,selected=view.value;try{const result=await cp.api('/api/analytics/usage?view='+selected);if(token!==request||selected!==view.value||location.hash!=='#analytics')return;const latest=result.latest?.time&&!result.latest.time.startsWith('0001')?result.latest:null;page.querySelector('.usage-graphs').innerHTML=[['cpu','CPU usage','#60a5fa'],['memory','Memory usage','#c084fc'],['disk','Disk storage usage','#34d399']].map(([key,title,color])=>graph(result.points,key,title,color,latest)).join('');status.textContent=result.warning||`${result.points.length} recorded points · ${selected==='live'?'Live readings':'Average usage'}${latest?' · Updated '+new Date(latest.time).toLocaleTimeString():''}`;}catch(error){if(token===request)status.textContent='Unable to load graphs. '+error.message;}}
  const diskColors=['#4f8fdc','#48a982','#d6a84b','#a67bd4','#d87575','#5bb4c7','#ca83a8','#8ca85a','#df8b52','#7189c7'];let diskPath='';
  const formatBytes=value=>{const units=['B','KiB','MiB','GiB','TiB'];let size=Number(value)||0,index=0;while(size>=1024&&index<units.length-1){size/=1024;index++;}return `${size>=10||index===0?size.toFixed(0):size.toFixed(1)} ${units[index]}`;};
  let diskRequest=0;
  function renderDiskChart(result,entries){
    const fs=result.filesystem,segments=entries.filter(entry=>entry.bytes>0).map((entry,index)=>({...entry,label:entry.isDir?entry.name:(result.root+(result.path||''))+' — files and directory metadata',color:diskColors[index%diskColors.length]}));
    if(fs){
      const other=Math.max(0,fs.usedBytes-result.totalBytes),reserved=Math.max(0,fs.capacityBytes-fs.usedBytes-fs.freeBytes);
      if(other)segments.push({label:'Other used space on this filesystem',bytes:other,color:'#6d7787'});
      if(reserved)segments.push({label:'Filesystem reserved space',bytes:reserved,color:'#a58a60'});
      if(fs.freeBytes)segments.push({label:'Free space',bytes:fs.freeBytes,color:'#314454'});
    }
    const total=segments.reduce((sum,item)=>sum+item.bytes,0),circumference=2*Math.PI*116;
    let offset=0;
    const arcs=segments.map((item,index)=>{
      const length=item.bytes/Math.max(1,total)*circumference,start=offset;offset+=length;
      return '<circle class="disk-donut-segment" cx="160" cy="160" r="116" fill="none" stroke="'+item.color+'" stroke-width="34" stroke-dasharray="'+length+' '+(circumference-length)+'" stroke-dashoffset="'+(-start)+'" transform="rotate(-90 160 160)" tabindex="0" data-segment="'+index+'" aria-label="'+esc(item.label+': '+formatBytes(item.bytes))+'"><title>'+esc(item.label+': '+formatBytes(item.bytes)+' ('+(item.bytes/Math.max(1,total)*100).toFixed(1)+'%)')+'</title></circle>';
    }).join('');
    const pie=diskPage.querySelector('.disk-pie');pie.removeAttribute('role');pie.style.background='none';
    pie.innerHTML='<svg viewBox="0 0 320 320" aria-label="Disk space by directory, including free space"><circle cx="160" cy="160" r="116" fill="none" stroke="#25303b" stroke-width="34"/>'+arcs+'</svg><div class="disk-donut-center"><strong>'+formatBytes(fs?fs.freeBytes:result.totalBytes)+'</strong><small>'+(fs?'Free space':'Directory usage')+'</small></div><div class="disk-chart-tooltip" role="status" hidden></div>';
    const tooltip=pie.querySelector('.disk-chart-tooltip');
    pie.querySelectorAll('[data-segment]').forEach(segment=>{
      const show=()=>{const item=segments[Number(segment.dataset.segment)];tooltip.textContent=item.label+' · '+formatBytes(item.bytes)+' · '+(item.bytes/Math.max(1,total)*100).toFixed(1)+'%';tooltip.hidden=false;};
      segment.onpointerenter=show;segment.onfocus=show;segment.onpointerleave=()=>tooltip.hidden=true;segment.onblur=()=>tooltip.hidden=true;
    });
    let summary=diskPage.querySelector('.disk-space-summary');if(!summary){summary=document.createElement('div');summary.className='disk-space-summary';pie.after(summary);}
    summary.innerHTML=fs?'<span><i style="background:#4f8fdc"></i>Used <b>'+formatBytes(fs.usedBytes)+'</b></span><span><i style="background:#314454"></i>Free <b>'+formatBytes(fs.freeBytes)+'</b></span><span>Capacity <b>'+formatBytes(fs.capacityBytes)+'</b></span>':'<span>Filesystem free space is currently unavailable.</span>';
  }
  async function loadDisk(path=diskPath){
    const token=++diskRequest,status=diskPage.querySelector('[data-disk-status]');status.textContent='Calculating directory sizes…';
    const panelMode=storageScope.value==='litecp';diskPage.querySelector('.disk-usage-layout').hidden=panelMode;storagePanel.hidden=!panelMode;
    if(panelMode){
      const list=storagePanel.querySelector('.litecp-storage-list');list.textContent='Calculating LiteCP storage…';
      try{const result=await cp.api('/api/analytics/disk-usage?scope=litecp');if(token!==diskRequest)return;
        storagePanel.querySelector('[data-storage-note]').textContent=result.note;
        list.innerHTML='<table class="data-table"><thead><tr><th>Purpose</th><th>Directory / file path</th><th>Used space</th><th></th></tr></thead><tbody>'+(result.items||[]).map(item=>'<tr><td><strong>'+esc(item.name)+'</strong><small class="storage-category">'+esc(item.category)+'</small></td><td><code>'+esc(item.path)+'</code></td><td>'+(item.status==='not-created'?'Not created':item.status==='unavailable'?'Unavailable':formatBytes(item.bytes)+(item.status==='partial'?' · partial':''))+'</td><td>'+(item.isDir&&['measured','partial'].includes(item.status)?'<button class="button button-secondary" data-storage-open="'+esc(item.path)+'">Explore</button>':'')+'</td></tr>').join('')+'</tbody></table>';
        list.querySelectorAll('[data-storage-open]').forEach(button=>button.onclick=()=>{storageScope.value='server';loadDisk(button.dataset.storageOpen.replace(/^\//,''));});
        if(result.partial)storagePanel.querySelector('[data-storage-note]').textContent+=' Scan time limit reached; some paths could not be measured.';
      }catch(error){if(token===diskRequest)list.textContent='Unable to measure LiteCP storage. '+error.message;}
      return;
    }
    try{
      const result=await cp.api('/api/analytics/disk-usage?path='+encodeURIComponent(path));if(token!==diskRequest)return;
      diskPath=result.path||'';diskPage.querySelector('[data-disk-location]').textContent=result.root+diskPath;
      diskPage.querySelector('[data-disk-total]').textContent=formatBytes(result.totalBytes)+' in this directory';
      const entries=result.entries||[],total=Math.max(1,result.totalBytes||0);renderDiskChart(result,entries);
      diskPage.querySelector('[data-disk-list]').innerHTML=entries.length?entries.map((entry,index)=>`<button type="button" class="disk-directory-row${entry.isDir?' is-directory':''}" data-disk-path="${esc(entry.path)}" ${entry.isDir?'':'disabled'}><i style="background:${diskColors[index%diskColors.length]}"></i><span><strong title="${esc(entry.name)}">${esc(entry.name)}</strong><small>${(entry.bytes/total*100).toFixed(1)}%</small></span><b>${formatBytes(entry.bytes)}</b></button>`).join(''):'<p class="usage-empty">This directory is empty.</p>';
      diskPage.querySelectorAll('[data-disk-path]:not([disabled])').forEach(button=>button.onclick=()=>loadDisk(button.dataset.diskPath));
      const up=diskPage.querySelector('[data-disk-up]');up.classList.toggle('hidden',!diskPath);up.dataset.diskPath=result.parent||'';
      status.textContent=(result.partial?'Partial scan: some paths could not be read · ':'')+entries.length+' entries · Updated '+new Date().toLocaleTimeString()+'. Chart includes the containing filesystem’s free space; other mounts can be opened separately.';
    }catch(error){if(token===diskRequest)status.textContent='Unable to calculate disk usage. '+error.message;}
  }
  diskPage.querySelector('[data-disk-refresh]').onclick=()=>loadDisk();diskPage.querySelector('[data-disk-up]').onclick=event=>loadDisk(event.currentTarget.dataset.diskPath||'');
  function activate(){const graphActive=location.hash==='#analytics',diskActive=location.hash==='#disk-usage',active=graphActive||diskActive;toggle.classList.toggle('active',active);clearInterval(timer);timer=null;if(active){toggle.setAttribute('aria-expanded','true');submenu.classList.remove('hidden');}if(graphActive){load();timer=setInterval(load,15000);}else request++;if(diskActive)loadDisk();}
  view.onchange=load;page.querySelector('[data-usage-refresh]').onclick=load;
  const navigate=window.LiteCPFeatures.navigate;window.LiteCPFeatures.navigate=id=>{navigate?.(id);activate();};window.addEventListener('hashchange',activate);
  const render=window.LiteCPFeatures.render;window.LiteCPFeatures.render=state=>{render(state);if((location.hash==='#analytics'&&!timer)||location.hash==='#disk-usage')activate();};
  if(cp.state()&&(location.hash==='#analytics'||location.hash==='#disk-usage')){cp.navigate(location.hash.slice(1),false);}
})();
