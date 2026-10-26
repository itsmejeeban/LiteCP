(() => {
 "use strict";
 const page=document.querySelector('[data-page-view="databases"]'),form=document.querySelector('#databaseForm');
 const dialog=document.createElement('dialog');dialog.className='fm-dialog database-create-dialog';dialog.setAttribute('aria-labelledby','databaseCreateTitle');
 const title=form.querySelector('h2');title.id='databaseCreateTitle';
 const close=document.createElement('button');close.type='button';close.className='icon-button';close.setAttribute('aria-label','Close create database');close.innerHTML='<svg viewBox="0 0 24 24"><path d="m6 6 12 12M18 6 6 18"/></svg>';form.querySelector('.panel-head').append(close);
 form.classList.remove('panel','compact-form-card');dialog.append(form);document.body.append(dialog);
 const body=form.querySelector('.settings-body'),submit=form.querySelector('[type="submit"]'),actions=document.createElement('div');actions.className='feature-actions';const cancel=document.createElement('button');cancel.type='button';cancel.className='button button-ghost';cancel.textContent='Cancel';actions.append(submit,cancel);body.append(actions);
 const error=document.createElement('p');error.dataset.databaseFormError='';error.setAttribute('role','alert');error.className='feature-note';body.append(error);
 const heading=page.querySelector('.page-heading'),headerActions=document.createElement('div');headerActions.className='feature-actions';headerActions.append(document.querySelector('#databaseEngine'));
 const open=document.createElement('button');open.type='button';open.className='button button-primary';open.textContent='Create database';headerActions.append(open);heading.append(headerActions);page.querySelector('.management-grid').classList.add('database-list-layout');
 open.addEventListener('click',()=>{error.textContent='';dialog.showModal();form.elements.name.focus();});
 function dismiss(){if(!submit.disabled)dialog.close();}
 close.addEventListener('click',dismiss);cancel.addEventListener('click',dismiss);dialog.addEventListener('cancel',event=>{if(submit.disabled)event.preventDefault();});
 form.addEventListener('database-created',()=>dialog.close());
})();
