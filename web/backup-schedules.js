(() => {
 const cp=window.LiteCP,settings=document.getElementById('backupSettingsForm'),panel=document.createElement('article');panel.className='panel';
 panel.innerHTML='<div class="panel-head"><h2>Backup schedules</h2><button class="button button-primary" type="button" data-add-schedule>Create schedule</button></div><div class="settings-body"><p>Each schedule has its own local retention count. Remote copies remain on the destination.</p><div data-schedule-list></div></div>';
 settings.insertAdjacentElement('afterend',panel);
 const dialog=document.createElement('dialog');dialog.className='fm-dialog';dialog.setAttribute('aria-label','Backup schedule');
 dialog.innerHTML='<form><header><h2>Backup schedule</h2><button type="button" class="icon-button" data-close aria-label="Close">×</button></header><div class="fm-dialog-body"><input name="id" type="hidden"><label class="field"><span>Name</span><input name="name" required maxlength="80"></label><label class="field"><span>Website</span><select name="siteId"></select></label><label class="field"><span>Destination</span><select name="destinationId"></select></label><label class="field"><span>Frequency</span><select name="preset"><option value="daily">Daily</option><option value="hourly">Hourly</option><option value="weekly">Weekly</option><option value="monthly">Monthly</option><option value="custom">Custom schedule</option></select></label><div data-timing="daily"><label class="field">Time<input name="dailyTime" type="time" value="02:00" required></label></div><div data-timing="hourly"><label class="field">Minute past each hour<input name="hourlyMinute" type="number" min="0" max="59" value="0" required></label></div><div data-timing="weekly" data-weekdays></div><div data-timing="monthly"><label class="field">Day of month<input name="monthDay" type="number" min="1" max="31" value="1" required></label><label class="field">Time<input name="monthlyTime" type="time" value="02:00" required></label><small>Days that do not exist in a month are skipped.</small></div><div data-timing="custom"><label class="field">Cron expression<input name="cron" placeholder="*/30 * * * *" required><small>Minute, hour, day, month, weekday.</small></label></div><p>Schedules use the panel time zone. Missed runs while the panel is offline are not replayed.</p><label class="field"><span>Local backups to keep per website</span><input name="keep" type="number" min="0" max="10000" value="0" required><small>0 keeps all. Older backups from this schedule are deleted only after a successful new backup.</small></label><label><input name="enabled" type="checkbox" checked> Enabled</label><p role="alert" data-error></p><div class="feature-actions"><button class="button button-primary" type="submit">Save schedule</button><button type="button" class="button button-secondary" data-close>Cancel</button></div></div></form>';
 document.body.append(dialog);const form=dialog.querySelector('form');let items=[];
 const weekdays=['Sunday','Monday','Tuesday','Wednesday','Thursday','Friday','Saturday'];
 const timingFields=[...form.querySelectorAll('[data-timing]')];
 form.querySelector('[data-weekdays]').innerHTML=weekdays.map((day,i)=>`<div class="field"><label><input type="checkbox" name="day${i}"> ${day}</label><input type="time" name="time${i}" value="02:00" aria-label="${day} backup time" required></div>`).join('');
 function showTiming(){
  const mode=form.elements.preset.value;
  for(const section of timingFields){section.hidden=section.dataset.timing!==mode;for(const input of section.querySelectorAll('input'))input.disabled=section.hidden;}
  if(mode==='weekly')weekdays.forEach((_,i)=>{form.elements[`time${i}`].disabled=!form.elements[`day${i}`].checked;});
 }
 function readTiming(item){
  const e=form.elements, times=item?.weekdayTimes;
  if(times&&Object.keys(times).length){
   e.preset.value='weekly';
   for(const [day,time] of Object.entries(times)){e[`day${day}`].checked=true;e[`time${day}`].value=time;}
  }else if(item){
   const [minute,hour,day,month,week]=(item.cron||'').trim().split(/\s+/);
   const clock=[hour,minute].map(v=>(v||'').padStart(2,'0')).join(':');
   const simpleTime=/^([01][0-9]|2[0-3]):[0-5][0-9]$/.test(clock);
   e.preset.value='custom';
   if(month==='*'&&week!==undefined){
    if(day==='*'&&week==='*'&&hour==='*'&&/^([0-9]|[0-5][0-9])$/.test(minute)){e.preset.value='hourly';e.hourlyMinute.value=minute;}
    else if(simpleTime&&day==='*'&&week==='*'){e.preset.value='daily';e.dailyTime.value=clock;}
    else if(simpleTime&&day==='*'&&/^[0-7]$/.test(week)){const i=Number(week)%7;e.preset.value='weekly';e[`day${i}`].checked=true;e[`time${i}`].value=clock;}
    else if(simpleTime&&week==='*'&&/^([1-9]|[12][0-9]|3[01])$/.test(day)){e.preset.value='monthly';e.monthDay.value=day;e.monthlyTime.value=clock;}
   }
  }
  showTiming();
 }
 function saveTiming(){
  const e=form.elements,mode=e.preset.value;
  const cronTime=value=>value.split(':').reverse().map(Number).join(' ');
  if(mode==='daily')return {cron:`${cronTime(e.dailyTime.value)} * * *`};
  if(mode==='hourly')return {cron:`${Number(e.hourlyMinute.value)} * * * *`};
  if(mode==='monthly')return {cron:`${cronTime(e.monthlyTime.value)} ${Number(e.monthDay.value)} * *`};
  if(mode==='custom')return {cron:e.cron.value.trim()};
  const weekdayTimes={};
  weekdays.forEach((_,i)=>{if(e[`day${i}`].checked)weekdayTimes[i]=e[`time${i}`].value;});
  if(!Object.keys(weekdayTimes).length)throw new Error('Select at least one weekday.');
  return {cron:'',weekdayTimes};
 }
 function timingLabel(item){
  if(!item.weekdayTimes)return item.cron;
  return weekdays.map((day,i)=>item.weekdayTimes[i]?`${day} ${item.weekdayTimes[i]}`:'').filter(Boolean).join(', ');
 }
 async function load(){items=await cp.api('/api/backup-schedules');const box=panel.querySelector('[data-schedule-list]');box.replaceChildren();if(!items.length){box.textContent='No schedules yet.';return}for(const item of items){const row=document.createElement('div');row.className='panel settings-body';const title=document.createElement('strong');title.textContent=item.name;const text=document.createElement('p');text.textContent=`${item.enabled?'Enabled':'Paused'} · ${timingLabel(item)} · Keep ${item.keep||'all'} · ${item.status||'Not run yet'}`;row.append(title,text);for(const action of ['Edit','Delete']){const button=document.createElement('button');button.type='button';button.className='button '+(action==='Delete'?'button-danger':'button-secondary');button.textContent=action;button.onclick=async()=>{if(action==='Edit'){open(item);return}if(!await window.LiteCPDialogs.confirm(`Delete schedule “${item.name}”? Existing backups will be kept.`,{title:'Delete backup schedule',confirmText:'Delete schedule',danger:true}))return;try{await cp.api('/api/backup-schedules',{method:'DELETE',body:{id:item.id}});await load()}catch(e){cp.showToast('Cannot delete schedule',e.message)}};row.append(button)}box.append(row)}}
 function open(item){form.reset();const state=cp.state();form.elements.siteId.replaceChildren(new Option('All websites','all'),...(state.sites||[]).map(s=>new Option(s.domain,s.id)));form.elements.destinationId.replaceChildren(new Option('Local storage only',''),...(state.backupDestinations||[]).map(d=>new Option(d.name,d.id)));if(item){for(const key of ['id','name','siteId','destinationId','cron','keep'])form.elements[key].value=item[key];form.elements.enabled.checked=item.enabled;}else{form.elements.id.value=''}readTiming(item);dialog.querySelector('[data-error]').textContent='';dialog.showModal();form.elements.name.focus()}
 panel.querySelector('[data-add-schedule]').onclick=()=>open();dialog.querySelectorAll('[data-close]').forEach(b=>b.onclick=()=>dialog.close());form.elements.preset.onchange=showTiming;form.querySelector('[data-weekdays]').onchange=showTiming;
 form.onsubmit=async event=>{event.preventDefault();const submit=form.querySelector('[type=submit]');submit.disabled=true;try{await cp.api('/api/backup-schedules',{method:form.elements.id.value?'PUT':'POST',body:{id:form.elements.id.value,name:form.elements.name.value,siteId:form.elements.siteId.value,destinationId:form.elements.destinationId.value,...saveTiming(),keep:Number(form.elements.keep.value),enabled:form.elements.enabled.checked}});dialog.close();await load();cp.showToast('Schedule saved','Backup timing and retention updated.')}catch(e){dialog.querySelector('[data-error]').textContent=e.message}finally{submit.disabled=false}};
 setInterval(()=>{if(panel.getClientRects().length&&!document.hidden)load().catch(()=>{});},5000);
})();
