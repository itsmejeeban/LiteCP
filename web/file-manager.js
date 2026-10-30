/* Advanced file workspace; all mutations use LiteCP's confined server APIs. */
(() => {
  "use strict";
  function normalizePath(value, home = "/public") {
    const raw = String(value).trim(), base = home.replace(/\/+$/g, "");
    if (raw.startsWith("/") && raw !== base && !raw.startsWith(base + "/")) throw new Error("Choose a path inside this website home.");
    value = raw === base ? "" : raw.startsWith(base + "/") ? raw.slice(base.length + 1) : raw;
    const path = String(value).trim().replace(/^\/+|\/+$/g, "");
    if (/[\x00-\x1f\\]/.test(path) || path.split("/").includes("..")) throw new Error("Choose a path inside this website. Parent traversal is not allowed.");
    return home === "/public" ? (path === "public" ? "" : path.startsWith("public/") ? path.slice(7) : path) : path;
  }
  function kind(entry) {
    if (entry.type === "directory") return "Folder";
    const ext = entry.name.includes(".") ? entry.name.split(".").pop().toLowerCase() : "";
    if (["zip","gz","tar","tgz","bz2","tbz","tbz2"].includes(ext)) return "Archive";
    if (["png","jpg","jpeg","gif","svg","webp","ico","avif"].includes(ext)) return "Image";
    return ext ? ext.toUpperCase() + " file" : "File";
  }
  function editableFile(entry) {
    if(!entry || entry.type!=="file")return false;
    const ext=entry.name.split(".").pop().toLowerCase();
    return !["zip","gz","tgz","tar","bz2","tbz","tbz2","png","jpg","jpeg","gif","svg","webp","ico","avif","pdf","mp3","wav","ogg","flac","m4a","mp4","webm","mov","avi","woff","woff2","ttf","eot","exe","dll","so","bin","doc","docx","xls","xlsx"].includes(ext);
  }
  const htmlFile=entry=>editableFile(entry)&&/\.html?$/i.test(entry.name);
  const extractableFile=entry=>entry?.type==="file"&&/\.(zip|tar|tar\.gz|tgz|tar\.bz2|tbz2?)$/i.test(entry.name);
  function prepareEntries(entries, query, hidden, sort, descending) {
    return entries.filter(x => (hidden || !x.name.startsWith(".")) && x.name.toLowerCase().includes(query.toLowerCase())).sort((a,b) => {
      if (a.type !== b.type) return a.type === "directory" ? -1 : 1;
      let order = sort === "size" ? a.size-b.size : sort === "modified" ? new Date(a.modified)-new Date(b.modified) : sort === "mode" ? parseInt(a.mode,8)-parseInt(b.mode,8) : sort === "type" ? kind(a).localeCompare(kind(b)) : a.name.localeCompare(b.name, undefined, {numeric:true});
      return (order || a.name.localeCompare(b.name)) * (descending ? -1 : 1);
    });
  }
  if (typeof module !== "undefined" && module.exports) module.exports = {normalizePath,kind,prepareEntries,editableFile,htmlFile,extractableFile};
  if (typeof document === "undefined") return;
  const cp = window.LiteCP, $ = s => document.querySelector(s), esc = cp.escapeHTML;
  const page = $('[data-page-view="files"]'), panel = $(".file-manager-panel");
  page.querySelector(":scope > .info-callout")?.remove();
  const heading = page.querySelector(".page-heading");
  heading.querySelector(".eyebrow")?.remove();
  heading.querySelector("p")?.remove();
  const fullscreen = document.createElement("button");
  fullscreen.type = "button"; fullscreen.className = "fm-tool fm-fullscreen-toggle";
  fullscreen.textContent = "Full screen"; fullscreen.setAttribute("aria-pressed", "false");
  const setFullscreen = enabled => {
    page.classList.toggle("fm-fullscreen", enabled);
    document.body.classList.toggle("fm-fullscreen-open", enabled);
    fullscreen.innerHTML = `${icon(enabled ? "close" : "grid")}<span>${enabled ? "Exit full screen" : "Full screen"}</span>`;
    fullscreen.setAttribute("aria-pressed", String(enabled));
  };
  fullscreen.addEventListener("click", () => setFullscreen(!page.classList.contains("fm-fullscreen")));
  document.addEventListener("keydown", event => {
    if(event.key === "Escape" && page.classList.contains("fm-fullscreen") && !document.querySelector("dialog[open]")) setFullscreen(false);
  });
  window.addEventListener("hashchange", () => { if(location.hash !== "#files") setFullscreen(false); });
  const paths = {
    folder:'M3 7h7l2-3h9v16H3z', file:'M6 3h8l4 4v14H6z M14 3v5h5', upload:'M12 16V3m-5 5 5-5 5 5M4 15v6h16v-6',
    plus:'M12 5v14M5 12h14', copy:'M8 8h13v13H8zM16 8V3H3v13h5', move:'M12 3v18M3 12h18M8 7l4-4 4 4M8 17l4 4 4-4',
    edit:'m4 16 12-12 4 4L8 20H4zM13 7l4 4', lock:'M6 10h12v11H6zM8 10V7a4 4 0 0 1 8 0v3', download:'M12 3v14m-5-5 5 5 5-5M4 18v3h16v-3',
    archive:'M4 4h16v16H4zM10 4v3h4v3h-4v3h4v3h-4v4', extract:'M4 4h16v16H4zM12 7v10m-4-4 4 4 4-4', trash:'M4 6h16M9 6V3h6v3M6 6l1 15h10l1-15M10 10v7M14 10v7',
    refresh:'M20 9a8 8 0 1 0 0 7M20 3v6h-6', up:'m5 12 7-7 7 7M12 5v15', back:'m14 5-7 7 7 7', next:'m10 5 7 7-7 7', search:'M10 17a7 7 0 1 0 0-14 7 7 0 0 0 0 14zm5-2 6 6',
    list:'M8 5h13M8 12h13M8 19h13M3 5h1M3 12h1M3 19h1', grid:'M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z', more:'M5 12h.01M12 12h.01M19 12h.01', close:'m6 6 12 12M18 6 6 18', info:'M12 11v6M12 7h.01M21 12a9 9 0 1 0-18 0 9 9 0 0 0 18 0',
    home:'M3 11 12 3l9 8v10h-7v-6h-4v6H3z', eye:'M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12zm10 3a3 3 0 1 0 0-6 3 3 0 0 0 0 6',
    select:'M4 4h16v16H4zM8 12l3 3 6-7', unselect:'M5 5h14v14H5z', settings:'M12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6zm0-6 1 3 3 1 3-1 2 3-2 2v3l2 2-2 3-3-1-3 1-1 3h-4l-1-3-3-1-3 1-2-3 2-2v-3L3 9l2-3 3 1 3-1z'
  };
  // The same Font Awesome Free and Bootstrap glyphs used by the reference manager.
  const glyphs = {
    folder:["solid",0xf07b], "folder-open":["solid",0xf07c], globe:["solid",0xf0ac], file:["regular",0xf15b],
    plus:["glyph",0x002b], copy:["regular",0xf0c5], move:["glyph",0xe068],
    upload:["solid",0xf093], download:["solid",0xf019], trash:["glyph",0xe020],
    delete:["glyph",0xe014], restore:["solid",0xf0e2], rename:["solid",0xf15b],
    edit:["solid",0xf303], "html-edit":["regular",0xf044], lock:["solid",0xf084],
    eye:["solid",0xf06e], extract:["glyph",0xe096], archive:["glyph",0xe097],
    home:["solid",0xf015], up:["solid",0xf3bf], back:["solid",0xf060], next:["solid",0xf061],
    refresh:["solid",0xf021], select:["regular",0xf14a], unselect:["regular",0xf0c8],
    settings:["glyph",0xe019], search:["solid",0xf002], close:["solid",0xf00d],
    grid:["solid",0xf065], more:["solid",0xf142], info:["solid",0xf05a], list:["solid",0xf03a],
    "file-text":["solid",0xf15c], "file-code":["solid",0xf1c9], "file-image":["solid",0xf1c5],
    "file-archive":["solid",0xf1c6], "file-pdf":["solid",0xf1c1], "file-audio":["solid",0xf1c7], "file-video":["solid",0xf1c8]
  };
  const icon = name => { const glyph=glyphs[name]; return glyph ? `<i class="fm-icon fm-icon-${glyph[0]}" aria-hidden="true">${String.fromCodePoint(glyph[1])}</i>` : `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="${paths[name] || paths.file}"/></svg>`; };
  const button = (action,label,name,extra="") => `<button type="button" class="fm-tool ${extra}" data-fm-action="${action}" title="${label}">${icon(action==="delete" ? "delete" : action==="rename" ? "rename" : action==="html-edit" ? "html-edit" : action==="trash" && label==="Restore" ? "restore" : action==="new-folder" ? "plus" : name)}<span>${label}</span></button>`;
  const preferenceKey = "litecpFileManagerPreferences";
  const readPreferences = () => {
    try { return {start:"root",hidden:false,skipEncoding:false,...JSON.parse(localStorage.getItem(preferenceKey) || "{}")}; }
    catch { return {start:"root",hidden:false,skipEncoding:false}; }
  };
  let preferences = readPreferences();
  let listing = null, selected = new Set(), folders = new Map(), expanded = new Set([""]), loadingFolders = new Set(), folderErrors = new Map(), site = "", view = "list", descending = false, history = [], historyIndex = -1, historyNavigation = false, lastIndex = -1;
  const displayPath = (path = "") => (listing?.home || "/public").replace(/\/$/, "") + (path ? "/" + path : "") || "/";
  const inputPath = value => normalizePath(value, listing?.home || "/public");
  let searchRequest = 0;
  const siteID = () => $("#fileSiteSelect").value;
  const rows = () => prepareEntries(listing?.entries || [], $("#fileSearch").value, $("#showHidden").checked, $("#fileSort").value, descending);
  const picked = () => (listing?.entries || []).filter(x => selected.has(x.path));
  const join = (a,b) => a ? `${a}/${b}` : b;
  page.classList.add("fm-page");
  panel.insertAdjacentHTML("afterbegin", `<header class="cpfm-header"><div class="cpfm-brand"><img class="litecp-logo" src="assets/litecp-cloud-logo-v2.png" alt="LiteCP" width="2029" height="775"> File Manager</div><form id="cpfmSearch"><label>Search</label><select id="cpfmSearchScope" aria-label="Search location"><option value="all">All Your Files</option><option value="root">Web Root (public_html)</option><option value="current">Current Directory</option></select><span>for</span><input id="cpfmSearchInput" type="search" aria-label="Search files" autocomplete="off"><button type="submit">Go</button></form><button type="button" class="cpfm-settings" data-fm-action="settings">${icon("settings")} Settings</button></header>`);
  $(".file-toolbar").innerHTML = `${button("new-file","File","plus")}${button("new-folder","Folder","folder")}${button("copy","Copy","copy")}${button("move","Move","move")}${button("upload","Upload","upload")}${button("upload-url","Upload via URL","upload")}${button("download","Download","download")}${button("delete","Delete","trash")}${button("trash","Restore","refresh")}${button("rename","Rename","edit")}${button("edit","Edit","edit")}${button("html-edit","HTML Editor","edit")}${button("chmod","Permissions","lock")}${button("view","View","eye")}${button("extract","Extract","extract")}${button("archive","Compress","archive")}<input class="hidden" id="fileUploadInput" type="file" multiple>`;
  fullscreen.innerHTML = `${icon("grid")}<span>Full screen</span>`;
  fullscreen.setAttribute('aria-label','Toggle File Manager full screen');
  $(".cpfm-header").append(fullscreen);
  $(".file-breadcrumb").insertAdjacentHTML("beforebegin", `<div class="cpfm-navigation">${button("home","Home","home")}${button("up","Up One Level","up")}${button("back","Back","back")}${button("forward","Forward","next")}${button("refresh","Reload","refresh")}${button("select-all","Select All","select")}${button("unselect-all","Unselect All","unselect")}${button("trash","View Trash","trash")}${button("empty-trash","Empty Trash","trash")}</div><form class="fm-address" id="fmAddressForm"><button type="button" class="fm-home" data-fm-action="home" aria-label="Home">${icon("home")}</button><label><input id="fmAddress" aria-label="Folder path" placeholder="/public" autocomplete="off" spellcheck="false"></label><button class="button button-secondary" type="submit">Go</button></form><button type="button" class="cpfm-collapse" id="cpfmCollapse">Collapse All</button>`);
  const filters = $(".file-filters");
  filters.innerHTML = `<label class="fm-search">${icon("search")}<input id="fileSearch" type="search" placeholder="Search this folder…" aria-label="Search this folder"></label><label class="fm-hidden"><input type="checkbox" id="showHidden" ${preferences.hidden ? "checked" : ""}> Hidden files</label><select id="fileSort" aria-label="Sort files"><option value="name">Name</option><option value="type">Type</option><option value="modified">Modified</option><option value="size">Size</option><option value="mode">Permissions</option></select><button type="button" class="fm-order" id="fmOrder" aria-label="Reverse sort order">↑</button><div class="fm-view-switch" role="group" aria-label="File view">${button("list","List view","list")}${button("grid","Grid view","grid")}</div>`;
  const body = document.createElement("div"); body.className = "fm-body";
  body.innerHTML = `<aside class="fm-folders" aria-label="Folder navigation"><div id="fmTree" role="tree" aria-label="Website folders"></div></aside><div class="fm-resizer" role="separator" aria-orientation="vertical" aria-label="Resize folder panel" aria-valuemin="220" aria-valuemax="720" tabindex="0"><span aria-hidden="true">⋮</span></div><div class="fm-content" tabindex="0" aria-label="Files in current folder"><div class="fm-selection"><label><input id="fmSelectVisible" type="checkbox" aria-label="Select all items in this folder"><span id="fmSelectionLabel">No items selected</span></label></div><div id="fmFileHost"></div><div class="fm-drop-hint">${icon("upload")}Drop files here to upload</div></div><aside class="fm-inspector" aria-label="File details" id="fmInspector"></aside>`;
  panel.append(body); $("#fmFileHost").append($("#fileTable"));
  const resizer=$(".fm-resizer"), savedTreeWidth=Number(localStorage.getItem("litecpFileTreeWidth"));
  const setTreeWidth=value=>{const maximum=Math.min(720,Math.max(220,body.clientWidth-420)),width=Math.max(220,Math.min(maximum,Math.round(value)));body.style.setProperty("--fm-tree-width",`${width}px`);panel.style.setProperty("--fm-tree-width",`${width}px`);resizer.setAttribute("aria-valuenow",String(width));return width;};
  setTreeWidth(Number.isFinite(savedTreeWidth) && savedTreeWidth ? savedTreeWidth : 330);
  resizer.addEventListener("pointerdown",event=>{event.preventDefault();resizer.setPointerCapture(event.pointerId);body.classList.add("fm-resizing");});
  resizer.addEventListener("pointermove",event=>{if(!resizer.hasPointerCapture(event.pointerId))return;setTreeWidth(event.clientX-body.getBoundingClientRect().left);});
  resizer.addEventListener("pointerup",event=>{if(!resizer.hasPointerCapture(event.pointerId))return;resizer.releasePointerCapture(event.pointerId);body.classList.remove("fm-resizing");localStorage.setItem("litecpFileTreeWidth",String(parseInt(getComputedStyle(body).getPropertyValue("--fm-tree-width"),10)));});
  resizer.addEventListener("dblclick",()=>{localStorage.setItem("litecpFileTreeWidth","400");setTreeWidth(400);});
  resizer.addEventListener("keydown",event=>{if(!["ArrowLeft","ArrowRight"].includes(event.key))return;event.preventDefault();const current=parseInt(getComputedStyle(body).getPropertyValue("--fm-tree-width"),10)||400;localStorage.setItem("litecpFileTreeWidth",String(setTreeWidth(current+(event.key==="ArrowLeft" ? -20 : 20))));});
  panel.insertAdjacentHTML("beforeend", '<footer class="fm-footer"><span id="fmStatus" role="status">Select a website to begin.</span><span>Double-click to open · F2 to rename</span></footer>');
  const trashPanel = $("#trashPanel");
  trashPanel.innerHTML = `<div class="panel-head"><div><h2>Trash</h2><p>Recoverable items stored outside your public website.</p></div><div class="feature-actions">${button("close-trash","Close","close")}${button("empty-trash","Empty trash","trash","fm-danger")}</div></div><div id="trashTable"></div>`;
  // clientX/clientY use viewport coordinates. Keep the fixed menu outside page
  // containers whose transforms/animations can create a different containing block.
  const menu = document.createElement("div"); menu.id = "fmContextMenu"; menu.className = "fm-context hidden"; menu.setAttribute("role","menu"); menu.setAttribute("aria-label","File actions"); document.body.append(menu);
  const dialog = document.createElement("dialog"); dialog.id="fmActionDialog"; dialog.className = "fm-dialog"; page.append(dialog);
  // Keep the editor in viewport coordinates, outside the file page's layout.
  document.body.append($("#editorModal"));
  dialog.setAttribute("aria-labelledby","fmDialogTitle");
  dialog.addEventListener("cancel", event => { if (dialog.querySelector('button[type="submit"]')?.disabled) event.preventDefault(); });
  function closeMenu() { menu.classList.add("hidden"); }
  function syncSelection() {
    const items = picked(), one = items.length === 1, current = items[0];
    $("#fmSelectionLabel").textContent = items.length ? `${items.length} selected · ${cp.formatBytes(items.reduce((sum,x)=>sum+(x.type === "file" ? x.size : 0),0))}` : "Select items to manage them";
    page.querySelectorAll("[data-fm-action]").forEach(el => {
      const a = el.dataset.fmAction;
      if (["copy","move","archive","delete","clear"].includes(a)) el.disabled = !items.length;
      if(a==="download")el.disabled=!items.length||items.some(item=>item.type!=="file")||items.length>250||(siteID()==="server-explorer"&&items.length>1);
      if (["rename","chmod","edit","html-edit","view","extract"].includes(a)) el.disabled = !one || (["view","download"].includes(a) && current?.type !== "file") || (a==="edit"&&!editableFile(current)) || (a==="html-edit"&&!htmlFile(current)) || (a === "extract" && !extractableFile(current));
    });
    $("#fmInspector").innerHTML = one ? `<div class="fm-detail-icon ${current.type === "directory" ? "is-folder" : ""}">${icon(current.type === "directory" ? "folder" : kind(current)==="Archive" ? "archive" : "file")}</div><h3>${esc(current.name)}</h3><span class="fm-kind">${esc(kind(current))}</span><dl><dt>Location</dt><dd>${esc(displayPath(current.path))}</dd><dt>Size</dt><dd>${current.type === "directory" ? "Folder" : cp.formatBytes(current.size)}</dd><dt>Permissions</dt><dd><code>${esc(current.mode)}</code></dd><dt>Modified</dt><dd>${esc(cp.formatDateTime(current.modified))}</dd></dl><p>Changes apply immediately to this website.</p>` : `<div class="fm-detail-icon">${icon(items.length ? "copy" : "info")}</div><h3>${items.length ? `${items.length} items selected` : "File details"}</h3><p>${items.length ? "Use the toolbar to copy, move, compress or move the selection to Trash." : "Select a file or folder to view its details and available actions."}</p>`;
    page.querySelectorAll("[data-fm-row]").forEach(row => row.classList.toggle("fm-selected",selected.has(row.dataset.fmRow)));
    page.querySelectorAll("[data-file-select]").forEach(input => input.checked=selected.has(input.value));
    const visible = rows();
    [$("#fmSelectAll"),$("#fmSelectVisible")].filter(Boolean).forEach(all=>{ all.checked = visible.length > 0 && visible.every(x=>selected.has(x.path)); all.indeterminate = !all.checked && visible.some(x=>selected.has(x.path)); });
  }
  function render(value) {
	const serverMode=value.siteId==="server-explorer";
	page.classList.toggle("server-file-explorer",serverMode);
	const brand=panel.querySelector('.cpfm-brand');
	if(brand?.lastChild?.nodeType===Node.TEXT_NODE)brand.lastChild.textContent=serverMode?' Server File Explorer · Read only':' File Manager';
    if(value.siteId!=="server-explorer" && !site && preferences.start==="domain" && preferences.startSite && preferences.startSite!==value.siteId && cp.state()?.sites.some(item=>item.id===preferences.startSite)) {
      $("#fileSiteSelect").value=preferences.startSite;$("#fileSiteSelect").dispatchEvent(new Event("change"));return;
    }
    const changed = !listing || listing.siteId !== value.siteId || listing.path !== value.path;
    const siteChanged=site !== value.siteId;
    const startupPath=value.siteId==="server-explorer" ? "" : siteChanged && preferences.start==="last" ? (localStorage.getItem(`litecpFileManagerLastPath:${value.siteId}:${value.home || "legacy"}`) || "") : ["public","domain"].includes(preferences.start) ? (value.publicPath || "") : preferences.start==="ftp" ? "public_ftp" : "";
    if (siteChanged) { folders.clear(); expanded=new Set([""]); folderErrors.clear(); loadingFolders.clear(); history=[]; historyIndex=-1; site=value.siteId; }
    if (changed) { selected.clear(); lastIndex=-1; if (!historyNavigation) { history=history.slice(0,historyIndex+1); history.push(value.path || ""); historyIndex=history.length-1; } historyNavigation=false; }
    listing=value; selected=new Set([...selected].filter(path=>(value.entries || []).some(x=>x.path===path)));
    historyNavigation=false;
    if(history[historyIndex] !== (value.path || "")){const historyMatch=history.lastIndexOf(value.path || "");if(historyMatch>=0)historyIndex=historyMatch;}
    page.querySelectorAll("[data-fm-action]").forEach(button=>button.disabled=false);
    folders.set(value.path || "",(value.entries || []).filter(x=>x.type==="directory"));
    if(siteChanged && startupPath!==(value.path || "")) setTimeout(()=>cp.goToFilePath(startupPath).catch(cp.handleError),0);
    else localStorage.setItem(`litecpFileManagerLastPath:${value.siteId}:${value.home || "legacy"}`,value.path || "");
    const visible=rows(), count=(value.entries || []).length;
    $("#fmAddress").value=displayPath(value.path);
    const parts=(value.path || "").split("/").filter(Boolean);
    $("#fileBreadcrumb").innerHTML=`<button data-file-path="">${icon("folder")}${esc(displayPath())}</button>`+parts.map((part,i)=>`<span>/</span><button data-file-path="${esc(parts.slice(0,i+1).join("/"))}">${esc(part)}</button>`).join("");
    page.querySelector('[data-fm-action="up"]').disabled=!value.path;
    page.querySelector('[data-fm-action="back"]').disabled=historyIndex<=0;
    page.querySelector('[data-fm-action="forward"]').disabled=historyIndex>=history.length-1;
    page.querySelector('[data-fm-action="list"]').setAttribute("aria-pressed",String(view==="list"));
    page.querySelector('[data-fm-action="grid"]').setAttribute("aria-pressed",String(view==="grid"));
    $("#fileTable").classList.toggle("fm-grid-view",view==="grid");
    const entryIcon = entry => {
      if (entry.type === "directory" && entry.path === listing?.publicPath && listing.publicPath) return `<span class="fm-entry-icon is-webroot">${icon("globe")}</span>`;
      const ext=entry.name.split(".").pop().toLowerCase();
      const type=entry.type==="directory" ? "folder" : kind(entry)==="Archive" ? "archive" : kind(entry)==="Image" ? "image" : ext==="pdf" ? "pdf" : ["mp3","wav","ogg","flac","m4a"].includes(ext) ? "audio" : ["mp4","webm","mov","avi"].includes(ext) ? "video" : ["html","htm","css","js","json","xml"].includes(ext) ? "code" : ["php","txt","md","ini","conf","sql","log","htaccess"].includes(ext) ? "text" : "file";
      return `<span class="fm-entry-icon is-${type}">${icon(type==="folder" || type==="file" ? type : "file-"+type)}</span>`;
    };
    const name = entry => `<button type="button" class="fm-name" data-fm-open="${esc(entry.path)}" title="${entry.type==="directory" ? "Select folder; double-click to open" : "Select file; double-click to edit"}">${entryIcon(entry)}<span>${esc(entry.name)}</span></button>`;
    if (!visible.length) $("#fileTable").innerHTML=`<div class="fm-empty">${icon($("#fileSearch").value ? "search" : "folder")}<h3>${count ? "No matching items" : "This folder is empty"}</h3><p>${count ? "Try another search or show hidden files." : "Upload files or create a new folder to get started."}</p></div>`;
    else if (view==="grid") $("#fileTable").innerHTML=`<div class="fm-cards">${visible.map(x=>`<article class="fm-card" data-fm-row="${esc(x.path)}" tabindex="0"><input type="checkbox" data-file-select value="${esc(x.path)}" aria-label="Select ${esc(x.name)}">${name(x)}<small>${x.type==="directory" ? "Folder" : cp.formatBytes(x.size)}</small><button class="fm-more" type="button" data-fm-menu="${esc(x.path)}" aria-label="Actions for ${esc(x.name)}">${icon("more")}</button></article>`).join("")}</div>`;
    else $("#fileTable").innerHTML=`<table class="fm-table"><thead><tr><th><input id="fmSelectAll" type="checkbox" aria-label="Select all visible items"></th>${[["name","Name"],["size","Size"],["modified","Last Modified"],["type","Type"],["mode","Permissions"]].map(([key,label])=>`<th aria-sort="${$("#fileSort").value===key ? (descending ? "descending" : "ascending") : "none"}"><button type="button" class="fm-sort-heading" data-fm-sort="${key}">${label}${$("#fileSort").value===key ? (descending ? " ▾" : " ▴") : ""}</button></th>`).join("")}</tr></thead><tbody>${visible.map(x=>`<tr data-fm-row="${esc(x.path)}" tabindex="0"><td><input type="checkbox" data-file-select value="${esc(x.path)}" aria-label="Select ${esc(x.name)}"></td><td>${name(x)}</td><td>${cp.formatBytes(x.size)}</td><td>${esc(cp.formatDateTime(x.modified))}</td><td>${esc(kind(x))}</td><td><button type="button" class="fm-mode" data-fm-permissions="${esc(x.path)}" title="Change permissions">${esc(x.mode)}</button></td></tr>`).join("")}</tbody></table>`;
    $("#fmStatus").textContent=`${visible.length} of ${count} items · ${(value.entries || []).filter(x=>x.type==="directory").length} folders · ${cp.formatBytes((value.entries || []).reduce((n,x)=>n+(x.type==="file" ? x.size : 0),0))} in files`;
    $("#fmOrder").textContent=descending ? "↓" : "↑";
    page.querySelectorAll('[data-fm-row]').forEach(row=>row.draggable=siteID()!=="server-explorer");
    syncSelection(); renderTree();
    if (!folders.has("") && !loadingFolders.has("")) loadFolder("");
  }
  function renderTree() {
    function branch(path,label,depth) {
      const open=expanded.has(path), children=folders.get(path), error=folderErrors.get(path);
      const root=path==="", displayLabel=root ? `(${displayPath()})` : label;
      const entryIcon=root ? `<span class="fm-tree-root-icons">${icon(open ? "folder-open" : "folder")}${icon("home")}</span>` : icon(open ? "folder-open" : "folder");
      return `<div role="treeitem" aria-expanded="${open}" aria-selected="${listing?.path===path}" class="fm-tree-item"><div class="fm-tree-row ${root ? "is-root" : ""} ${listing?.path===path ? "active" : ""}" style="--depth:${depth}"><button type="button" data-fm-expand="${esc(path)}" aria-label="${open ? "Collapse" : "Expand"} ${esc(displayLabel)}">${open ? "−" : "+"}</button><button type="button" data-fm-folder="${esc(path)}">${entryIcon}<span>${esc(displayLabel)}</span></button></div>${open ? `<div role="group">${children ? children.map(child=>branch(child.path,child.name,depth+1)).join("") : `<small class="fm-tree-note">${error ? esc(error) : "Loading folders…"}</small>`}</div>` : ""}</div>`;
    }
    $("#fmTree").innerHTML=branch("","public",0);
  }
  async function loadFolder(path) {
    if(siteID()!=="server-explorer" && /^\.trash(?:\/|$)/.test(path)){await showTrash();return;}
    const expected=site; loadingFolders.add(path); folderErrors.delete(path);
    try { const value=await cp.api(`/api/files?siteId=${encodeURIComponent(expected)}&path=${encodeURIComponent(path)}`); if (site!==expected) return; folders.set(path,value.entries.filter(x=>x.type==="directory")); }
    catch(error) { if (site===expected) folderErrors.set(path,"Unable to load. Collapse and expand to retry."); }
    finally { if (site===expected) { loadingFolders.delete(path); renderTree(); } }
  }
  function choose(path,event={}) {
    const visible=rows(), index=visible.findIndex(x=>x.path===path);
    if (event.shiftKey && lastIndex>=0 && index>=0) for (let i=Math.min(index,lastIndex);i<=Math.max(index,lastIndex);i++) selected.add(visible[i].path);
    else if (event.ctrlKey || event.metaKey || event.target?.matches("input[type=checkbox]")) { if (selected.has(path)) selected.delete(path); else selected.add(path); }
    else selected=new Set([path]);
    lastIndex=index; syncSelection();
  }
  function selectOne(path) { selected=new Set([path]); syncSelection(); }
  function showMenu(path,x,y) {
    if (!selected.has(path)) selectOne(path);
    const current=picked(), singleFile=current.length===1 && current[0].type==="file";
    const actions=[...(current.length&&current.every(item=>item.type==="file") ? [["download","Download","download"]] : []),...(singleFile ? [["view","View","eye"]] : []),...(singleFile&&editableFile(current[0]) ? [["edit","Edit","edit"]] : []),...(singleFile&&htmlFile(current[0]) ? [["html-edit","HTML Editor","html-edit"]] : []),["move","Move","move"],["copy","Copy","copy"],["rename","Rename","rename"],["chmod","Change Permissions","lock"],["delete","Delete","delete"],...(singleFile && extractableFile(current[0]) ? [["extract","Extract","extract"]] : []),["archive","Compress","archive"]];
    menu.innerHTML=actions.filter(([action])=>siteID()!=="server-explorer" || ["download","view"].includes(action)).map(([action,label,name])=>button(action,label,name)).join("");
    menu.querySelectorAll("button").forEach(b=>{ const peer=page.querySelector(`[data-fm-action="${b.dataset.fmAction}"]`); b.disabled=peer?.disabled || false; b.setAttribute("role","menuitem"); });
    menu.classList.remove("hidden");
    const gap=8;
    menu.style.maxHeight=Math.max(1,window.innerHeight-gap*2)+"px";
    menu.style.maxWidth=Math.max(1,window.innerWidth-gap*2)+"px";
    menu.style.left="0px";menu.style.top="0px";
    const rect=menu.getBoundingClientRect();
    const position=contextMenuPosition(x,y,rect.width,rect.height,window.innerWidth,window.innerHeight);
    menu.style.left=position.left+"px";menu.style.top=position.top+"px";
    menu.querySelector("button:not(:disabled)")?.focus({preventScroll:true});
  }
  function contextMenuPosition(x,y,width,height,viewportWidth,viewportHeight) {
    const gap=8,offset=2;
    const left=x+offset+width<=viewportWidth-gap ? x+offset : x-width-offset;
    const top=y+offset+height<=viewportHeight-gap ? y+offset : y-height-offset;
    return {left:Math.max(gap,Math.min(left,Math.max(gap,viewportWidth-width-gap))),top:Math.max(gap,Math.min(top,Math.max(gap,viewportHeight-height-gap)))};
  }
  function openDialog({title,description,fields="",submit="Save",danger=false,perform,onCancel}) {
    if (dialog.open) return;
    dialog.classList.remove("fm-search-dialog","fm-preferences-dialog");
    const expectedSite=siteID(), expectedPath=cp.path();
    dialog.innerHTML=`<form><header><h2 id="fmDialogTitle">${esc(title)}</h2><button type="button" data-fm-cancel aria-label="Close dialog">${icon("close")}</button></header><div class="fm-dialog-body">${description ? `<p>${esc(description)}</p>` : ""}${fields}<p class="fm-error hidden" role="alert"></p></div><footer>${submit ? `<button type="submit" class="button ${danger ? "fm-danger-button" : "button-primary"}">${esc(submit)}</button>` : ""}<button type="button" class="button button-ghost" data-fm-cancel>Cancel</button></footer></form>`;
    dialog.querySelectorAll("[data-fm-cancel]").forEach(b=>b.onclick=()=>{if(!onCancel || onCancel()!==false)dialog.close();});
    dialog.querySelector("form").onsubmit=async event=>{
      event.preventDefault(); if (!event.target.reportValidity()) return;
      const submittedForm=event.target;
      const buttons=dialog.querySelectorAll("button"); buttons.forEach(b=>b.disabled=!(onCancel && b.hasAttribute("data-fm-cancel"))); dialog.querySelector(".fm-error").classList.add("hidden");
      try { if (siteID()!==expectedSite || cp.path()!==expectedPath) throw new Error("The website or folder changed. Close this dialog and try again."); await perform(new FormData(submittedForm)); if(dialog.contains(submittedForm))dialog.close(); }
      catch(error) { const output=submittedForm.querySelector(".fm-error"); if(dialog.open&&dialog.contains(submittedForm)){output.textContent=error.message;output.classList.remove("hidden");} }
      finally { buttons.forEach(b=>b.disabled=false); }
    };
    dialog.showModal(); dialog.querySelector("input:not([type=checkbox])")?.focus();
  }
  function showSearchResults(result, query) {
    if (dialog.open) return;
    dialog.classList.remove("fm-preferences-dialog");
    dialog.classList.add("fm-search-dialog");
    const entries=result.entries || [];
    dialog.innerHTML=`<div class="fm-search-results"><header><h2 id="fmDialogTitle">Search Results for: ${esc(query)}</h2><button type="button" data-fm-cancel aria-label="Close dialog">${icon("close")}</button></header><div class="fm-dialog-body"><p>Double click on a file or folder to open the folder (or the folder it is contained in if it is a file).</p>${result.truncated ? '<p class="fm-search-warning">Only the first 1,000 matches are shown. Refine your search to see fewer results.</p>' : ""}${entries.length ? `<div class="fm-search-table"><table class="fm-table"><thead><tr><th>Name</th></tr></thead><tbody>${entries.map(entry=>`<tr><td><button type="button" class="fm-search-result" data-fm-search-path="${esc(entry.path)}" data-fm-search-type="${esc(entry.type)}">${esc(displayPath(entry.path))}</button></td></tr>`).join("")}</tbody></table></div>` : '<div class="fm-empty"><p>No matching files or folders.</p></div>'}</div><footer><button type="button" class="button button-secondary" data-fm-cancel>Close</button></footer></div>`;
    dialog.querySelectorAll("[data-fm-cancel]").forEach(button=>button.onclick=()=>dialog.close());
    dialog.querySelectorAll("[data-fm-search-path]").forEach(button=>{const open=async()=>{
      const path=button.dataset.fmSearchPath, folder=button.dataset.fmSearchType==="directory" ? path : path.split("/").slice(0,-1).join("/");
      dialog.close();
      await cp.goToFilePath(folder);
      if(button.dataset.fmSearchType!=="directory") { selected=new Set([path]); syncSelection(); }
    };button.ondblclick=open;button.onclick=event=>{if(event.detail===0)open();};});
    dialog.showModal();
  }
  async function searchFiles() {
    const query=$("#cpfmSearchInput").value.trim();
    if(!query) { $("#cpfmSearchInput").focus(); return; }
    const scope=$("#cpfmSearchScope").value;
    const root=scope==="current" ? cp.path() : scope==="root" ? (listing?.publicPath || "") : "";
    const expectedSite=siteID(), expectedPath=cp.path(), request=++searchRequest;
    const params=new URLSearchParams({siteId:expectedSite,root,query,hidden:$("#showHidden").checked ? "1" : "0"});
    const result=await cp.api(`/api/files/search?${params}`);
    if(request!==searchRequest || siteID()!==expectedSite || cp.path()!==expectedPath) return;
    showSearchResults(result,query);
  }
  async function editFile(item,html=false) {
    const open=async form=>{await cp.openEditor(item.path,{encoding:form?.get("encoding")||"UTF-8",visual:html});};
    if(preferences.skipEncoding) { open().catch(cp.handleError); return; }
    const expectedSite=siteID(), expectedPath=cp.path(), catalog=await cp.api("/api/files/encodings");
    if(siteID()!==expectedSite||cp.path()!==expectedPath)return;
    const encodings=catalog.encodings||["UTF-8"];
    openDialog({title:html ? "HTML Editor" : "Edit",description:"",fields:`<div class="fm-path-list"><strong>You will be editing:</strong><div>${esc(displayPath(item.path))}</div></div><label class="fm-field"><span>Character encoding:</span><select name="encoding">${encodings.map(value=>`<option>${value}</option>`).join("")}</select><small>Text files up to 64 MiB. Saving preserves the selected encoding.</small></label>`,submit:"Edit",perform:open});
  }
  const field = (label,name,value="",help="") => `<label class="fm-field"><span>${esc(label)}</span><input name="${name}" value="${esc(value)}" autocomplete="off" ${["name","mode"].includes(name) ? "required" : ""}>${help ? `<small>${esc(help)}</small>` : ""}</label>`;
  const pathField=(label,name,value)=>`<label class="fm-field"><span>${esc(label)}</span><span class="fm-path-input">${icon("home")}<input name="${name}" value="${esc(value)}" autocomplete="off" required></span></label>`;
  const pathList=(label,items)=>`<div class="fm-path-list"><strong>${esc(label)}</strong>${items.map(item=>`<div>${esc(displayPath(item.path))}</div>`).join("")}</div>`;
  function filename(value) { const name=String(value).trim(); if (!name || name==="." || name===".." || /[/\\\x00-\x1f]/.test(name)) throw new Error("Enter a file name without slashes or control characters."); return name; }
  async function refresh() { closeMenu(); await cp.loadFiles(); }
  async function action(name) {
    closeMenu(); const items=picked(), item=items[0], activeSite=siteID(), currentPath=cp.path();
    if(activeSite==="server-explorer" && !["home","up","back","forward","refresh","reload","view","download","settings","select-all","unselect-all","clear","list","grid"].includes(name))return;
    if (!activeSite) { cp.showToast("Choose a website","Add or select a website before managing files.","error"); return; }
    if (["copy","move","archive","delete"].includes(name) && !items.length) return;
    if (["rename","chmod","edit","html-edit","view","download","extract"].includes(name) && items.length!==1) return;
    const api = (url,method,body) => cp.api(url,{method,body:{siteId:activeSite,...body}});
    if (name==="refresh") { folders.clear(); folderErrors.clear(); await refresh(); return; }
    if (name==="clear") { selected.clear(); syncSelection(); return; }
    if (name==="list" || name==="grid") { view=name; if (listing) render(listing); return; }
    if (name==="home") { await cp.goToFilePath(""); return; }
    if (name==="select-all") { selected=new Set(rows().map(x=>x.path)); syncSelection(); return; }
    if (name==="unselect-all") { selected.clear(); syncSelection(); return; }
    if (name==="up") { await cp.goToFilePath(currentPath.split("/").slice(0,-1).join("/")); return; }
    if (name==="back" || name==="forward") { const next=historyIndex+(name==="back" ? -1 : 1); if (next<0 || next>=history.length) return; historyIndex=next; historyNavigation=true; await cp.goToFilePath(history[next]); return; }
    if (name==="edit" || name==="html-edit") { if (item.type==="file") await editFile(item,name==="html-edit"); return; }
    if (name==="view") { if (!cp.demo && item.type==="file") window.open(`/api/files/download?preview=1&siteId=${encodeURIComponent(activeSite)}&path=${encodeURIComponent(item.path)}`,"_blank","noopener"); return; }
    if (name==="download") {
      if(cp.demo)throw new Error('Downloads require the installed server.');
      if(!items.length||items.some(entry=>entry.type!=="file"))throw new Error('Select files to download. Use Compress for folders.');
      if(items.length===1){window.location.assign(`/api/files/download?siteId=${encodeURIComponent(activeSite)}&path=${encodeURIComponent(item.path)}`);return;}
      if(items.length>250)throw new Error('Select at most 250 files per ZIP download.');
      const form=document.createElement('form');form.method='POST';form.action='/api/files/download-selected?siteId='+encodeURIComponent(activeSite);form.target='_blank';form.hidden=true;
      for(const entry of items){const input=document.createElement('input');input.type='hidden';input.name='paths';input.value=entry.path;form.append(input);}
      document.body.append(form);form.submit();form.remove();return;
    }
    if(name==="upload-url") {
      let controller=null,running=false;
      openDialog({title:"Upload via URL",description:`Download a public HTTP/HTTPS file into ${displayPath(currentPath)}. Existing files will not be replaced. Links must point to the file, not a website download page.`,fields:'<label class="fm-field"><span>File URL</span><input name="url" type="url" required maxlength="8192" placeholder="https://example.com/archive.zip"></label><label class="fm-field"><span>Save as (optional)</span><input name="filename" maxlength="255" placeholder="Use the filename from the URL"></label><progress data-url-progress max="100" value="0"></progress><p data-url-status role="status"></p>',submit:"Download file",onCancel:()=>{controller?.abort();return true;},perform:async data=>{
        if(cp.demo)throw new Error('URL uploads require the installed server.');
        controller=new AbortController();running=true;const output=dialog.querySelector('[data-url-status]'),progress=dialog.querySelector('[data-url-progress]');
        const cancelEscape=()=>controller.abort();dialog.addEventListener('cancel',cancelEscape);
        const cancelClose=()=>{if(running)controller.abort();};dialog.addEventListener('close',cancelClose);
        let completed=false;
        try{
          output.textContent='Connecting to download server…';progress.removeAttribute('value');
          const response=await fetch('/api/files/upload-url',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},signal:controller.signal,body:JSON.stringify({siteId:activeSite,path:currentPath,url:String(data.get('url')),name:String(data.get('filename')||'')})});
          if(!response.ok){const error=await response.json().catch(()=>({}));throw new Error(error.error||`Download failed (${response.status})`);}
          const reader=response.body.getReader(),decoder=new TextDecoder();let buffer='';
          const update=line=>{if(!line.trim())return;const state=JSON.parse(line);if(state.status==='error')throw new Error(state.error);if(state.status==='complete')completed=true;if(state.total>0){progress.value=Math.min(100,state.bytes/state.total*100);}else progress.removeAttribute('value');output.textContent=state.status==='complete'?'Download complete.':`Downloaded ${cp.formatBytes(state.bytes)}${state.total>0?' of '+cp.formatBytes(state.total):''}`;};
          while(true){const {done,value}=await reader.read();buffer+=decoder.decode(value||new Uint8Array(),{stream:!done});let end;while((end=buffer.indexOf('\n'))>=0){update(buffer.slice(0,end));buffer=buffer.slice(end+1);}if(done){if(buffer.trim())update(buffer);break;}}
          if(!completed)throw new Error('Download interrupted before completion. Refresh before retrying.');
          if(siteID()===activeSite&&cp.path()===currentPath)await refresh();cp.showToast('URL upload complete','File saved in the selected directory.');
        }catch(error){controller.abort();if(error.name==='AbortError')throw new Error('Download cancelled. Refresh to check whether the file completed.');throw error;}
        finally{running=false;dialog.removeEventListener('cancel',cancelEscape);dialog.removeEventListener('close',cancelClose);}
      }});return;
    }
    if (name==="upload") {
      openDialog({title:"File Upload",description:`Select the file you want to upload to “${displayPath(currentPath)}”.`,fields:'<div class="fm-upload-drop" id="fmUploadDrop"><p>Drop files here to start uploading</p><p>or</p><button type="button" class="button" id="fmChooseUpload">Select File</button></div>',submit:null});
      $("#fmChooseUpload").onclick=()=>$("#fileUploadInput").click();
      $("#fmUploadDrop").ondragover=event=>event.preventDefault();
      $("#fmUploadDrop").ondrop=event=>{event.preventDefault();event.stopPropagation();dialog.close();upload([...event.dataTransfer.files]);}; return;
    }
    if (name==="trash") { await showTrash(); return; }
    if (name==="close-trash") { trashPanel.classList.add("hidden"); return; }
    if (name==="settings") {
      document.querySelector(".cpfm-settings-menu")?.remove();
      openDialog({title:"Preferences",description:"",fields:`<div class="fm-preferences-start"><p>Always open this directory in the future by default:</p><label><input type="radio" name="start" value="root" ${!["last","public","ftp","domain"].includes(preferences.start) ? "checked" : ""}> Home Directory</label><label><input type="radio" name="start" value="public" ${preferences.start==="public" ? "checked" : ""}> Web Root (public_html)</label><label><input type="radio" name="start" value="ftp" ${preferences.start==="ftp" ? "checked" : ""}> Public FTP Root (public_ftp)</label><label><input type="radio" name="start" value="domain" ${preferences.start==="domain" ? "checked" : ""}> Document Root for:</label><select name="startSite" aria-label="Default website">${(cp.state()?.sites||[]).map(item=>`<option value="${esc(item.id)}" ${(preferences.startSite||siteID())===item.id ? "selected" : ""}>${esc(item.domain)}</option>`).join("")}</select><label><input type="radio" name="start" value="last" ${preferences.start==="last" ? "checked" : ""}> Last Opened Folder</label></div><div class="fm-preferences-options"><label><input type="checkbox" name="hidden" ${$("#showHidden").checked ? "checked" : ""}> Show Hidden Files (dotfiles)</label><label><input type="checkbox" name="skipEncoding" ${preferences.skipEncoding ? "checked" : ""}> Disable Character Encoding Verification Dialogs</label></div>`,submit:"Save",perform:async data=>{
        preferences={start:String(data.get("start")||"root"),startSite:String(data.get("startSite")||siteID()),hidden:data.has("hidden"),skipEncoding:data.has("skipEncoding")};
        localStorage.setItem(preferenceKey,JSON.stringify(preferences));$("#showHidden").checked=preferences.hidden;$("#showHidden").dispatchEvent(new Event("change"));
        cp.showToast("File Manager preferences saved","Your display and startup choices were updated.","success");
      }});
      dialog.classList.add("fm-preferences-dialog");return;
    }
    if (name==="new-folder" || name==="new-file") {
      const isFolder=name==="new-folder";
      openDialog({title:isFolder ? "New Folder" : "New File",description:"",fields:field(isFolder ? "New Folder Name:" : "New File Name:","name","",isFolder ? "" : "(ex: file.txt, file.html, file.php)")+pathField(isFolder ? "New Folder will be created in:" : "New file will be created in:","destination",displayPath(currentPath)),submit:isFolder ? "Create New Folder" : "Create New File",perform:async form=>{const result=await api(isFolder ? "/api/files/directory" : "/api/files/create","POST",{path:inputPath(form.get("destination")),name:filename(form.get("name"))}); await refresh(); cp.showToast(isFolder ? "Folder created" : "File created",result.path || String(form.get("name")));}}); return;
    }
    if (name==="rename") { openDialog({title:"Rename",description:"",fields:pathList("Old file name:",[item])+field("New file name:","name",item.name),submit:"Rename File",perform:async form=>{await api("/api/files/rename","POST",{path:item.path,destination:join(item.path.split("/").slice(0,-1).join("/"),filename(form.get("name")))}); await refresh();}}); return; }
    if (name==="copy" || name==="move") { openDialog({title:name==="copy" ? "Copy" : "Move",description:"",fields:pathList(name==="copy" ? "File path:" : "Current Path:",items)+pathField(`Enter the file path that you want to ${name} ${items.length===1 ? "this file" : "these files"} to:`,"destination",displayPath(currentPath)),submit:name==="copy" ? "Copy Files" : "Move Files",perform:async form=>{await api("/api/files/transfer","POST",{paths:items.map(x=>x.path),destination:inputPath(form.get("destination")),operation:name}); folders.clear(); await refresh();}}); return; }
    if (name==="chmod") {
      openDialog({title:"Change Permissions",description:"",fields:pathList("File(s):",[item])+`<input type="hidden" name="mode" value="${esc(item.mode)}"><div class="fm-permission-grid"><strong>Mode</strong><strong>User</strong><strong>Group</strong><strong>World</strong>${[["Read",4],["Write",2],["Execute",1]].map(([label,bit])=>`<strong>${label}</strong>${["User","Group","World"].map((owner,index)=>`<input type="checkbox" data-mode-bit="${bit << ((2-index)*3)}" aria-label="${owner} ${label.toLowerCase()}">`).join("")}`).join("")}<strong>Permission</strong>${[0,1,2].map(index=>`<input type="text" inputmode="numeric" maxlength="1" pattern="[0-7]" data-mode-digit="${index}" aria-label="${["User","Group","World"][index]} permission">`).join("")}</div>`,submit:"Change Permissions",perform:async form=>{const digits=[...dialog.querySelectorAll("[data-mode-digit]")].map(input=>input.value).join("");const mode="0"+digits;if (!/^0?[0-7]{3}$/.test(mode) || (parseInt(mode,8)&2)) throw new Error("Use three octal digits without write permission for World.");await api("/api/files/permissions","PUT",{path:item.path,mode});await refresh();}});
      const modeInput=dialog.querySelector('[name="mode"]'), boxes=[...dialog.querySelectorAll("[data-mode-bit]")];
      const update=()=>{boxes.forEach(box=>box.checked=Boolean(parseInt(modeInput.value,8)&Number(box.dataset.modeBit)));dialog.querySelectorAll("[data-mode-digit]").forEach(input=>input.value=modeInput.value.slice(-3)[Number(input.dataset.modeDigit)]);}; update(); modeInput.oninput=update;
      dialog.querySelectorAll("[data-mode-digit]").forEach(input=>input.oninput=()=>{const digits=[...dialog.querySelectorAll("[data-mode-digit]")].map(input=>input.value).join("");if(/^[0-7]{3}$/.test(digits)){modeInput.value="0"+digits;boxes.forEach(box=>box.checked=Boolean(parseInt(modeInput.value,8)&Number(box.dataset.modeBit)));}});
      boxes.forEach(box=>box.onchange=()=>{modeInput.value="0"+boxes.reduce((mode,b)=>mode+(b.checked ? Number(b.dataset.modeBit) : 0),0).toString(8).padStart(3,"0");update();}); return;
    }
    if (name==="archive") { const extensions={zip:".zip",tar:".tar","tar.gz":".tar.gz","tar.bz2":".tar.bz2"};openDialog({title:"Compress",description:"",fields:`<div class="fm-path-list"><strong>Compression Type:</strong>${[["zip","Zip Archive"],["tar","Tar Archive"],["tar.gz","GZiped Tar Archive"],["tar.bz2","Bzip2ed Tar Archive"]].map(([value,label])=>`<label><input type="radio" name="compression" value="${value}" ${value==="zip" ? "checked" : ""}> ${label}</label>`).join("")}</div>`+pathList("Files to compress:",items)+field("Archive name:","name","archive.zip"),submit:"Compress Files",perform:async form=>{let target=filename(form.get("name")),suffix=extensions[form.get("compression")];if(!suffix)throw new Error("Choose an archive format.");target=target.replace(/\.(tar\.bz2|tar\.gz|tbz2?|tgz|zip|tar)$/i,"")+suffix;await api("/api/files/archive","POST",{paths:items.map(x=>x.path),destination:join(currentPath,target)});await refresh();}});dialog.querySelectorAll('[name="compression"]').forEach(input=>input.onchange=()=>{const name=dialog.querySelector('[name="name"]');name.value=name.value.replace(/\.(tar\.bz2|tar\.gz|tbz2?|tgz|zip|tar)$/i,"")+extensions[input.value];});return; }
    if (name==="extract") { openDialog({title:"Extract",description:"Files with matching names may be replaced.",fields:pathList("File to extract:",[item])+pathField("Enter the path you wish to extract the files to:","destination",displayPath(currentPath)),submit:"Extract Files",perform:async form=>{await api("/api/files/extract","POST",{path:item.path,destination:inputPath(form.get("destination"))});folders.clear();await refresh();}}); return; }
    if (name==="delete") { openDialog({title:"Trash",description:"Are you sure you want to move the following files to the trash?",fields:pathList("",items),submit:"Confirm",danger:true,perform:async()=>{try {for(const entry of items)await api("/api/files","DELETE",{path:entry.path});} finally {folders.clear();await refresh();}}}); return; }
    if (name==="empty-trash") openDialog({title:"Permanently empty Trash?",description:"Every item in this website’s Trash will be permanently removed. This cannot be undone.",fields:field("Type EMPTY to confirm","confirmation"),submit:"Delete permanently",danger:true,perform:async form=>{if(form.get("confirmation")!=="EMPTY")throw new Error("Type EMPTY exactly to confirm.");await api("/api/files/trash","DELETE",{id:""});await showTrash();}});
  }
  async function showTrash() {
    trashPanel.classList.remove("hidden"); $("#trashTable").innerHTML='<div class="fm-empty"><p>Loading Trash…</p></div>'; trashPanel.scrollIntoView({behavior:"smooth",block:"nearest"});
    try {const result=cp.demo ? {entries:[]} : await cp.api(`/api/files/trash?siteId=${encodeURIComponent(siteID())}`);$("#trashTable").innerHTML=result.entries.length ? `<table class="fm-table"><thead><tr><th>File name</th><th>Original location</th><th>Size</th><th>Moved to Trash</th><th>Actions</th></tr></thead><tbody>${result.entries.map(x=>`<tr><td>${esc(x.name)}</td><td>${esc(x.originalPath)}</td><td>${cp.formatBytes(x.size)}</td><td>${esc(cp.formatDateTime(x.deletedAt))}</td><td><button class="button button-secondary" data-fm-restore="${esc(x.id)}">Restore</button><button class="button fm-danger" data-fm-purge="${esc(x.id)}">Delete permanently</button></td></tr>`).join("")}</tbody></table>` : '<div class="fm-empty"><h3>Trash is empty</h3><p>Items moved to Trash will appear here.</p></div>';}
    catch(error){$("#trashTable").textContent=error.message;}
  }
  function upload(files) {
	if(siteID()==="server-explorer")return;
    if (!files.length) return;
    if(new Set(files.map(file=>file.name)).size!==files.length){cp.showToast("Duplicate filenames","Choose files with unique names.","error");return;}
    const current=cp.path(), activeSite=siteID();
    if (!activeSite) return;
    if(dialog.open)return;
    let activeXHR=null,cancelled=false,running=false,uploadProgress,uploadStatus;
    const cancelUpload=()=>{
      if(cancelled||!running)return;
      cancelled=true;
      if(uploadStatus)uploadStatus.textContent='Upload cancelled.';
      activeXHR?.abort();
      cp.showToast('Upload cancelled','Remaining files will not be uploaded. Completed files were retained; refresh to check the current file.','info');
    };
    const conflicts=files.filter(file=>listing?.entries.some(x=>x.name===file.name));
    if(conflicts.length){cp.showToast("Existing filenames","Rename existing or selected files before uploading: "+conflicts.map(file=>file.name).join(', '),"error");return;}
    openDialog({title:"Upload files",description:`Destination: ${displayPath(current)}.${conflicts.length ? " Existing names detected: a single-file upload may replace its existing file; multi-file uploads reject conflicts." : " Upload starts automatically."}`,fields:`<ul class="fm-upload-list">${files.map(file=>`<li>${icon("file")}<span>${esc(file.name)}</span><small>${cp.formatBytes(file.size)}</small></li>`).join("")}</ul><progress id="fmUploadProgress" value="0" max="100"></progress><p id="fmUploadStatus" role="status">Ready to upload ${files.length} file(s).</p>`,submit:"Upload files",onCancel:()=>{cancelUpload();return true;},perform:async()=>{
      running=true;try{
      if(cp.demo)throw new Error("Uploads are unavailable in demo mode.");
      if(files.length>1&&conflicts.length)throw new Error("Rename existing files before uploading multiple files with the same names.");
      for(let index=0;index<files.length;index++){
        if(cancelled)throw new Error("Upload cancelled. Completed files were retained.");
        const file=files[index];await new Promise((resolve,reject)=>{
          const data=new FormData();data.append("siteId",activeSite);data.append("path",current);data.append("noReplace","1");data.append("files",file,file.name);
          const xhr=new XMLHttpRequest();activeXHR=xhr;xhr.onabort=()=>reject(new Error("Upload cancelled. Earlier files were retained; refresh to check the current file."));xhr.open("POST","/api/files/upload");xhr.responseType="json";
          xhr.upload.onprogress=event=>{if(event.lengthComputable){if(cancelled)return;const percent=Math.round(event.loaded/event.total*100);uploadProgress.value=(index+event.loaded/event.total)/files.length*100;uploadStatus.textContent=`File ${index+1} of ${files.length}: ${file.name} — ${percent}%`;}};
          xhr.onerror=()=>reject(new Error(`Connection interrupted at ${file.name}. ${index} earlier file(s) uploaded; refresh before retrying.`));
          xhr.onload=()=>xhr.status>=200&&xhr.status<300?resolve():reject(new Error(xhr.response?.error||`Upload failed at ${file.name}; ${index} earlier file(s) uploaded.`));xhr.send(data);
        });
      }
      if(cancelled)throw new Error('Upload cancelled.');
      if(siteID()===activeSite&&cp.path()===current)await refresh();
      if(!cancelled)cp.showToast("Upload complete",`${files.length} file(s) uploaded.`);
      }finally{running=false;activeXHR=null;}
    }});
    const uploadForm=dialog.querySelector('form');
    uploadProgress=dialog.querySelector('#fmUploadProgress');uploadStatus=dialog.querySelector('#fmUploadStatus');
    const onEscape=()=>cancelUpload();
    dialog.addEventListener('cancel',onEscape);dialog.addEventListener('close',()=>{cancelUpload();dialog.removeEventListener('cancel',onEscape);},{once:true});
    uploadForm.querySelector('[type="submit"]').hidden=true;
    uploadForm.requestSubmit();
  }
  document.addEventListener("litecp:open-trash",()=>showTrash().catch(cp.handleError));
  function run(name) { action(name).catch(error=>cp.handleError(error)); }
  page.addEventListener("click",event=>{
    const control=event.target.closest("[data-fm-action]");if(control){event.preventDefault();event.stopImmediatePropagation();run(control.dataset.fmAction);return;}
    const folder=event.target.closest("[data-fm-folder]");if(folder){cp.goToFilePath(folder.dataset.fmFolder);return;}
    const expand=event.target.closest("[data-fm-expand]");if(expand){const path=expand.dataset.fmExpand;if(expanded.has(path))expanded.delete(path);else{expanded.add(path);if(!folders.has(path))loadFolder(path);}renderTree();return;}
    const more=event.target.closest("[data-fm-menu]");if(more){showMenu(more.dataset.fmMenu,event.clientX,event.clientY);return;}
    const permissions=event.target.closest("[data-fm-permissions]");if(permissions){selectOne(permissions.dataset.fmPermissions);run("chmod");return;}
    const sort=event.target.closest("[data-fm-sort]");if(sort){const key=sort.dataset.fmSort;descending=$("#fileSort").value===key ? !descending : false;$("#fileSort").value=key;if(listing)render(listing);return;}
    const row=event.target.closest("[data-fm-row]");if(row){choose(row.dataset.fmRow,event);return;}
    const restore=event.target.closest("[data-fm-restore]"), purge=event.target.closest("[data-fm-purge]");
    if(restore || purge){const activeSite=siteID();openDialog({title:restore ? "Restore from Trash" : "Permanently delete item?",description:restore ? "Restore to its original location, or enter a new path. The parent folder must exist; existing files are not overwritten." : "This item will be permanently removed and cannot be recovered.",fields:restore ? field("Restore path (optional)","destination") : field("Type DELETE to confirm","confirmation"),submit:restore ? "Restore item" : "Delete permanently",danger:!restore,perform:async form=>{if(purge && form.get("confirmation")!=="DELETE")throw new Error("Type DELETE exactly to confirm.");await cp.api("/api/files/trash",{method:restore ? "POST" : "DELETE",body:{siteId:activeSite,id:(restore || purge).dataset[restore ? "fmRestore" : "fmPurge"],destination:restore ? inputPath(form.get("destination")) : ""}});await showTrash();await refresh();}});}
  },true);
  menu.addEventListener("click",event=>{const b=event.target.closest("[data-fm-action]");if(b)run(b.dataset.fmAction);});
  menu.addEventListener("keydown",event=>{if(event.key==="Escape"){closeMenu();$(".fm-content").focus();}else if(event.key==="ArrowDown" || event.key==="ArrowUp"){event.preventDefault();const buttons=[...menu.querySelectorAll("button:not(:disabled)")],index=buttons.indexOf(document.activeElement);buttons[(index+(event.key==="ArrowDown" ? 1 : buttons.length-1))%buttons.length]?.focus();}});
  document.addEventListener("click",event=>{if(!event.target.closest(".fm-context,[data-fm-menu]"))closeMenu();});
  window.addEventListener("resize",closeMenu);window.addEventListener("scroll",closeMenu,true);
  $("#fileTable").addEventListener("dblclick",event=>{const row=event.target.closest("[data-fm-row]");if(!row || event.target.closest("input,button.fm-more,.fm-mode"))return;selectOne(row.dataset.fmRow);const entry=picked()[0];if(entry.type==="directory")cp.goToFilePath(entry.path);else run(siteID()==="server-explorer"?"view":"edit");});
  $("#fileTable").addEventListener("contextmenu",event=>{const row=event.target.closest("[data-fm-row]");if(!row)return;event.preventDefault();showMenu(row.dataset.fmRow,event.clientX,event.clientY);});
  $("#fileTable").addEventListener("change",event=>{if(event.target.id==="fmSelectAll"){selected=event.target.checked ? new Set(rows().map(x=>x.path)) : new Set();syncSelection();}});
  $("#fmSelectVisible").onchange=event=>{selected=event.target.checked ? new Set(rows().map(x=>x.path)) : new Set();syncSelection();};
  $("#fmAddressForm").onsubmit=event=>{event.preventDefault();try{cp.goToFilePath(inputPath($("#fmAddress").value));}catch(error){cp.handleError(error);}};
  $("#cpfmSearch").onsubmit=event=>{event.preventDefault();searchFiles().catch(cp.handleError);};
  $("#cpfmCollapse").onclick=()=>{expanded=new Set();renderTree();};
  ["fileSearch","showHidden","fileSort"].forEach(id=>$("#"+id).addEventListener(id==="fileSearch" ? "input" : "change",()=>{selected.clear();lastIndex=-1;if(id==="fileSort")descending=["size","modified"].includes($("#fileSort").value);if(listing)render(listing);}));
  $("#fmOrder").onclick=()=>{descending=!descending;$("#fmOrder").textContent=descending ? "↓" : "↑";if(listing)render(listing);};
  $("#fileUploadInput").onchange=event=>{if(event.target.files.length && dialog.open)dialog.close();upload([...event.target.files]);event.target.value="";};
  let internalDrag=null,movePending=false;
  function dropDirectory(target){
    const tree=target.closest('[data-fm-folder]');if(tree)return {path:tree.dataset.fmFolder,node:tree.closest('.fm-tree-row')||tree};
    const row=target.closest('[data-fm-row]');if(!row)return null;
    const entry=(listing?.entries||[]).find(item=>item.path===row.dataset.fmRow);
    return entry?.type==='directory'?{path:entry.path,node:row}:null;
  }
  function validMoveTarget(paths,destination){
    return paths.length>0&&paths.every(path=>destination!==path&&!destination.startsWith(path+'/')&&destination!==path.split('/').slice(0,-1).join(''));
  }
  function clearDropHighlight(){page.querySelectorAll('.fm-drop-target').forEach(node=>node.classList.remove('fm-drop-target'));}
  page.addEventListener('dragstart',event=>{
    const row=event.target.closest('[data-fm-row]');if(!row||siteID()==='server-explorer'||movePending)return;
    if(!selected.has(row.dataset.fmRow))selectOne(row.dataset.fmRow);
    internalDrag={site:siteID(),source:cp.path(),paths:picked().map(item=>item.path)};
    event.dataTransfer.effectAllowed='move';event.dataTransfer.setData('application/x-litecp-files',JSON.stringify(internalDrag));
  });
  page.addEventListener('dragend',()=>{internalDrag=null;clearDropHighlight();});
  page.addEventListener('dragover',event=>{
    if(!internalDrag)return;const target=dropDirectory(event.target);clearDropHighlight();
    if(target&&siteID()===internalDrag.site&&validMoveTarget(internalDrag.paths,target.path)){event.preventDefault();event.dataTransfer.dropEffect='move';target.node.classList.add('fm-drop-target');}
    else event.dataTransfer.dropEffect='none';
  });
  page.addEventListener('drop',async event=>{
    if(!internalDrag)return;event.preventDefault();event.stopPropagation();
    const drag=internalDrag,target=dropDirectory(event.target);internalDrag=null;clearDropHighlight();
    if(movePending||!target||siteID()!==drag.site||cp.path()!==drag.source||!validMoveTarget(drag.paths,target.path))return;
    movePending=true;
    try{
      if(!await window.LiteCPDialogs.confirm(`Move ${drag.paths.length} selected item(s) into ${displayPath(target.path)}?`))return;
      if(siteID()!==drag.site||cp.path()!==drag.source)throw new Error('Website or folder changed. Drag the items again.');
      await cp.api('/api/files/transfer',{method:'POST',body:{siteId:drag.site,paths:drag.paths,destination:target.path,operation:'move'}});
      selected.clear();folders.clear();folderErrors.clear();await refresh();cp.showToast('Items moved',`Moved ${drag.paths.length} item(s) into ${displayPath(target.path)}.`);
    }catch(error){cp.handleError(error);}finally{movePending=false;}
  },true);
  let dragDepth=0;
  panel.addEventListener("dragenter",event=>{if(!event.dataTransfer.types.includes("Files"))return;event.preventDefault();dragDepth++;panel.classList.add("fm-dragging");});
  panel.addEventListener("dragover",event=>{if(event.dataTransfer.types.includes("Files"))event.preventDefault();});
  panel.addEventListener("dragleave",()=>{if(--dragDepth<=0){dragDepth=0;panel.classList.remove("fm-dragging");}});
  panel.addEventListener("drop",event=>{event.preventDefault();dragDepth=0;panel.classList.remove("fm-dragging");upload([...event.dataTransfer.files]);});
  page.addEventListener("keydown",event=>{if(event.target.closest("input,select,textarea,[contenteditable]"))return;if((event.ctrlKey || event.metaKey)&&event.key.toLowerCase()==="a"){event.preventDefault();selected=new Set(rows().map(x=>x.path));syncSelection();}else if(event.key==="Escape"){closeMenu();selected.clear();syncSelection();}else if(event.key==="F2"&&picked().length===1){event.preventDefault();run("rename");}else if(event.key==="Delete"&&picked().length){event.preventDefault();run("delete");}});
  $("#fileSiteSelect").addEventListener("change",()=>{selected.clear();closeMenu();trashPanel.classList.add("hidden");});
  window.LiteCPFileManager={render,loading:()=>{closeMenu();selected.clear();syncSelection();page.querySelectorAll("[data-fm-action]").forEach(button=>button.disabled=true);}};
  if(cp.state() && location.hash==="#files")cp.loadFiles();
})();
