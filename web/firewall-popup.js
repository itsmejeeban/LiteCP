(() => {
  const page = document.querySelector('[data-page-view="firewall"]');
  const grid = page.querySelector('.management-grid');
  const form = document.getElementById('firewallForm');
  const table = page.querySelector('.management-table');
  grid.style.gridTemplateColumns = 'minmax(0, 1fr)';
  const dialog = document.createElement('dialog');
  dialog.className = 'fm-dialog firewall-rule-dialog';
  dialog.setAttribute('aria-labelledby', 'firewallRuleTitle');
  const title = form.querySelector('h2');title.id = 'firewallRuleTitle';
  form.classList.remove('panel', 'compact-form-card');
  dialog.append(form);document.body.append(dialog);
  const close = document.createElement('button');close.type = 'button';close.className = 'icon-button';
  close.setAttribute('aria-label', 'Close firewall rule');close.textContent = '×';
  form.querySelector('.panel-head').append(close);
  const open = document.createElement('button');open.type = 'button';open.className = 'button button-primary';open.textContent = 'Add rules';
  table.querySelector('.panel-head').append(open);
  const submit = form.querySelector('[type="submit"]');
  function show(edit) {
    title.textContent = edit ? 'Edit rule' : 'Add rule';
    submit.textContent = edit ? 'Save changes' : 'Add rule';
    dialog.showModal();form.elements.port.focus();
  }
  function dismiss(){if(form.getAttribute('aria-busy') !== 'true')dialog.close();}
  open.addEventListener('click',()=>{form.reset();form.elements.id.value='';form.elements.direction.dispatchEvent(new Event('change'));show(false);});
  close.addEventListener('click',dismiss);
  document.getElementById('firewallCancelEdit').addEventListener('click',dismiss);
  dialog.addEventListener('cancel',event=>{if(form.getAttribute('aria-busy') === 'true')event.preventDefault();});
  table.addEventListener('click',event=>{
    if(event.target.closest('[data-firewall-action="edit"]'))setTimeout(()=>show(true),0);
  });
  form.addEventListener('firewall-saved',()=>dialog.close());
})();
