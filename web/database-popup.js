(() => {
 "use strict";
 const page=document.querySelector('[data-page-view="databases"]'),form=document.querySelector('#databaseForm');
 const dialog=document.createElement('dialog');dialog.className='fm-dialog database-create-dialog';dialog.setAttribute('aria-labelledby','databaseCreateTitle');
 const title=form.querySelector('h2');title.id='databaseCreateTitle';
 const close=document.createElement('button');close.type='button';close.className='icon-button';close.setAttribute('aria-label','Close create database');close.innerHTML='<svg viewBox="0 0 24 24"><path d="m6 6 12 12M18 6 6 18"/></svg>';form.querySelector('.panel-head').append(close);
 form.classList.remove('panel','compact-form-card');dialog.append(form);document.body.append(dialog);
 const body=form.querySelector('.settings-body'),submit=form.querySelector('[type="submit"]'),actions=document.createElement('div');actions.className='feature-actions';const cancel=document.createElement('button');cancel.type='button';cancel.className='button button-ghost';cancel.textContent='Cancel';actions.append(submit,cancel);body.append(actions);
 const generateIcon='<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 7v5h-5M4 17v-5h5M6 7a7 7 0 0 1 12-1l2 3M4 15l2 3a7 7 0 0 0 12-1"/></svg>';
 for(const [name,label,prefix] of [['name','database name','db_'],['username','username','user_']]){
  const input=form.elements[name],wrapper=document.createElement('span');wrapper.className='password-copy-field database-identifier-field';input.before(wrapper);wrapper.append(input);
  const generate=document.createElement('button');generate.type='button';generate.className='password-copy-button database-generate-button';generate.title='Generate '+label;generate.setAttribute('aria-label','Generate '+label);generate.innerHTML=generateIcon;
  generate.onclick=()=>{input.value=prefix+Array.from(crypto.getRandomValues(new Uint8Array(10)),value=>value.toString(16).padStart(2,'0')).join('');input.dispatchEvent(new Event('input',{bubbles:true}));};
  wrapper.append(generate,window.LiteCPPasswordCopy.button(()=>input.value,()=>{input.focus();input.select();},label));
 }
 const password=form.elements.password,passwordWrapper=password.closest('.password-copy-field'),passwordActions=body.querySelector('.password-actions'),generatePassword=passwordActions?.querySelector('button');
 if(passwordWrapper&&generatePassword){
  passwordWrapper.classList.add('database-password-field');generatePassword.className='password-copy-button database-generate-button';generatePassword.title='Generate password';generatePassword.setAttribute('aria-label','Generate password');generatePassword.innerHTML=generateIcon;passwordWrapper.append(generatePassword);passwordActions.classList.add('hidden');
 }
 const error=document.createElement('p');error.dataset.databaseFormError='';error.setAttribute('role','alert');error.className='feature-note';body.append(error);
 const heading=page.querySelector('.page-heading'),headerActions=document.createElement('div');headerActions.className='feature-actions';headerActions.append(document.querySelector('#databaseEngine'));
 const open=document.createElement('button');open.type='button';open.className='button button-primary';open.textContent='Create database';headerActions.append(open);heading.append(headerActions);page.querySelector('.management-grid').classList.add('database-list-layout');
 open.addEventListener('click',()=>{error.textContent='';dialog.showModal();form.elements.name.focus();});
 function dismiss(){if(!submit.disabled)dialog.close();}
 close.addEventListener('click',dismiss);cancel.addEventListener('click',dismiss);dialog.addEventListener('cancel',event=>{if(submit.disabled)event.preventDefault();});
 form.addEventListener('database-created',()=>dialog.close());
})();
