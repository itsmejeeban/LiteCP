(() => {
  "use strict";
  const cp = window.LiteCP;
  const card = document.querySelector("#panelRepairPanel");
  const app = document.querySelector("#appView");
  if (!card || !app) return;

  const repair = card.querySelector("#panelRepairRun");
  const check = card.querySelector("#panelRepairCheck");
  const refresh = card.querySelector("#panelRepairRefresh");
  const status = card.querySelector("#panelRepairStatus");
  const progress = card.querySelector("#panelRepairProgress");
  const time = card.querySelector("#panelRepairTime");
  const details = card.querySelector("#panelRepairDetails");
  const output = card.querySelector("#panelRepairOutput");
  const apiPath = "/api/panel-repair";

  let lastRun = null;
  let busy = false;
  let visible = false;
  let sessionRevision = 0;
  let timer;

  function syncControls() {
    const running = lastRun?.status === "running";
    repair.disabled = check.disabled = cp.demo || busy || !lastRun || running;
    refresh.disabled = cp.demo || busy;
    progress.hidden = !running;
  }

  function render(run) {
    lastRun = run;
    switch (run.status) {
      case "running":
        status.textContent = run.mode === "repair" ? "Checking and repairing the panel…" : "Checking panel health…";
        break;
      case "healthy":
        status.textContent = "Panel health checks passed.";
        break;
      case "issues":
        status.textContent = "Some checks need attention. Review the diagnostic report.";
        break;
      case "failed":
        status.textContent = "The diagnostic run could not finish. Review the report and try again.";
        break;
      default:
        status.textContent = "Ready to check panel health.";
    }
    const timestamp = run.finishedAt || run.startedAt;
    time.hidden = !Number.isFinite(Date.parse(timestamp));
    time.textContent = time.hidden ? "" : (run.finishedAt ? "Finished " : "Started ") + cp.formatDateTime(timestamp);
    output.textContent = run.output || "";
    details.hidden = !output.textContent;
    syncControls();
  }

  function schedule(delay) {
    clearTimeout(timer);
    if (visible && !document.hidden && lastRun?.status === "running") {
      timer = setTimeout(() => request(), delay);
    }
  }

  async function request(mode) {
    if (!visible || busy || cp.demo) return;
    if (mode && (!lastRun || lastRun.status === "running")) return;
    clearTimeout(timer);
    const requestSession = sessionRevision;
    busy = true;
    syncControls();
    if (mode) status.textContent = "Starting panel health checks…";
    let delay = 2000;
    let refreshAfterConflict = false;
    try {
      const run = mode ? await cp.api(apiPath, {method: "POST", body: {mode}}) : await cp.api(apiPath);
      if (requestSession === sessionRevision) render(run);
    } catch (error) {
      if (requestSession !== sessionRevision) return;
      if (error.status === 409 && mode) {
        // A run may have started from another browser after our last check.
        refreshAfterConflict = true;
      } else {
        if (error.status === 401 || error.status === 403) {
          lastRun = null;
          output.textContent = "";
          details.hidden = time.hidden = true;
        }
        status.textContent = error.message;
        delay = 6000;
      }
    } finally {
      if (requestSession === sessionRevision) {
        busy = false;
        syncControls();
        schedule(delay);
      }
    }
    if (refreshAfterConflict) return request();
  }

  repair.addEventListener("click", () => request("repair"));
  check.addEventListener("click", () => request("check"));
  refresh.addEventListener("click", () => request());

  function activate() {
    if (app.classList.contains("hidden")) {
      sessionRevision++;
      visible = busy = false;
      clearTimeout(timer);
      render({status: "idle"});
      lastRun = null;
      syncControls();
      return;
    }
    const next = location.hash === "#settings/repair";
    if (next === visible) return;
    visible = next;
    clearTimeout(timer);
    if (!visible) return;
    if (cp.demo) {
      status.textContent = "Panel repair is available on installed LiteCP servers.";
      return;
    }
    if (!lastRun) status.textContent = "Loading panel repair status…";
    request();
  }

  const navigate = window.LiteCPFeatures.navigate;
  window.LiteCPFeatures.navigate = id => { navigate?.(id); activate(); };
  window.addEventListener("hashchange", activate);
  document.addEventListener("visibilitychange", () => {
    clearTimeout(timer);
    if (!document.hidden) request();
  });
  new MutationObserver(activate).observe(app, {attributes: true, attributeFilter: ["class"]});
  activate();
})();
