(() => {
 const cp=window.LiteCP,table=document.getElementById('sitesTable');
 table.addEventListener('click',async event=>{const button=event.target.closest('[data-site-suspend]');if(!button)return;const site=cp.state().sites.find(s=>s.id===button.dataset.siteSuspend);if(!site)return;if(!await window.LiteCPDialogs.confirm(`${site.suspended?'Restore access to':'Suspend'} ${site.domain}? Website files, databases, email and FTP are preserved. This controls web access only.`,{title:site.suspended?'Unsuspend website':'Suspend website',confirmText:site.suspended?'Unsuspend':'Suspend'}))return;button.disabled=true;try{await cp.api('/api/sites/suspension',{method:'PUT',body:{id:site.id,suspended:!site.suspended}});await cp.refresh();}catch(e){cp.showToast('Cannot update website',e.message);}finally{button.disabled=false;}});
})();

