(() => {
 "use strict";
 const cp=window.LiteCP,local=document.querySelector('#totalLocalBackupsValue'),remote=document.querySelector('#totalRemoteBackupsValue');let pending=false;
 async function refresh(){
  if(pending||document.hidden||(location.hash&&location.hash!=='#dashboard'))return;
  pending=true;
  try{const result=await cp.api('/api/backups/counts');local.textContent=result.local;remote.textContent=result.remote;local.title='Existing scheduled backups, completed manual website backups and system snapshots';remote.title='Successful remote uploads recorded in Backup History; remote storage is not scanned';}
  catch(error){local.textContent=remote.textContent='—';local.title=remote.title='Backup counts unavailable: '+error.message;}
  finally{pending=false;}
 }
 window.addEventListener('hashchange',refresh);document.addEventListener('visibilitychange',refresh);setInterval(refresh,30000);refresh();
})();
