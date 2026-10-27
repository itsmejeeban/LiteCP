(() => {
  "use strict";
  const groups = [
    {id:"website-tools", title:"Website Tools", icon:"tools", after:"websites", pages:["file-manager","files","databases","website-backups","website-cron","website-ftp","website-ssl"]},
    {id:"security", title:"Security", icon:"security", after:"email", pages:["firewall","security","waf"]},
    {id:"environment", title:"Server Environment", icon:"server-environment", pages:["applications","extensions"]}
  ];
  let anchor = document.querySelector('.nav-item[data-page="email"]');
  for (const group of groups) {
    if(group.after)anchor=document.querySelector(`.nav-item[data-page="${group.after}"]`);
    const wrapper=document.createElement("div"),button=document.createElement("button"),submenu=document.createElement("nav");
    wrapper.className="sidebar-feature-group";
    button.type="button";button.className="nav-item sidebar-group-toggle";
    button.setAttribute("aria-controls",`sidebar-${group.id}-submenu`);
    const image=document.createElement("img");image.src=`assets/sidebar/${group.icon}.svg`;image.alt="";image.width=22;image.height=22;
    const label=document.createElement("span");label.textContent=group.title;
    button.append(image,label);button.insertAdjacentHTML("beforeend",'<svg class="sidebar-group-chevron" viewBox="0 0 24 24" aria-hidden="true"><path d="m7 10 5 5 5-5"/></svg>');
    submenu.id=`sidebar-${group.id}-submenu`;submenu.className="settings-submenu hidden";submenu.setAttribute("aria-label",group.title);
    for(const page of group.pages){const link=document.querySelector(`.nav-item[data-page="${page}"]`);if(link)submenu.append(link);}
    wrapper.append(button,submenu);anchor.after(wrapper);anchor=wrapper;
    const expand=open=>{submenu.classList.toggle("hidden",!open);button.setAttribute("aria-expanded",String(open));};
    button.addEventListener("click",()=>expand(button.getAttribute("aria-expanded")!=="true"));
    group.button=button;group.expand=expand;expand(false);
  }
  let lastRoute="";
  function sync(){
    const route=location.hash.slice(1).split("/")[0];
    for(const group of groups){const active=group.pages.includes(route);group.button.classList.toggle("active",active);if(active&&route!==lastRoute)group.expand(true);}
    lastRoute=route;
  }
  const previous=window.LiteCPFeatures.navigate;
  window.LiteCPFeatures.navigate=id=>{previous?.(id);sync();};
  window.addEventListener("hashchange",sync);sync();
})();
