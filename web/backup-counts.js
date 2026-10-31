(() => {
 "use strict";
 const cp=window.LiteCP,local=document.querySelector('#totalLocalBackupsValue'),remote=document.querySelector('#totalRemoteBackupsValue');let pending=false;
 function backupAge(value,now=Date.now()){
  const timestamp=value?Date.parse(value):NaN;
  if(!Number.isFinite(timestamp))return 'No backups yet';
  const minutes=Math.floor(Math.max(0,now-timestamp)/60000);
  if(minutes<1)return 'Last created just now';
  const amount=minutes<60?minutes:minutes<1440?Math.floor(minutes/60):Math.floor(minutes/1440);
  const unit=minutes<60?'minute':minutes<1440?'hour':'day';
  return `Last created ${amount} ${unit}${amount===1?'':'s'} ago`;
 }
 function showAge(localTime,remoteTime){
  document.querySelector('#lastLocalBackupCreated').textContent=backupAge(localTime);
  document.querySelector('#lastRemoteBackupCreated').textContent=backupAge(remoteTime);
 }
 async function refresh(){
  if(pending||document.hidden||(location.hash&&location.hash!=='#dashboard'))return;
  pending=true;
  try{const result=await cp.api('/api/backups/counts');local.textContent=result.local;remote.textContent=result.remote;showAge(result.lastLocalCreatedAt,result.lastRemoteCreatedAt);local.title='Existing scheduled backups, completed manual website backups and system snapshots';remote.title='Successful remote uploads recorded in Backup History; remote storage is not scanned';}
  catch(error){local.textContent=remote.textContent='—';local.title=remote.title='Backup counts unavailable: '+error.message;document.querySelector('#lastLocalBackupCreated').textContent=document.querySelector('#lastRemoteBackupCreated').textContent='Last backup unavailable';}
  finally{pending=false;}
 }
 window.addEventListener('hashchange',refresh);document.addEventListener('visibilitychange',refresh);setInterval(refresh,30000);refresh();
})();
