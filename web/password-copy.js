(() => {
  "use strict";
  function button(readValue, selectValue, name = "password") {
    const copy = document.createElement("button");
    copy.type = "button";
    copy.className = "password-copy-button";
    copy.title = `Copy ${name}`;
    copy.setAttribute("aria-label", `Copy ${name}`);
    copy.innerHTML = '<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V4H4v12h4"/></svg>';
    copy.addEventListener("click", async event => {
      event.preventDefault();
      const value = readValue();
      if (!value) { window.LiteCP.showToast(`No ${name} to copy`, `Generate or enter a ${name} first.`); return; }
      try {
        await navigator.clipboard.writeText(value);
        window.LiteCP.showToast("Copied", `${name[0].toUpperCase()+name.slice(1)} copied to your clipboard.`);
      } catch {
        selectValue?.();
        window.LiteCP.showToast("Copy unavailable", `Select and copy the ${name} manually.`, "error");
      }
    });
    return copy;
  }
  function visibilityButton(input) {
    const toggle=document.createElement("button");toggle.type="button";toggle.className="password-copy-button password-visibility-button";
    function render(){
      const visible=input.type==="text",label=visible?"Hide password":"Show password";
      toggle.title=label;toggle.setAttribute("aria-label",label);toggle.setAttribute("aria-pressed",String(visible));
      toggle.innerHTML=visible?'<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m3 3 18 18M10.6 10.6a2 2 0 0 0 2.8 2.8M9.9 5.2A11 11 0 0 1 12 5c6 0 10 7 10 7a18 18 0 0 1-3.2 3.8M6.2 6.2A20 20 0 0 0 2 12s4 7 10 7a11 11 0 0 0 5.8-1.6"/></svg>':'<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12Z"/><circle cx="12" cy="12" r="3"/></svg>';
    }
    toggle.addEventListener("click",event=>{event.preventDefault();input.type=input.type==="password"?"text":"password";render();});render();return toggle;
  }
  function enhance(input, options = {}) {
    if (!input || input.parentElement?.classList.contains("password-copy-field")) return;
    const wrapper = document.createElement("span");
    wrapper.className = "password-copy-field"+(options.copy===false?" password-eye-only":"");
    input.insertAdjacentElement("beforebegin", wrapper);
    wrapper.append(input, visibilityButton(input));
    if(options.copy!==false)wrapper.append(button(() => input.value, () => { input.focus(); input.select(); }));
  }
  window.LiteCPPasswordCopy = { enhance, button };
})();
