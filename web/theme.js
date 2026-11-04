(() => {
  "use strict";
  const themes = [
    {id:"default",name:"Default",description:"Neutral zinc, crisp borders and restrained surfaces",accent:"#fafafa",surface:"#18181b",radius:"6px"}
  ];
  const storageKey="litecp.uiTheme";
  const valid = value => themes.some(theme=>theme.id===value) ? value : "default";
  function read(){try{return valid(localStorage.getItem(storageKey));}catch{return "default";}}
  function apply(value){
    const id=valid(value);
    document.documentElement.dataset.theme="dark";
    document.documentElement.dataset.uiTheme=id;
    return id;
  }
  let saved=read();
  apply(saved);
  window.LiteCPTheme={themes,apply,current:()=>document.documentElement.dataset.uiTheme};
  window.addEventListener("storage",event=>{if(event.key===storageKey||event.key===null){saved=read();apply(saved);sync();}});
  function sync(){
    const selected=document.querySelector('#appearanceTheme');
    if(selected)selected.value=document.documentElement.dataset.uiTheme;
    document.querySelectorAll('[name="appearancePreset"]').forEach(input=>{input.checked=input.value===document.documentElement.dataset.uiTheme;});
  }
  document.addEventListener("DOMContentLoaded",()=>{
    const form=document.querySelector('#appearanceForm');
    if(!form)return;
    const select=form.querySelector('#appearanceTheme'),cards=form.querySelector('#appearancePresets'),status=form.querySelector('#appearanceStatus');
    for(const theme of themes){
      const option=document.createElement('option');option.value=theme.id;option.textContent=theme.name;select.append(option);
      const label=document.createElement('label');label.className='appearance-preset';
      const radio=document.createElement('input');radio.type='radio';radio.name='appearancePreset';radio.value=theme.id;
      const preview=document.createElement('span');preview.className='appearance-swatch';preview.setAttribute('aria-hidden','true');
      preview.style.setProperty('--preview-accent',theme.accent);preview.style.setProperty('--preview-surface',theme.surface);preview.style.borderRadius=theme.radius;
      const title=document.createElement('strong');title.textContent=theme.name;
      const detail=document.createElement('small');detail.textContent=theme.description;
      label.append(radio,preview,title,detail);cards.append(label);
    }
    function preview(value){apply(value);sync();status.textContent='Previewing '+themes.find(theme=>theme.id===valid(value)).name+'. Save to keep this theme.';}
    select.addEventListener('change',()=>preview(select.value));
    cards.addEventListener('change',event=>{if(event.target.name==='appearancePreset')preview(event.target.value);});
    form.querySelector('#appearanceCancel').addEventListener('click',()=>{apply(saved);sync();status.textContent='Saved theme restored.';});
    form.addEventListener('submit',event=>{
      event.preventDefault();
      try{localStorage.setItem(storageKey,valid(select.value));saved=valid(select.value);apply(saved);status.textContent='Theme saved for this browser.';}
      catch{status.textContent='Theme preview applied, but browser storage is unavailable. It could not be saved.';}
    });
    sync();
  });
})();
