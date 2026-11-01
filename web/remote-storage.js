(() => {
 'use strict';
 const cp=window.LiteCP,cache=new Map(),pending=new Set();let running=0;
 const bytes=n=>{const units=['B','KiB','MiB','GiB','TiB','PiB'];let i=0;while(n>=1024&&i<units.length-1){n/=1024;i++;}return n.toLocaleString(undefined,{maximumFractionDigits:1})+' '+units[i];};
 function paint(cell,data){cell.replaceChildren();if(!data.available){cell.textContent='Unavailable';cell.title=data.message||'Remote server cannot report capacity';return;}const total=document.createElement('div'),free=document.createElement('div');total.textContent='Total: '+bytes(data.total)+' (100%)';free.textContent='Free: '+bytes(data.free)+' ('+data.freePercent.toFixed(1)+'%)';cell.append(total,free);cell.title=data.message+' — Checked '+new Date(data.checkedAt).toLocaleString();}
 function refresh(){
  const destinations=cp.state()?.backupDestinations||[];
  document.querySelectorAll('[data-remote-storage]').forEach(cell=>{
   const d=destinations.find(d=>d.id===cell.dataset.remoteStorage);if(!d)return;
   const key=JSON.stringify([d.id,d.host,d.port,d.protocol,d.remotePath,d.username,d.hostKeySha256]);const saved=cache.get(key);
   if(saved&&Date.now()-saved.at<60000){paint(cell,saved.data);return;}
   if(pending.has(key)||running>=2)return;
   cell.textContent='Checking storage…';pending.add(key);running++;
   cp.api('/api/backup-destinations/storage',{method:'POST',body:{id:d.id}}).then(data=>cache.set(key,{at:Date.now(),data})).catch(()=>cache.set(key,{at:Date.now(),data:{available:false,message:'Storage check failed; try Test connection'}})).finally(()=>{pending.delete(key);running--;refresh();});
  });
 }
 window.LiteCPRemoteStorage={refresh,invalidate(id){for(const [key]of cache)if(JSON.parse(key)[0]===id)cache.delete(key);refresh();}};
 const previous=window.LiteCPFeatures.render;window.LiteCPFeatures.render=state=>{previous(state);refresh();};refresh();
})();
