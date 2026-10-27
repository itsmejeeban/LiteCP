(() => {
  "use strict";
  function button(readValue, selectValue) {
    const copy = document.createElement("button");
    copy.type = "button";
    copy.className = "password-copy-button";
    copy.title = "Copy password";
    copy.setAttribute("aria-label", "Copy password");
    copy.innerHTML = '<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V4H4v12h4"/></svg>';
    copy.addEventListener("click", async event => {
      event.preventDefault();
      const value = readValue();
      if (!value) { window.LiteCP.showToast("No password to copy", "Generate or enter a password first."); return; }
      try {
        await navigator.clipboard.writeText(value);
        window.LiteCP.showToast("Copied", "Password copied to your clipboard.");
      } catch {
        selectValue?.();
        window.LiteCP.showToast("Copy unavailable", "Select and copy the password manually.", "error");
      }
    });
    return copy;
  }
  function enhance(input) {
    if (!input || input.parentElement?.classList.contains("password-copy-field")) return;
    const wrapper = document.createElement("span");
    wrapper.className = "password-copy-field";
    input.insertAdjacentElement("beforebegin", wrapper);
    wrapper.append(input, button(() => input.value, () => { input.focus(); input.select(); }));
  }
  window.LiteCPPasswordCopy = { enhance, button };
})();
