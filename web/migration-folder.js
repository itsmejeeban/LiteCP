(() => {
  "use strict";
  const cp = window.LiteCP;
  const page = document.querySelector('[data-page-view="migrations"]');
  const panel = document.createElement("article");
  panel.className = "panel compact-form-card";
  panel.hidden = true;
  panel.innerHTML = `
    <div class="panel-head"><div>
      <h2>1. Find and inspect a backup</h2>
      <p>Scan a folder on the LiteCP server where you stored your backups.</p>
    </div></div>
    <div class="settings-body">
      <form data-folder-scan>
        <label class="field"><span>Backup type</span>
          <select name="platform" aria-describedby="migrationFolderFormat">
            <option value="cpanel">cPanel account backup</option>
            <option value="plesk">Plesk website export</option>
            <option value="litecp">LiteCP / LiteSpeed backup</option>
          </select>
          <small id="migrationFolderFormat"></small>
        </label>
        <label class="field"><span>Backup folder location</span>
          <input name="folder" type="text" placeholder="/var/backups/migration" autocomplete="off" spellcheck="false" required>
          <small>Enter an absolute server path without symbolic links. The scan checks ZIP and TAR archives directly in this folder. Choose an archive up to 8 GiB.</small>
        </label>
        <button type="submit" class="button button-primary" data-folder-scan-button>Scan folder</button>
      </form>
      <p role="status" data-folder-status></p>
      <div data-folder-results hidden>
        <div class="table-scroll">
          <table class="data-table" aria-label="Backup files in the selected folder">
            <thead><tr><th scope="col">Select</th><th scope="col">Backup file</th><th scope="col">Size</th><th scope="col">Last modified</th></tr></thead>
            <tbody data-folder-files></tbody>
          </table>
        </div>
        <p class="feature-note">Inspection verifies the archive format. The review below imports website files and an optional SQL dump into an empty target. The source backup is kept.</p>
        <button type="button" class="button button-primary" data-folder-inspect disabled>Inspect and restore</button>
      </div>
    </div>`;
  page.querySelector("#migrationUploadForm").before(panel);

  const get = selector => panel.querySelector(selector);
  const form = get("[data-folder-scan]");
  const scanButton = get("[data-folder-scan-button]");
  const status = get("[data-folder-status]");
  const results = get("[data-folder-results]");
  const rows = get("[data-folder-files]");
  const inspectButton = get("[data-folder-inspect]");
  const importForm = page.querySelector("#migrationImportForm");
  const output = page.querySelector("#migrationOutput");
  const notes = {
    cpanel: "Select an account archive containing public_html.",
    plesk: "Use a portable website export containing httpdocs. Export the website from a native Plesk server backup before restoring it here.",
    litecp: "Use a LiteCP manual website ZIP or scheduled website .tar.gz backup."
  };
  let scanRequest = 0;
  let scanned = null;
  let scanning = false;
  let inspecting = false;

  function updateNote() {
    get("#migrationFolderFormat").textContent = notes[form.elements.platform.value];
  }

  function selectedItem() {
    const checked = rows.querySelector("input:checked");
    return checked ? scanned?.items[Number(checked.value)] : undefined;
  }

  function updateControls() {
    form.elements.platform.disabled = inspecting;
    form.elements.folder.disabled = inspecting;
    scanButton.disabled = scanning || inspecting;
    inspectButton.disabled = inspecting || !selectedItem();
    rows.querySelectorAll("input").forEach(input => input.disabled = inspecting);
  }

  function clearReview() {
    if (!page.dataset.folderReview) return;
    delete page.dataset.folderReview;
    importForm.classList.add("hidden");
    output.classList.add("migration-output-hidden");
  }

  function clearResults() {
    scanned = null;
    rows.replaceChildren();
    results.hidden = true;
    clearReview();
  }

  form.addEventListener("input", () => {
    scanRequest++;
    scanning = false;
    clearResults();
    updateNote();
    updateControls();
    status.textContent = "";
  });

  form.addEventListener("submit", async event => {
    event.preventDefault();
    if (scanning || inspecting) return;
    const platform = form.elements.platform.value;
    const folder = form.elements.folder.value.trim();
    if (!folder.startsWith("/")) {
      status.textContent = "Enter an absolute folder path such as /var/backups/migration.";
      return;
    }
    const request = ++scanRequest;
    clearResults();
    scanning = true;
    updateControls();
    status.textContent = "Scanning the folder…";
    try {
      if (cp.demo) throw new Error("Folder restore requires an installed LiteCP server.");
      const response = await cp.api("/api/migrations/folder/scan", {method: "POST", body: {platform, folder}});
      if (request !== scanRequest) return;
      const items = response?.items;
      if (!Array.isArray(items)) throw new Error("LiteCP returned an invalid archive list.");
      rows.innerHTML = items.map((item, index) => `<tr>
        <td><input type="radio" name="folderBackup" value="${index}" aria-label="Select ${cp.escapeHTML(item.name)}"></td>
        <td><strong>${cp.escapeHTML(item.name)}</strong></td>
        <td>${cp.formatBytes(item.size)}</td><td>${cp.escapeHTML(cp.formatDateTime(item.modifiedAt))}</td>
      </tr>`).join("");
      scanned = {platform, folder, items};
      results.hidden = !items.length;
      const count = items.length === 1 ? "1 archive found." : `${items.length} archives found.`;
      status.textContent = (items.length ? count : "No matching backup archives found.") +
        (response.truncated ? " Scan limit reached. Use a smaller folder to see additional files." : "");
    } catch (error) {
      if (request === scanRequest) status.textContent = `Folder scan failed. ${error.message}`;
    } finally {
      if (request === scanRequest) {
        scanning = false;
        updateControls();
      }
    }
  });

  rows.addEventListener("change", () => {
    clearReview();
    updateControls();
  });

  inspectButton.addEventListener("click", async () => {
    const item = selectedItem();
    if (!item || inspecting) return;
    const {platform, folder} = scanned;
    const {name, size, modifiedAt} = item;
    clearReview();
    inspecting = true;
    updateControls();
    status.textContent = `Inspecting ${name}…`;
    try {
      const preview = await cp.api("/api/migrations/folder/restore", {method: "POST", body: {platform, folder, name, size, modifiedAt}});
      if (!preview?.id || !Array.isArray(preview.roots) || !Array.isArray(preview.databases)) {
        throw new Error("LiteCP returned an invalid archive preview.");
      }
      if (location.hash !== "#migrations/folder") {
        status.textContent = `${name} staged. Open Upload & Restore Backup and choose Show staged archives to continue.`;
        return;
      }
      status.textContent = `${name} is ready for review below.`;
      page.dataset.folderReview = "true";
      window.LiteCPFeatures.reviewMigration(preview);
      output.textContent = `Archive staged from ${name}. Review the website root and choose an empty target before restoring.`;
      document.dispatchEvent(new Event("migration-show-review"));
      importForm.scrollIntoView({block: "start"});
    } catch (error) {
      status.textContent = `Archive inspection failed. ${error.message}`;
    } finally {
      inspecting = false;
      updateControls();
    }
  });

  function updateVisibility() {
    panel.hidden = page.dataset.migrationSection !== "folder";
  }
  updateNote();
  updateControls();
  updateVisibility();
  document.addEventListener("migration-section-change", updateVisibility);
})();
