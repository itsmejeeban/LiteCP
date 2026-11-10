(() => {
  "use strict";
  const cp = window.LiteCP;
  const card = document.querySelector("#updateRestorePointsPanel");
  if (!card) return;

  const apiPath = "/api/updates/restore-points";
  const app = document.querySelector("#appView");
  const form = card.querySelector("#updateRestoreRetentionForm");
  const limit = form.elements.limit;
  const save = form.querySelector("button[type=submit]");
  const refresh = card.querySelector("#refreshUpdateRestorePoints");
  const status = card.querySelector("#updateRestorePointsStatus");
  const list = card.querySelector("#updateRestorePointsList");
  let busy = false;
  let savedLimit = null;

  function render(data) {
    savedLimit = data.limit;
    limit.value = savedLimit;
    if (data.points.length === 0) {
      list.innerHTML = "<p>No restore points saved.</p>";
      return;
    }
    const escape = cp.escapeHTML;
    const rows = data.points.map(point => {
      const id = escape(point.id);
      const version = point.version ? "LiteCP " + escape(point.version) : "Unknown version";
      const date = Number.isFinite(Date.parse(point.savedAt)) ? escape(cp.formatDateTime(point.savedAt)) : "Unknown";
      const size = Number.isFinite(point.sizeBytes) ? escape(cp.formatBytes(point.sizeBytes)) : "Unavailable";
      const state = point.state === "verified" ? "Verified update" : "Earlier or unsuccessful update";
      return `<tr><td><strong>${version}</strong><small>${id}</small><small>${state}</small></td>
        <td>${date}</td><td>${size}</td><td><button type="button" class="button button-danger" data-delete-point="${id}" aria-label="Delete restore point ${id}">Delete</button></td></tr>`;
    }).join("");
    list.innerHTML = `<div class="table-scroll" tabindex="0" aria-label="LiteCP restore points">
      <table class="data-table"><thead><tr><th scope="col">Restore point</th><th scope="col">Saved</th><th scope="col">Size</th><th scope="col">Action</th></tr></thead><tbody>${rows}</tbody></table></div>`;
  }

  function syncControls() {
    limit.disabled = save.disabled = busy || savedLimit === null;
    refresh.disabled = busy;
    for (const button of list.querySelectorAll("[data-delete-point]")) button.disabled = busy;
  }

  async function work(action) {
    if (busy) return;
    busy = true;
    syncControls();
    try {
      await action();
    } catch (error) {
      status.textContent = error.message;
    } finally {
      busy = false;
      syncControls();
    }
  }

  function load() {
    if (cp.demo) {
      refresh.disabled = true;
      status.textContent = "Restore points are available on installed LiteCP servers.";
      return;
    }
    return work(async () => {
      status.textContent = "Loading restore points…";
      const data = await cp.api(apiPath);
      render(data);
      status.textContent = data.warning || "";
    });
  }

  refresh.addEventListener("click", load);
  form.addEventListener("submit", event => {
    event.preventDefault();
    const next = limit.valueAsNumber;
    if (savedLimit === null || !Number.isInteger(next) || next < 0 || next > 10) return;
    work(async () => {
      if (next < savedLimit && !await window.LiteCPDialogs.confirm(
        "Saving a lower limit can permanently delete the oldest verified restore points.",
        {title: "Lower restore point limit", confirmText: "Save limit", danger: true}
      )) return;
      status.textContent = "Saving restore point limit…";
      const data = await cp.api(apiPath, {method: "PUT", body: {limit: next}});
      render(data);
      status.textContent = data.warning || "Restore point limit saved.";
    });
  });

  list.addEventListener("click", event => {
    const button = event.target.closest("[data-delete-point]");
    if (!button) return;
    const id = button.dataset.deletePoint;
    work(async () => {
      if (!await window.LiteCPDialogs.confirm(
        `Permanently delete restore point ${id} and its recovery files?`,
        {title: "Delete restore point", confirmText: "Delete restore point", danger: true}
      )) return;
      status.textContent = "Deleting restore point…";
      const data = await cp.api(apiPath + "/" + encodeURIComponent(id), {method: "DELETE"});
      render(data);
      status.textContent = data.warning || "Restore point deleted.";
    });
  });

  let wasVisible = false;
  function activate(force = false) {
    const visible = location.hash === "#settings/update" && !app.classList.contains("hidden");
    if (visible && (!wasVisible || force)) load();
    wasVisible = visible;
  }
  const navigate = window.LiteCPFeatures.navigate;
  window.LiteCPFeatures.navigate = id => { navigate?.(id); activate(); };
  window.addEventListener("hashchange", () => activate());
  window.addEventListener("litecp:update-settled", () => activate(true));
  new MutationObserver(() => activate()).observe(app, {attributes: true, attributeFilter: ["class"]});
  activate();
})();
