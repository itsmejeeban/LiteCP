(() => {
  "use strict";

  const selectors = [
    ".website-tool-picker > .field > select",
    "#fileSiteSelect",
    '[data-page-view="website-dns"] select[data-site]',
    '[data-page-view="website-redis"] select[data-site]',
    '[data-page-view="website-traffic"] select[data-site]',
    "[data-website-picker] select[data-website]",
    '[data-page-view="website-node"] select[name="siteID"]',
    '[data-page-view="website-python"] select[name="siteID"]',
    '[data-page-view="website-ruby"] select[name="siteID"]',
  ];

  document.querySelectorAll(selectors.join(",")).forEach((select, index) => {
    const label = select.closest("label");
    if (!label) return;
    const group = document.createElement("div");
    group.className = "website-picker-search";
    group.innerHTML = `<span class="website-picker-or" aria-hidden="true">OR</span>
      <div class="website-picker-search-field">
        <label class="field"><span>Search website</span><input type="search" placeholder="Search domains" autocomplete="off" aria-controls="website-picker-results-${index}"></label>
        <div class="website-picker-results" id="website-picker-results-${index}" hidden></div>
      </div>`;
    label.before(group);
    group.prepend(label);

    const search = group.querySelector("input");
    const results = group.querySelector(".website-picker-results");
    const choices = () => Array.from(select.options).filter(option =>
      option.value && option.value !== "server-explorer" && !option.disabled && !option.hidden &&
      !option.closest("optgroup[disabled]"));
    const matches = () => {
      const query = search.value.trim().toLowerCase();
      return query ? choices().filter(option => option.label.toLowerCase().includes(query)) : [];
    };
    const close = () => { results.hidden = true; };
    const choose = option => {
      if (search.disabled || select.disabled || !choices().includes(option)) return;
      search.value = option.label;
      close();
      if (select.value !== option.value) {
        select.value = option.value;
        select.dispatchEvent(new Event("change", { bubbles: true }));
      }
    };
    const show = () => {
      if (search.disabled || !search.value.trim()) {
        close();
        return;
      }
      const buttons = matches().slice(0, 20).map(option => {
        const button = document.createElement("button");
        button.type = "button";
        button.textContent = option.label;
        button.addEventListener("click", () => {
          choose(option);
          search.focus();
          close();
        });
        return button;
      });
      results.replaceChildren(...buttons);
      if (!buttons.length) results.textContent = "No matching websites";
      results.hidden = false;
    };

    search.addEventListener("input", show);
    search.addEventListener("keydown", event => {
      if (event.key === "Escape" && !results.hidden) {
        event.preventDefault();
        close();
      } else if (event.key === "ArrowDown" && !results.hidden && results.firstElementChild) {
        event.preventDefault();
        results.firstElementChild.focus();
      } else if (event.key === "Enter") {
        event.preventDefault();
        const options = matches();
        const exact = options.find(option => option.label.toLowerCase() === search.value.trim().toLowerCase());
        if (exact || options.length === 1) choose(exact || options[0]);
        else show();
      }
    });
    results.addEventListener("keydown", event => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      close();
      search.focus();
    });
    group.addEventListener("focusout", event => {
      if (!group.contains(event.relatedTarget)) close();
    });
    select.addEventListener("change", () => {
      if (search.value) search.value = choices().find(option => option.selected)?.label || "";
      close();
    });

    const sync = () => {
      const explorer = select.value === "server-explorer";
      group.classList.toggle("website-picker-search-unavailable", explorer);
      search.disabled = explorer || select.disabled || !choices().length;
      if (!results.hidden) show();
    };
    new MutationObserver(sync).observe(select, {
      subtree: true, childList: true, attributes: true, attributeFilter: ["disabled", "hidden"],
    });
    sync();
  });
})();
