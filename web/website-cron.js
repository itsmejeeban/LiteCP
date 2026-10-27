(() => {
  "use strict";
  const cp = window.LiteCP, esc = cp.escapeHTML;
  const presets = {
    minute: "* * * * *", five: "*/5 * * * *", fifteen: "*/15 * * * *",
    thirty: "*/30 * * * *", hourly: "0 * * * *", daily: "0 0 * * *",
    weekly: "0 0 * * 0", monthly: "0 0 1 * *"
  };
  const fields = ["minute", "hour", "day", "month", "weekday"];
  const limits = [[0, 59], [0, 23], [1, 31], [1, 12], [0, 7]];
  let siteID = "", generation = 0, listRequest = 0, busy = false, ready = false, editing = "", items = [];
  const dialog = document.createElement("dialog");
  dialog.className = "fm-dialog website-cron-dialog";
  dialog.setAttribute("aria-labelledby", "websiteCronTitle");
  document.body.append(dialog);
  const find = selector => dialog.querySelector(selector);
  const context = () => ({ siteID, generation });
  const current = active => active.siteID === siteID && active.generation === generation && dialog.open;
  const status = message => { find("[data-cron-status]").textContent = message; };
  const endpoint = active => "/api/website-cron?siteId=" + encodeURIComponent(active.siteID);
  const form = () => find("form");
  const schedule = () => fields.map(name => form().elements[name].value.trim()).join(" ");

  function setControls() {
    dialog.querySelectorAll("button,input,select,textarea").forEach(control => { control.disabled = busy || !ready; });
    find("[data-cron-close]").disabled = busy;
    find("[data-cron-refresh]").disabled = busy;
  }
  function updatePreview() { find("[data-cron-expression]").textContent = schedule(); find("[data-cron-custom]").hidden = form().elements.preset.value !== "custom"; }
  function setSchedule(value) {
    const parts = value.trim().split(/\s+/);
    fields.forEach((name, index) => { form().elements[name].value = parts[index] || "*"; });
    form().elements.preset.value = Object.keys(presets).find(key => presets[key] === value) || "custom";
    updatePreview();
  }
  function resetForm() {
    editing = "";
    form().elements.name.value = "";
    form().elements.command.value = "";
    form().elements.enabled.checked = true;
    setSchedule(presets.five);
    find("[data-cron-save]").textContent = "Create cron job";
    find("[data-cron-cancel-edit]").hidden = true;
  }
  function validPart(value, minimum, maximum) {
    if (!value || !/^[\d*,/\-]+$/.test(value)) return false;
    return value.split(",").every(segment => {
      const pieces = segment.split("/");
      if (pieces.length > 2 || (pieces.length === 2 && (!/^\d+$/.test(pieces[1]) || Number(pieces[1]) < 1 || Number(pieces[1]) > maximum - minimum + 1))) return false;
      if (pieces[0] === "*") return true;
      const bounds = pieces[0].split("-");
      if (bounds.length > 2 || (pieces.length === 2 && bounds.length !== 2) || bounds.some(bound => !/^\d+$/.test(bound) || Number(bound) < minimum || Number(bound) > maximum)) return false;
      return bounds.length < 2 || Number(bounds[0]) <= Number(bounds[1]);
    });
  }
  function renderList() {
    find("[data-cron-list]").innerHTML = items.length ? items.map(item => `
      <article class="website-cron-job"><div class="website-cron-job-head"><strong>${esc(item.name || "Scheduled command")}</strong><span class="badge ${item.enabled ? "success" : ""}">${item.enabled ? "Enabled" : "Disabled"}</span></div>
      <code class="website-cron-schedule">${esc(item.schedule)}</code><pre>${esc(item.command)}</pre>
      <div class="feature-actions"><button type="button" class="button button-primary" data-cron-run="${esc(item.id)}">Run test</button><button type="button" class="button button-secondary" data-cron-edit="${esc(item.id)}">Edit</button><button type="button" class="button button-secondary" data-cron-toggle="${esc(item.id)}">${item.enabled ? "Disable" : "Enable"}</button><button type="button" class="button button-ghost danger" data-cron-remove="${esc(item.id)}">Remove</button></div></article>`).join("") : '<div class="website-cron-empty">No custom cron jobs yet. Choose a schedule and command to create one.</div>';
  }
  async function load(active) {
    if (!current(active)) return;
    if (cp.demo) throw new Error("Custom cron scheduling requires the installed Linux server.");
    const request = ++listRequest;
    let result;
    try { result = await cp.api(endpoint(active)); }
    catch (error) { if (current(active) && request === listRequest) throw error; return; }
    if (!current(active) || request !== listRequest) return;
    items = result.items;
    find("[data-cron-domain]").textContent = result.domain;
    find("[data-cron-context]").textContent = `Time zone: ${result.timezone}`;
    find("[data-cron-php]").textContent = result.phpCommand || "PHP CLI is not available for this runtime.";
    ready = true;
    renderList();
    setControls();
  }
  async function act(method, body, message, reset) {
    const active = context();
    if (busy || !ready || !current(active)) return;
    busy = true; setControls(); status("Saving cron jobs…");
    try {
      await cp.api(endpoint(active), { method, body });
      if (!current(active)) return;
      if (reset) resetForm();
      await load(active);
      if (current(active)) status(message);
    } catch (error) { if (current(active)) status(error.message); }
    finally { if (current(active)) { busy = false; setControls(); } }
  }

  document.querySelector("#sitesTable").addEventListener("click", async event => {
    const button = event.target.closest("[data-site-cron-jobs]");
    if (!button) return;
    siteID = button.dataset.siteCronJobs; generation++; busy = false; ready = false; items = [];
    const active = context();
    dialog.innerHTML = `<header><div><h2 id="websiteCronTitle">Website cron jobs</h2><p data-cron-domain></p></div><button type="button" class="website-cron-close" data-cron-close aria-label="Close cron jobs"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M6 6l12 12M18 6 6 18"/></svg></button></header>
      <div class="fm-dialog-body"><p data-cron-context>Loading website schedule…</p>
      <form novalidate><div class="website-cron-form-head"><h3 data-cron-form-title>Add a cron job</h3><label class="website-cron-enabled"><input name="enabled" type="checkbox" checked> Enabled</label></div>

      <label class="field"><span>Run this job</span><select name="preset"><option value="minute">Every minute</option><option value="five" selected>Every 5 minutes</option><option value="fifteen">Every 15 minutes</option><option value="thirty">Every 30 minutes</option><option value="hourly">Once an hour</option><option value="daily">Once a day at midnight</option><option value="weekly">Once a week on Sunday</option><option value="monthly">Once a month</option><option value="custom">Custom schedule</option></select></label>
      <div data-cron-custom hidden><div class="website-cron-fields">${fields.map((name, index) => `<label class="field"><span>${["Minute", "Hour", "Day", "Month", "Weekday"][index]}</span><input name="${name}" data-cron-field autocomplete="off" spellcheck="false" maxlength="128" required aria-describedby="websiteCronSyntax"><small>${["0–59", "0–23", "1–31", "1–12", "0–7; Sun = 0 or 7"][index]}</small></label>`).join("")}</div>
      <p class="website-cron-help" id="websiteCronSyntax">Use * for every value, */5 for every five, 1,15 for a list, or 1-5 for a range. When both day and weekday are restricted, either match runs the job.</p><p class="website-cron-preview">Schedule: <code data-cron-expression></code></p></div>
      <label class="field"><span>Command</span><textarea name="command" rows="2" maxlength="4096" spellcheck="false" required placeholder="Example: /usr/bin/php /path/to/script.php"></textarea><small>Paste the command provided by your application.</small></label>
      <details class="website-cron-options"><summary>More options</summary><label class="field"><span>Job name (optional)</span><input name="name" maxlength="100" placeholder="Daily report"></label><p class="website-cron-php">PHP command: <code data-cron-php></code></p></details>
      <div class="feature-actions"><button type="submit" class="button button-primary" data-cron-save>Create cron job</button><button type="button" class="button button-secondary" data-cron-cancel-edit hidden>Cancel edit</button></div></form>
      <p data-cron-status role="status" aria-live="polite"></p><div class="website-cron-list-head"><h3>Custom cron jobs</h3><button type="button" class="button button-secondary" data-cron-refresh>Refresh</button></div><div data-cron-list></div><section data-cron-result hidden><h3>Test result</h3><p data-cron-result-message role="status"></p><pre data-cron-output></pre></section></div>`;
    resetForm(); dialog.showModal(); setControls();
    try { await load(active); }
    catch (error) { if (current(active)) status(error.message); }
  });
  dialog.addEventListener("cancel", event => { if (busy) event.preventDefault(); });
  dialog.addEventListener("keydown", event => event.stopPropagation());
  dialog.addEventListener("change", event => {
    if (event.target === form().elements.preset) { if (presets[event.target.value]) setSchedule(presets[event.target.value]); else updatePreview(); }
  });
  dialog.addEventListener("input", event => {
    if (event.target.matches("[data-cron-field]")) { form().elements.preset.value = "custom"; updatePreview(); }
  });
  dialog.addEventListener("submit", async event => {
    event.preventDefault();
    if (busy || !ready) return;
    const values = form().elements, rawCommand = values.command.value, command = rawCommand.trim(), name = values.name.value.trim();
    for (let index = 0; index < fields.length; index++) {
      const control = values[fields[index]];
      if (!validPart(control.value.trim(), ...limits[index])) { status(`Enter a valid ${fields[index]} value (${limits[index].join("–")}).`); control.focus(); return; }
    }
    if (!command || rawCommand.length > 4096 || /[\r\n\x00]/.test(rawCommand)) { status("Enter one command line of up to 4096 characters. Use && to join commands."); values.command.focus(); return; }
    if (name.length > 100) { status("Use a job name of up to 100 characters."); values.name.focus(); return; }
    await act(editing ? "PUT" : "POST", { ...(editing ? { id: editing } : {}), name, schedule: schedule(), command, enabled: values.enabled.checked }, editing ? "Cron job updated." : "Cron job created.", true);
  });
  dialog.addEventListener("click", async event => {
    if (busy) return;
    if (event.target.closest("[data-cron-close]")) { dialog.close(); return; }
    if (event.target.closest("[data-cron-refresh]")) { const active = context(); status("Loading cron jobs…"); try { await load(active); if (current(active)) status(""); } catch (error) { if (current(active)) status(error.message); } return; }
    if (!ready) return;
    if (event.target.closest("[data-cron-cancel-edit]")) { resetForm(); status(""); return; }
    const run = event.target.closest("[data-cron-run]");
    if (run) {
      const active = context(), job = items.find(value => value.id === run.dataset.cronRun);
      if (!job) return;
      if (!await window.LiteCPDialogs.confirm("Run this command now? It can change website data or send messages, just like its scheduled run.", {title:"Test cron job",confirmText:"Run test"}) || !current(active) || busy) return;
      busy=true;setControls();status("Running test (up to 30 seconds)…");
      try {
        const result=await cp.api('/api/website-cron/run?siteId='+encodeURIComponent(active.siteID),{method:'POST',body:{id:job.id}});
        if(!current(active))return;
        find('[data-cron-result]').hidden=false;
        find('[data-cron-result-message]').textContent=result.message+` (${(result.durationMs/1000).toFixed(1)} seconds)`;
        find('[data-cron-output]').textContent=result.output||'No output returned.';
        status(result.success?'Test completed successfully.':'Test failed. Check the output below.');
      }catch(error){if(current(active))status(error.message);}
      finally{if(current(active)){busy=false;setControls();}}
      return;
    }
    const edit = event.target.closest("[data-cron-edit]"), toggle = event.target.closest("[data-cron-toggle]"), remove = event.target.closest("[data-cron-remove]");
    const item = items.find(value => value.id === (edit?.dataset.cronEdit || toggle?.dataset.cronToggle || remove?.dataset.cronRemove));
    if (!item) return;
    if (edit) {
      editing = item.id; form().elements.name.value = item.name || ""; form().elements.command.value = item.command; form().elements.enabled.checked = item.enabled;
      setSchedule(item.schedule); find("[data-cron-save]").textContent = "Save changes"; find("[data-cron-cancel-edit]").hidden = false; status(""); form().elements.command.focus();
    } else if (toggle) {
      await act("PUT", { id: item.id, name: item.name, schedule: item.schedule, command: item.command, enabled: !item.enabled }, item.enabled ? "Cron job disabled." : "Cron job enabled.", false);
    } else if (remove) {
      const active = context();
      if (await window.LiteCPDialogs.confirm(`Remove the cron job “${item.name || item.schedule}”?\n\nIts schedule will be removed. Website files and databases will be kept.`, { title: "Remove cron job", confirmText: "Remove cron job", danger: true }) && current(active)) await act("DELETE", { id: item.id, confirm: true }, "Cron job removed.", editing === item.id);
    }
  });
  const previous = window.LiteCPFeatures.render;
  window.LiteCPFeatures.render = state => {
    previous(state);
    document.querySelectorAll('#sitesTable [data-site-action="delete"]').forEach(button => {
      if (button.parentElement.querySelector("[data-site-cron-jobs]")) return;
      const cron = document.createElement("button");
      cron.type = "button"; cron.dataset.siteCronJobs = button.dataset.id; cron.title = "Cron jobs";
      cron.setAttribute("aria-label", "Manage cron jobs for " + (state.sites.find(site => site.id === button.dataset.id)?.domain || "website"));
      cron.innerHTML = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20.5 10a8.5 8.5 0 1 1-5-6M12 7v5l3 2M19 2v6m-3-3h6"/></svg><span class="site-action-label">Cron jobs</span>';
      button.parentElement.prepend(cron);
    });
  };
  if (cp.state()) window.LiteCPFeatures.render(cp.state());
})();
