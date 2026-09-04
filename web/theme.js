(() => {
  let preference = "system";
  try { preference = localStorage.getItem("litecp-theme") || "system"; } catch { /* storage may be unavailable */ }
  const dark = preference === "dark" || (preference === "system" && matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.dataset.theme = dark ? "dark" : "light";
})();
