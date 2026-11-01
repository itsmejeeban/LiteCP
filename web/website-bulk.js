(() => {
  "use strict";
  const cp = window.LiteCP, table = document.getElementById("sitesTable");
  const selected = new Set();
  let busy = false, results = [];
  const toolbar = document.createElement("section");
  toolbar.className = "website-bulk";
  toolbar.setAttribute("aria-label", "Bulk website actions");
  toolbar.innerHTML = '<div class="website-bulk-controls"><strong data-bulk-count>0 selected</strong><button type="button" class="button button-secondary" data-bulk-clear>Clear selection</button><span class="website-bulk-actions"><button type="button" class="button button-secondary" data-bulk-action="suspend">Suspend</button><button type="button" class="button button-secondary" data-bulk-action="unsuspend">Unsuspend</button><button type="button" class="button button-primary" data-bulk-action="ssl">Issue SSL</button><button type="button" class="button button-danger danger" data-bulk-action="remove">Remove</button></span></div><p class="website-bulk-hint">Select websites on any page. Issue SSL skips websites that already have SSL.</p><p data-bulk-progress role="status" aria-live="polite"></p><details data-bulk-results hidden><summary>Action results</summary><div class="website-bulk-results"></div></details>';
  table.before(toolbar);
  const find = selector => toolbar.querySelector(selector);
  const checks = () => [...table.querySelectorAll("[data-site-select]")];

  function render() {
    const sites = cp.state()?.sites || [];
    const valid = new Set(sites.map(site => site.id));
    for (const id of selected) if (!valid.has(id)) selected.delete(id);
    const visible = checks();
    for (const input of visible) input.checked = selected.has(input.dataset.siteSelect);
    const all = table.querySelector("[data-sites-select-page]");
    if (all) {
      const count = visible.filter(input => input.checked).length;
      all.checked = visible.length > 0 && count === visible.length;
      all.indeterminate = count > 0 && count < visible.length;
    }
    table.inert = busy;
    find("[data-bulk-count]").textContent = `${selected.size} selected`;
    toolbar.querySelectorAll("button").forEach(button => { button.disabled = busy || !selected.size; });
  }

  function renderResults() {
    const details = find("[data-bulk-results]");
    details.hidden = results.length === 0;
    find(".website-bulk-results").innerHTML = results.map(result => `<div class="website-bulk-result"><strong>${cp.escapeHTML(result.domain)}</strong><span class="website-bulk-${result.status}">${cp.escapeHTML(result.message)}</span></div>`).join("");
  }

  table.addEventListener("change", event => {
    if (busy) return;
    const input = event.target;
    if (input.matches("[data-site-select]")) {
      if (input.checked) selected.add(input.dataset.siteSelect);
      else selected.delete(input.dataset.siteSelect);
    } else if (input.matches("[data-sites-select-page]")) {
      checks().forEach(item => {
        if (input.checked) selected.add(item.dataset.siteSelect);
        else selected.delete(item.dataset.siteSelect);
      });
    } else return;
    render();
  });
  find("[data-bulk-clear]").addEventListener("click", () => { if (!busy) { selected.clear(); render(); } });

  toolbar.addEventListener("click", async event => {
    const button = event.target.closest("[data-bulk-action]");
    if (!button || busy || !selected.size) return;
    const action = button.dataset.bulkAction;
    if (!["suspend", "unsuspend", "ssl", "remove"].includes(action)) return;
    // Freeze both IDs and domains before confirmation; no later selection is included.
    const sites = (cp.state()?.sites || []).filter(site => selected.has(site.id)).map(site => ({...site}));
    if (!sites.length) return;
    busy = true;
    render();
    const progress = find("[data-bulk-progress]");
    try {
      const domains = sites.map(site => site.domain).join("\n");
      let email;
      if (action === "remove") {
        const match = `REMOVE ${sites.length} ${sites.length === 1 ? "WEBSITE" : "WEBSITES"}`;
        const answer = await window.LiteCPDialogs.prompt(`Permanently remove these ${sites.length} websites?\n\n${domains}\n\nThis deletes their website files, Trash, linked managed databases and FTP accounts. Local and remote backups and mailboxes are kept. This cannot be undone.`, "", {title:"Remove selected websites", label:`Type ${match} to confirm`, match, required:true, danger:true, confirmText:"Remove permanently"});
        if (answer !== match) return;
      } else if (action === "ssl") {
        const missing = sites.filter(site => !site.ssl);
        if (missing.length) {
          email = await window.LiteCPDialogs.prompt(`Issue SSL for ${missing.length} websites:\n\n${missing.map(site => site.domain).join("\n")}\n\nPoint these domains to this server and allow HTTP on port 80. Certificates are requested one at a time.`, cp.state()?.settings?.email || "", {title:"Issue SSL for selected websites",label:"Renewal email address",type:"email",required:true,confirmText:"Issue SSL"});
          if (!email) return;
        }
      } else if (!await window.LiteCPDialogs.confirm(`${action === "suspend" ? "Suspend" : "Unsuspend"} web access for these websites?\n\n${domains}\n\nFiles, databases, mailboxes and FTP accounts are preserved.`, {title:`${action === "suspend" ? "Suspend" : "Unsuspend"} selected websites`,confirmText:action === "suspend" ? "Suspend" : "Unsuspend"})) return;

      results = [];
      renderResults();
      for (const [index, site] of sites.entries()) {
        progress.textContent = `${index + 1} of ${sites.length}: ${site.domain}`;
        try {
          if ((action === "ssl" && site.ssl) || (action === "suspend" && site.suspended) || (action === "unsuspend" && !site.suspended)) {
            results.push({domain:site.domain,status:"skip",message:action === "ssl" ? "Skipped: SSL already installed" : action === "suspend" ? "Already suspended" : "Already active"});
          } else {
            if (action === "remove") await cp.api(`/api/sites/${encodeURIComponent(site.id)}?deleteData=true`, {method:"DELETE",body:{domain:site.domain}});
            else if (action === "ssl") await cp.api("/api/sites/ssl", {method:"POST",body:{id:site.id,email}});
            else await cp.api("/api/sites/suspension", {method:"PUT",body:{id:site.id,suspended:action === "suspend"}});
            results.push({domain:site.domain,status:"success",message:{remove:"Removed",ssl:"SSL issued",suspend:"Suspended",unsuspend:"Unsuspended"}[action]});
          }
          selected.delete(site.id);
        } catch (error) {
          results.push({domain:site.domain,status:"error",message:error.message || "Action failed"});
          // An expired administrator session must not trigger more queued requests.
          if (error.status === 401 || error.status === 403) {
            for (const pending of sites.slice(index + 1)) results.push({domain:pending.domain,status:"error",message:"Not run: sign in and retry"});
            break;
          }
        }
        renderResults();
      }
      renderResults();
      find("[data-bulk-results]").open = true;
      const failed = results.filter(result => result.status === "error").length;
      progress.textContent = `Finished: ${results.filter(result => result.status === "success").length} succeeded, ${results.filter(result => result.status === "skip").length} skipped, ${failed} failed. ${failed ? "Failed websites remain selected for retry." : ""}`;
      try { await cp.refresh(); }
      catch (error) { progress.textContent += ` Could not refresh the website list: ${error.message}. Refresh before another action.`; }
    } catch (error) {
      progress.textContent = error.message || "Could not start the bulk action.";
      cp.handleError(error);
    } finally {
      busy = false;
      render();
    }
  });
  window.LiteCPWebsiteBulk = {render};
  render();
})();
