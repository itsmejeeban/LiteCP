(() => {
  "use strict";
  const cp=window.LiteCP,scan=document.querySelector('[data-page-view="security"]');
  const link=document.querySelector('.nav-item[data-page="security"]');
  link.querySelector('span').textContent="Malware Scan";
  scan.querySelector('.page-heading h1').textContent="Malware Scan";
  scan.querySelector('.page-heading p').textContent="Scan website files with ClamAV.";
  const waf=document.createElement('section');waf.className="page";waf.dataset.pageView="waf";
  waf.innerHTML='<div class="page-heading"><div><h1>WAF</h1><p>Manage the web application firewall for each website.</p></div></div>';
  const wafTable=document.querySelector('#wafTable');wafTable.classList.add('table-scroll');wafTable.tabIndex=0;wafTable.setAttribute('role','region');wafTable.setAttribute('aria-label','Website firewall settings');
  waf.append(wafTable.closest('.panel'));scan.after(waf);
  scan.querySelector('.management-grid').style.gridTemplateColumns="minmax(0,780px)";
  const wafLink=document.createElement('a');wafLink.href="#waf";wafLink.className="nav-item";wafLink.dataset.page="waf";
  wafLink.innerHTML=link.querySelector('svg').outerHTML+'<span>WAF</span>';
  wafLink.addEventListener('click',event=>{event.preventDefault();cp.navigate('waf');});link.after(wafLink);
  cp.navigate(location.hash.slice(1)||'dashboard',false);
})();
