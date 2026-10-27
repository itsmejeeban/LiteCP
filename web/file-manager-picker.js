(() => {
  "use strict";
  const cp=window.LiteCP,esc=cp.escapeHTML,page=document.createElement('section');
  page.className='page website-tool-page';page.dataset.pageView='file-manager';
  page.innerHTML='<div class="page-heading"><h1>File Manager</h1></div><article class="panel"><form class="settings-body website-tool-picker"><label class="field"><span>Website</span><select required aria-label="Website for File Manager"></select></label><button type="submit" class="button button-primary">Open File Manager</button></form></article>';
  document.querySelector('.main-content').append(page);
  const link=document.querySelector('#sidebar .nav-item[data-page="files"]');
  link.dataset.page='file-manager';link.href='#file-manager';
  link.addEventListener('click',event=>{event.preventDefault();event.stopImmediatePropagation();cp.navigate('file-manager');},true);
  const select=page.querySelector('select');
  function render(state){const selected=select.value;select.innerHTML='<option value="">Select a website</option>'+(state?.sites||[]).map(site=>`<option value="${esc(site.id)}">${esc(site.domain)}</option>`).join('');if([...select.options].some(option=>option.value===selected))select.value=selected;page.querySelector('button').disabled=!(state?.sites||[]).length;}
  page.querySelector('form').onsubmit=async event=>{event.preventDefault();if(!select.value)return;const target=document.querySelector('#fileSiteSelect');target.value=select.value;target.dispatchEvent(new Event('change',{bubbles:true}));cp.navigate('files');try{await cp.goToFilePath('');}catch(error){cp.handleError(error);}};
  const previous=window.LiteCPFeatures.render;window.LiteCPFeatures.render=state=>{previous(state);render(state);};render(cp.state());
})();
