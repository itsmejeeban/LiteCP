(() => {
 "use strict";
 const $=s=>document.querySelector(s),notice=$('#updateNotice'),label=notice.querySelector('span'),app=$('#appView');
 const checkButton=$('#checkUpdatesButton'),install=$('#installPanelUpdate'),versions=$('#panelUpdateVersions'),message=$('#panelUpdateMessage');
 const progress=$('#panelUpdateProgress'),bar=$('#panelUpdateBar'),percent=$('#panelUpdatePercent');
 function showProgress(result){
  progress.hidden=!['running','success','failed'].includes(result.state);
  const steps=result.state==='success'?4:result.completedStages;
  const count=steps==null?NaN:Number(steps);
  if(Number.isInteger(count)&&count>=0&&count<=4){
   const value=count*25;bar.value=value;percent.textContent=value+'%';
  }else{bar.removeAttribute('value');percent.textContent='';}
 }
 let checking=false,last=0,latest='',running=false,polling=false;
 function say(text){if(message)message.textContent=text;}
 async function status(){
  if(!install||app.classList.contains('hidden')||polling)return;
  polling=true;
  try{const response=await fetch('/api/updates/status',{credentials:'same-origin',cache:'no-store'});if(!response.ok)return;const result=await response.json();const wasRunning=running;running=result.state==='running';showProgress(result);if(wasRunning&&result.state==='success'){latest='';notice.classList.add('hidden');check(true);}if(result.state!=='idle')say(result.message||'LiteCP update is running…');install.disabled=running||!latest;}
  catch{if(running)say('LiteCP is restarting. Reconnect and sign in again shortly.');}finally{polling=false;}
 }
 async function check(force=false){
  if(app.classList.contains('hidden')||checking||(!force&&Date.now()-last<300000))return;
  checking=true;if(checkButton)checkButton.disabled=true;
  try {
   const response=await fetch('/api/updates',{credentials:'same-origin',cache:'no-store'});
   if(!response.ok)throw new Error('Could not check for updates. Try again shortly.');
   const result=await response.json();last=Date.now();
   const available=result.checked&&result.available&&/^v?\d+\.\d+\.\d+$/.test(result.latest);
   latest=available?result.latest:'';
   notice.classList.toggle('hidden',!available);
   if(available){label.textContent=`LiteCP ${result.latest.replace(/^v/,'')} version available for update`;notice.href='#settings/update';}
   if(versions)versions.textContent=`Installed: ${result.current}${/^[a-f0-9]{7,40}$/i.test(result.build||'')?' (build '+result.build.slice(0,12)+')':''}${result.latest?' · Latest stable release: '+result.latest:''}`;
   if(!running)say(!result.checked?(result.message||'The release service is unavailable. Try again later.'):available?'A new LiteCP version is ready to install.':'No newer stable LiteCP release is available.');
   if(install)install.disabled=running||!available;
  }catch(error){say(error instanceof TypeError?'Could not reach the panel. Check your connection and the panel HTTPS certificate, then try again.':error.message);}
  finally{checking=false;if(checkButton)checkButton.disabled=false;}
 }
 checkButton?.addEventListener('click',()=>check(true));
 install?.addEventListener('click',async()=>{
  if(!latest||running)return;
  const selected=latest;
  if(!await window.LiteCPDialogs.confirm(`Install LiteCP ${selected}? The panel will restart and you may need to sign in again. Hosted websites remain online. The previous binary and panel data will be retained.`,{title:'Update LiteCP',confirmText:'Update LiteCP'}))return;
  install.disabled=true;
  try{await window.LiteCP.api('/api/updates/install',{method:'POST',body:{version:selected}});running=true;showProgress({state:'running',completedStages:0});say('Starting the LiteCP update…');setTimeout(status,1500);}
  catch(error){say(error.message);install.disabled=false;}
 });
 notice.addEventListener?.('click',event=>{event.preventDefault();window.LiteCP.navigate('settings/update');});
 new MutationObserver(()=>{check();status();}).observe(app,{attributes:true,attributeFilter:['class']});
 setInterval(()=>check(),300000);setInterval(()=>{if(running)status();},4000);check();status();
})();
