(() => {
 "use strict";
 const notice=document.querySelector('#updateNotice'),label=notice.querySelector('span'),app=document.querySelector('#appView');
 let checking=false,last=0;
 async function check(){
  if(app.classList.contains('hidden')||checking||Date.now()-last<300000)return;
  checking=true;
  try {
   const response=await fetch('/api/updates',{credentials:'same-origin',cache:'no-store'});
   if(!response.ok)return;
   const result=await response.json();last=Date.now();
   const available=result.checked&&result.available&&/^v?\d+\.\d+\.\d+$/.test(result.latest);
   notice.classList.toggle('hidden',!available);
   if(available){label.textContent=`LiteCP ${result.latest.replace(/^v/,'')} version available for update`;notice.href=`https://github.com/itsmejeeban/LiteCP/releases/tag/${result.latest}`;}
  }catch{ /* An unavailable release service must not interrupt the panel. */ }
  finally{checking=false;}
 }
 new MutationObserver(check).observe(app,{attributes:true,attributeFilter:['class']});
 document.querySelector('#refreshButton').addEventListener('click',check);
 setInterval(check,300000);check();
})();
