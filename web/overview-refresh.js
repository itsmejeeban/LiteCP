(() => {
  'use strict';
  const cp=window.LiteCP;
  let pending=false;
  async function refresh(){
    if(pending||document.hidden||(location.hash&&location.hash!=='#dashboard')||!cp.state())return;
    pending=true;
    const status=document.querySelector('#overviewMetricsStatus');
    try{
      if(await cp.refreshMetrics()===null)return;
      status.textContent='Metrics updated '+new Date().toLocaleTimeString()+'. Refreshes every 15 seconds.';
    }catch(error){status.textContent='Metrics could not refresh. Displayed values may be out of date.';}
    finally{pending=false;}
  }
  const status=document.createElement('p');status.id='overviewMetricsStatus';status.className='feature-note';
  document.querySelector('[data-page-view="dashboard"] .metrics-grid').after(status);
  const navigate=window.LiteCPFeatures.navigate;
  window.LiteCPFeatures.navigate=id=>{navigate?.(id);if(id==='dashboard')refresh();};
  window.addEventListener('hashchange',refresh);
  document.addEventListener('visibilitychange',refresh);
  setInterval(refresh,15000);refresh();
})();
