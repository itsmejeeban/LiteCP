(() => {
  "use strict";
  const themes = [
    {id:"default",name:"Default",description:"Original LiteCP design",accent:"#0866ff",surface:"#0d0d0d",radius:"8px"},
    {id:"shadcn",name:"shadcn/ui",description:"Neutral zinc, crisp borders and restrained surfaces",accent:"#fafafa",surface:"#18181b",radius:"6px"},
    {id:"mui",name:"Material UI (MUI)",description:"Indigo accents, elevated cards and rounded controls",accent:"#b9a5ff",surface:"#211f26",radius:"16px"},
    {id:"mantine",name:"Mantine",description:"Compact blue controls and slate surfaces",accent:"#339af0",surface:"#25262b",radius:"4px"},
    {id:"heroui",name:"HeroUI",description:"Violet accents and generous soft corners",accent:"#a78bfa",surface:"#181424",radius:"18px"},
    {id:"chakra",name:"Chakra UI",description:"Teal accents and clear, comfortable controls",accent:"#5eead4",surface:"#17232b",radius:"8px"},
    {id:"primereact",name:"PrimeReact",description:"Emerald highlights and structured data tables",accent:"#34d399",surface:"#18212f",radius:"6px"},
    {id:"fluent",name:"Fluent UI",description:"Windows-inspired blue and precise geometry",accent:"#60a5fa",surface:"#202020",radius:"4px"},
    {id:"daisyui",name:"daisyUI",description:"Playful violet with bold, rounded buttons",accent:"#c084fc",surface:"#211b32",radius:"14px"},
    {id:"baseui",name:"Base UI",description:"Minimal graphite with subtle orange accents",accent:"#fdba74",surface:"#1c1c1c",radius:"6px"}
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
