/* Advanced file workspace; all mutations use LiteCP's confined server APIs. */
(() => {
  "use strict";
  function normalizePath(value) {
    const path = String(value).trim().replace(/^\/+|\/+$/g, "");
    if (/[\x00-\x1f\\]/.test(path) || path.split("/").includes("..")) throw new Error("Choose a path inside this website. Parent traversal is not allowed.");
    return path === "public" ? "" : path.startsWith("public/") ? path.slice(7) : path;
  }
  function kind(entry) {
    if (entry.type === "directory") return "Folder";
    const ext = entry.name.includes(".") ? entry.name.split(".").pop().toLowerCase() : "";
    if (["zip","gz","tar","tgz"].includes(ext)) return "Archive";
    if (["png","jpg","jpeg","gif","svg","webp","ico","avif"].includes(ext)) return "Image";
    return ext ? ext.toUpperCase() + " file" : "File";
  }
  function prepareEntries(entries, query, hidden, sort, descending) {
    return entries.filter(x => (hidden || !x.name.startsWith(".")) && x.name.toLowerCase().includes(query.toLowerCase())).sort((a,b) => {
      if (a.type !== b.type) return a.type === "directory" ? -1 : 1;
      let order = sort === "size" ? a.size-b.size : sort === "modified" ? new Date(a.modified)-new Date(b.modified) : sort === "type" ? kind(a).localeCompare(kind(b)) : a.name.localeCompare(b.name, undefined, {numeric:true});
      return (order || a.name.localeCompare(b.name)) * (descending ? -1 : 1);
    });
  }
  if (typeof module !== "undefined" && module.exports) module.exports = {normalizePath,kind,prepareEntries};
  if (typeof document === "undefined") return;
  const cp = window.LiteCP, $ = s => document.querySelector(s), esc = cp.escapeHTML;
  const page = $('[data-page-view="files"]'), panel = $(".file-manager-panel");
  const heading = page.querySelector(".page-heading");
  heading.querySelector(".eyebrow")?.remove();
  heading.querySelector("p")?.remove();
  const fullscreen = document.createElement("button");
  fullscreen.type = "button"; fullscreen.className = "button button-secondary fm-fullscreen-toggle";
  fullscreen.textContent = "Full screen"; fullscreen.setAttribute("aria-pressed", "false");
  heading.append(fullscreen);
  const setFullscreen = enabled => {
    page.classList.toggle("fm-fullscreen", enabled);
    document.body.classList.toggle("fm-fullscreen-open", enabled);
    fullscreen.textContent = enabled ? "Exit full screen" : "Full screen";
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
    list:'M8 5h13M8 12h13M8 19h13M3 5h1M3 12h1M3 19h1', grid:'M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z', more:'M5 12h.01M12 12h.01M19 12h.01', close:'m6 6 12 12M18 6 6 18', info:'M12 11v6M12 7h.01M21 12a9 9 0 1 0-18 0 9 9 0 0 0 18 0'
  };
  const icon = name => `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="${paths[name] || paths.file}"/></svg>`;
  const button = (action,label,name,extra="") => `<button type="button" class="fm-tool ${extra}" data-fm-action="${action}" title="${label}">${icon(name)}<span>${label}</span></button>`;
  let listing = null, selected = new Set(), folders = new Map(), expanded = new Set([""]), loadingFolders = new Set(), folderErrors = new Map(), site = "", view = "list", descending = false, history = [], historyIndex = -1, historyNavigation = false, lastIndex = -1;
  const siteID = () => $("#fileSiteSelect").value;
  const rows = () => prepareEntries(listing?.entries || [], $("#fileSearch").value, $("#showHidden").checked, $("#fileSort").value, descending);
  const picked = () => (listing?.entries || []).filter(x => selected.has(x.path));
  const join = (a,b) => a ? `${a}/${b}` : b;
  page.classList.add("fm-page");
  $(".file-toolbar").innerHTML = `${button("new-file","New file","file")}${button("new-folder","New folder","folder")}${button("upload","Upload","upload","fm-primary")}<span class="fm-divider"></span>${button("copy","Copy","copy")}${button("move","Move","move")}${button("rename","Rename","edit")}${button("chmod","Permissions","lock")}${button("archive","Compress","archive")}${button("extract","Extract","extract")}${button("delete","Trash","trash","fm-danger")}<span class="fm-divider"></span>${button("refresh","Refresh","refresh")}${button("trash","View trash","trash")}<input class="hidden" id="fileUploadInput" type="file" multiple>`;
  $(".file-breadcrumb").insertAdjacentHTML("beforebegin", `<form class="fm-address" id="fmAddressForm"><div class="fm-navigation">${button("back","Back","back")}${button("forward","Forward","next")}${button("up","Up one folder","up")}</div><label>${icon("folder")}<input id="fmAddress" aria-label="Folder path" placeholder="/public" autocomplete="off" spellcheck="false"></label><button class="button button-secondary" type="submit">Go</button></form>`);
  const filters = $(".file-filters");
  filters.innerHTML = `<label class="fm-search">${icon("search")}<input id="fileSearch" type="search" placeholder="Search this folder…" aria-label="Search this folder"></label><label class="fm-hidden"><input type="checkbox" id="showHidden" checked> Hidden files</label><select id="fileSort" aria-label="Sort files"><option value="name">Name</option><option value="type">Type</option><option value="modified">Modified</option><option value="size">Size</option></select><button type="button" class="fm-order" id="fmOrder" aria-label="Reverse sort order">↑</button><div class="fm-view-switch" role="group" aria-label="File view">${button("list","List view","list")}${button("grid","Grid view","grid")}</div>`;
  const body = document.createElement("div"); body.className = "fm-body";
  body.innerHTML = `<aside class="fm-folders" aria-label="Folder navigation"><div class="fm-sidebar-title">FOLDERS <span>Website root</span></div><div id="fmTree" role="tree" aria-label="Website folders"></div><div class="fm-sidebar-note">${icon("lock")}Confined to this website</div></aside><div class="fm-content" tabindex="0" aria-label="Files in current folder"><div class="fm-selection"><label><input id="fmSelectVisible" type="checkbox" aria-label="Select all items in this folder"><span id="fmSelectionLabel">No items selected</span></label><div>${button("edit","Edit","edit")}${button("download","Download","download")}${button("clear","Clear","close")}</div></div><div id="fmFileHost"></div><div class="fm-drop-hint">${icon("upload")}Drop up to 4 files here to upload</div></div><aside class="fm-inspector" aria-label="File details" id="fmInspector"></aside>`;
  panel.append(body); $("#fmFileHost").append($("#fileTable"));
  panel.insertAdjacentHTML("beforeend", '<footer class="fm-footer"><span id="fmStatus" role="status">Select a website to begin.</span><span>Double-click to open · F2 to rename</span></footer>');
  const trashPanel = $("#trashPanel");
  trashPanel.innerHTML = `<div class="panel-head"><div><h2>Trash</h2><p>Recoverable items stored outside your public website.</p></div><div class="feature-actions">${button("close-trash","Close","close")}${button("empty-trash","Empty trash","trash","fm-danger")}</div></div><div id="trashTable"></div>`;
  const menu = document.createElement("div"); menu.className = "fm-context hidden"; menu.setAttribute("role","menu"); menu.setAttribute("aria-label","File actions"); document.body.append(menu);
  const dialog = document.createElement("dialog"); dialog.className = "fm-dialog"; document.body.append(dialog);
  dialog.setAttribute("aria-labelledby","fmDialogTitle");
  dialog.addEventListener("cancel", event => { if (dialog.querySelector('button[type="submit"]')?.disabled) event.preventDefault(); });
  function closeMenu() { menu.classList.add("hidden"); }
  function syncSelection() {
    const items = picked(), one = items.length === 1, current = items[0];
    $("#fmSelectionLabel").textContent = items.length ? `${items.length} selected · ${cp.formatBytes(items.reduce((sum,x)=>sum+(x.type === "file" ? x.size : 0),0))}` : "Select items to manage them";
    page.querySelectorAll("[data-fm-action]").forEach(el => {
      const a = el.dataset.fmAction;
      if (["copy","move","archive","delete","clear"].includes(a)) el.disabled = !items.length;
      if (["rename","chmod","edit","download","extract"].includes(a)) el.disabled = !one || (["edit","download"].includes(a) && current?.type !== "file") || (a === "extract" && !current?.name.toLowerCase().endsWith(".zip"));
    });
    $("#fmInspector").innerHTML = one ? `<div class="fm-detail-icon ${current.type === "directory" ? "is-folder" : ""}">${icon(current.type === "directory" ? "folder" : kind(current)==="Archive" ? "archive" : "file")}</div><h3>${esc(current.name)}</h3><span class="fm-kind">${esc(kind(current))}</span><dl><dt>Location</dt><dd>/public/${esc(current.path)}</dd><dt>Size</dt><dd>${current.type === "directory" ? "Folder" : cp.formatBytes(current.size)}</dd><dt>Permissions</dt><dd><code>${esc(current.mode)}</code></dd><dt>Modified</dt><dd>${esc(cp.formatDateTime(current.modified))}</dd></dl><p>Changes apply immediately to this website.</p>` : `<div class="fm-detail-icon">${icon(items.length ? "copy" : "info")}</div><h3>${items.length ? `${items.length} items selected` : "File details"}</h3><p>${items.length ? "Use the toolbar to copy, move, compress or move the selection to Trash." : "Select a file or folder to view its details and available actions."}</p>`;
    page.querySelectorAll("[data-fm-row]").forEach(row => row.classList.toggle("fm-selected",selected.has(row.dataset.fmRow)));
    page.querySelectorAll("[data-file-select]").forEach(input => input.checked=selected.has(input.value));
    const visible = rows();
    [$("#fmSelectAll"),$("#fmSelectVisible")].filter(Boolean).forEach(all=>{ all.checked = visible.length > 0 && visible.every(x=>selected.has(x.path)); all.indeterminate = !all.checked && visible.some(x=>selected.has(x.path)); });
  }
  function render(value) {
    const changed = !listing || listing.siteId !== value.siteId || listing.path !== value.path;
    if (site !== value.siteId) { folders.clear(); expanded=new Set([""]); folderErrors.clear(); loadingFolders.clear(); history=[]; historyIndex=-1; site=value.siteId; }
    if (changed) { selected.clear(); lastIndex=-1; if (!historyNavigation) { history=history.slice(0,historyIndex+1); history.push(value.path || ""); historyIndex=history.length-1; } historyNavigation=false; }
    listing=value; selected=new Set([...selected].filter(path=>(value.entries || []).some(x=>x.path===path)));
    historyNavigation=false;
    if(history[historyIndex] !== (value.path || "")){const historyMatch=history.lastIndexOf(value.path || "");if(historyMatch>=0)historyIndex=historyMatch;}
    page.querySelectorAll("[data-fm-action]").forEach(button=>button.disabled=false);
    folders.set(value.path || "",(value.entries || []).filter(x=>x.type==="directory"));
    const visible=rows(), count=(value.entries || []).length;
    $("#fmAddress").value="/public"+(value.path ? "/"+value.path : "");
    const parts=(value.path || "").split("/").filter(Boolean);
    $("#fileBreadcrumb").innerHTML=`<button data-file-path="">${icon("folder")}public</button>`+parts.map((part,i)=>`<span>/</span><button data-file-path="${esc(parts.slice(0,i+1).join("/"))}">${esc(part)}</button>`).join("");
    page.querySelector('[data-fm-action="up"]').disabled=!value.path;
    page.querySelector('[data-fm-action="back"]').disabled=historyIndex<=0;
    page.querySelector('[data-fm-action="forward"]').disabled=historyIndex>=history.length-1;
    page.querySelector('[data-fm-action="list"]').setAttribute("aria-pressed",String(view==="list"));
    page.querySelector('[data-fm-action="grid"]').setAttribute("aria-pressed",String(view==="grid"));
    $("#fileTable").classList.toggle("fm-grid-view",view==="grid");
    const entryIcon = entry => `<span class="fm-entry-icon ${entry.type === "directory" ? "is-folder" : kind(entry)==="Archive" ? "is-archive" : ""}">${icon(entry.type==="directory" ? "folder" : kind(entry)==="Archive" ? "archive" : "file")}</span>`;
    const name = entry => `<button type="button" class="fm-name" data-fm-open="${esc(entry.path)}" title="${entry.type==="directory" ? "Open folder" : "Select file; double-click to edit"}">${entryIcon(entry)}<span>${esc(entry.name)}</span></button>`;
    if (!visible.length) $("#fileTable").innerHTML=`<div class="fm-empty">${icon($("#fileSearch").value ? "search" : "folder")}<h3>${count ? "No matching items" : "This folder is empty"}</h3><p>${count ? "Try another search or show hidden files." : "Upload files or create a new folder to get started."}</p></div>`;
    else if (view==="grid") $("#fileTable").innerHTML=`<div class="fm-cards">${visible.map(x=>`<article class="fm-card" data-fm-row="${esc(x.path)}" tabindex="0"><input type="checkbox" data-file-select value="${esc(x.path)}" aria-label="Select ${esc(x.name)}">${name(x)}<small>${x.type==="directory" ? "Folder" : cp.formatBytes(x.size)}</small><button class="fm-more" type="button" data-fm-menu="${esc(x.path)}" aria-label="Actions for ${esc(x.name)}">${icon("more")}</button></article>`).join("")}</div>`;
    else $("#fileTable").innerHTML=`<table class="fm-table"><thead><tr><th><input id="fmSelectAll" type="checkbox" aria-label="Select all visible items"></th><th>Name</th><th>Size</th><th>Type</th><th>Permissions</th><th>Modified</th><th><span class="sr-only">Actions</span></th></tr></thead><tbody>${visible.map(x=>`<tr data-fm-row="${esc(x.path)}" tabindex="0"><td><input type="checkbox" data-file-select value="${esc(x.path)}" aria-label="Select ${esc(x.name)}"></td><td>${name(x)}</td><td>${x.type==="directory" ? "—" : cp.formatBytes(x.size)}</td><td>${esc(kind(x))}</td><td><button type="button" class="fm-mode" data-fm-permissions="${esc(x.path)}" title="Change permissions">${esc(x.mode)}</button></td><td>${esc(cp.formatDateTime(x.modified))}</td><td><button type="button" class="fm-more" data-fm-menu="${esc(x.path)}" aria-label="Actions for ${esc(x.name)}">${icon("more")}</button></td></tr>`).join("")}</tbody></table>`;
    $("#fmStatus").textContent=`${visible.length} of ${count} items · ${(value.entries || []).filter(x=>x.type==="directory").length} folders · ${cp.formatBytes((value.entries || []).reduce((n,x)=>n+(x.type==="file" ? x.size : 0),0))} in files`;
    $("#fmOrder").textContent=descending ? "↓" : "↑";
    syncSelection(); renderTree();
    if (!folders.has("") && !loadingFolders.has("")) loadFolder("");
  }
  function renderTree() {
    function branch(path,label,depth) {
      const open=expanded.has(path), children=folders.get(path), error=folderErrors.get(path);
      return `<div role="treeitem" aria-expanded="${open}" aria-selected="${listing?.path===path}" class="fm-tree-item"><div class="fm-tree-row ${listing?.path===path ? "active" : ""}" style="--depth:${depth}"><button type="button" data-fm-expand="${esc(path)}" aria-label="${open ? "Collapse" : "Expand"} ${esc(label)}">${open ? "⌄" : "›"}</button><button type="button" data-fm-folder="${esc(path)}">${icon("folder")}<span>${esc(label)}</span></button></div>${open ? `<div role="group">${children ? children.map(child=>branch(child.path,child.name,depth+1)).join("") : `<small class="fm-tree-note">${error ? esc(error) : "Loading folders…"}</small>`}</div>` : ""}</div>`;
    }
    $("#fmTree").innerHTML=branch("","public",0);
  }
  async function loadFolder(path) {
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
    menu.innerHTML=[["edit","Edit","edit"],["download","Download","download"],["rename","Rename","edit"],["copy","Copy to…","copy"],["move","Move to…","move"],["chmod","Permissions","lock"],["archive","Compress","archive"],["extract","Extract ZIP","extract"],["delete","Move to Trash","trash"]].map(([action,label,name])=>button(action,label,name,action==="delete" ? "fm-danger" : "")).join("");
    menu.querySelectorAll("button").forEach(b=>{ const peer=page.querySelector(`[data-fm-action="${b.dataset.fmAction}"]`); b.disabled=peer?.disabled || false; b.setAttribute("role","menuitem"); });
    menu.classList.remove("hidden"); menu.style.left=Math.min(x,window.innerWidth-menu.offsetWidth-12)+"px"; menu.style.top=Math.min(y,window.innerHeight-menu.offsetHeight-12)+"px"; menu.querySelector("button:not(:disabled)")?.focus();
  }
  function openDialog({title,description,fields="",submit="Save",danger=false,perform}) {
    if (dialog.open) return;
    const expectedSite=siteID(), expectedPath=cp.path();
    dialog.innerHTML=`<form><header><div><span class="eyebrow">FILE MANAGER</span><h2 id="fmDialogTitle">${esc(title)}</h2></div><button type="button" data-fm-cancel aria-label="Close dialog">${icon("close")}</button></header><div class="fm-dialog-body"><p>${esc(description)}</p>${fields}<p class="fm-error hidden" role="alert"></p></div><footer><button type="button" class="button button-ghost" data-fm-cancel>Cancel</button><button type="submit" class="button ${danger ? "fm-danger-button" : "button-primary"}">${esc(submit)}</button></footer></form>`;
    dialog.querySelectorAll("[data-fm-cancel]").forEach(b=>b.onclick=()=>dialog.close());
    dialog.querySelector("form").onsubmit=async event=>{
      event.preventDefault(); if (!event.target.reportValidity()) return;
      const buttons=dialog.querySelectorAll("button"); buttons.forEach(b=>b.disabled=true); dialog.querySelector(".fm-error").classList.add("hidden");
      try { if (siteID()!==expectedSite || cp.path()!==expectedPath) throw new Error("The website or folder changed. Close this dialog and try again."); await perform(new FormData(event.target)); dialog.close(); }
      catch(error) { const output=dialog.querySelector(".fm-error"); output.textContent=error.message; output.classList.remove("hidden"); }
      finally { buttons.forEach(b=>b.disabled=false); }
    };
    dialog.showModal(); dialog.querySelector("input:not([type=checkbox])")?.focus();
  }
  const field = (label,name,value="",help="") => `<label class="fm-field"><span>${esc(label)}</span><input name="${name}" value="${esc(value)}" autocomplete="off" ${["name","mode"].includes(name) ? "required" : ""}>${help ? `<small>${esc(help)}</small>` : ""}</label>`;
  function filename(value) { const name=String(value).trim(); if (!name || name==="." || name===".." || /[/\\\x00-\x1f]/.test(name)) throw new Error("Enter a file name without slashes or control characters."); return name; }
  async function refresh() { closeMenu(); await cp.loadFiles(); }
  async function action(name) {
    closeMenu(); const items=picked(), item=items[0], activeSite=siteID(), currentPath=cp.path();
    if (!activeSite) { cp.showToast("Choose a website","Add or select a website before managing files.","error"); return; }
    if (["copy","move","archive","delete"].includes(name) && !items.length) return;
    if (["rename","chmod","edit","download","extract"].includes(name) && items.length!==1) return;
    const api = (url,method,body) => cp.api(url,{method,body:{siteId:activeSite,...body}});
    if (name==="refresh") { folders.clear(); folderErrors.clear(); await refresh(); return; }
    if (name==="clear") { selected.clear(); syncSelection(); return; }
    if (name==="list" || name==="grid") { view=name; if (listing) render(listing); return; }
    if (name==="up") { await cp.goToFilePath(currentPath.split("/").slice(0,-1).join("/")); return; }
    if (name==="back" || name==="forward") { const next=historyIndex+(name==="back" ? -1 : 1); if (next<0 || next>=history.length) return; historyIndex=next; historyNavigation=true; await cp.goToFilePath(history[next]); return; }
    if (name==="edit") { if (item.type==="file") cp.openEditor(item.path); return; }
    if (name==="download") { if (!cp.demo && item.type==="file") window.location.assign(`/api/files/download?siteId=${encodeURIComponent(activeSite)}&path=${encodeURIComponent(item.path)}`); return; }
    if (name==="upload") { $("#fileUploadInput").click(); return; }
    if (name==="trash") { await showTrash(); return; }
    if (name==="close-trash") { trashPanel.classList.add("hidden"); return; }
    if (name==="new-folder" || name==="new-file") {
      const isFolder=name==="new-folder";
      openDialog({title:isFolder ? "New folder" : "New file",description:`Create inside /public${currentPath ? "/"+currentPath : ""}.`,fields:field("Name","name",isFolder ? "new-folder" : "new-file.txt"),submit:"Create",perform:async form=>{const result=await api(isFolder ? "/api/files/directory" : "/api/files/create","POST",{path:currentPath,name:filename(form.get("name"))}); await refresh(); cp.showToast(isFolder ? "Folder created" : "File created",result.path || String(form.get("name")));}}); return;
    }
    if (name==="rename") { openDialog({title:"Rename item",description:item.name,fields:field("New name","name",item.name),submit:"Rename",perform:async form=>{await api("/api/files/rename","POST",{path:item.path,destination:join(item.path.split("/").slice(0,-1).join("/"),filename(form.get("name")))}); await refresh();}}); return; }
    if (name==="copy" || name==="move") { openDialog({title:name==="copy" ? "Copy items" : "Move items",description:`${items.length} selected item(s). Existing destination names are never overwritten.`,fields:field("Destination folder","destination",currentPath,"Relative to /public. Leave empty for the website root. The folder must already exist."),submit:name==="copy" ? "Copy" : "Move",perform:async form=>{await api("/api/files/transfer","POST",{paths:items.map(x=>x.path),destination:normalizePath(form.get("destination")),operation:name}); folders.clear(); await refresh();}}); return; }
    if (name==="chmod") {
      openDialog({title:"File permissions",description:`${item.name}. Changes apply to this item only. Special modes and world-writable permissions are blocked.`,fields:field("Octal permissions","mode",item.mode)+`<div class="fm-permission-grid"><strong></strong><strong>Read</strong><strong>Write</strong><strong>Execute</strong>${["Owner","Group","Others"].map((label,index)=>`<strong>${label}</strong>${[4,2,1].map(bit=>`<input type="checkbox" data-mode-bit="${bit << ((2-index)*3)}" aria-label="${label} ${bit===4 ? "read" : bit===2 ? "write" : "execute"}">`).join("")}`).join("")}</div>`,submit:"Apply permissions",perform:async form=>{const mode=String(form.get("mode"));if (!/^0?[0-7]{3}$/.test(mode) || (parseInt(mode,8)&2)) throw new Error("Use three octal digits without write permission for Others.");await api("/api/files/permissions","PUT",{path:item.path,mode});await refresh();}});
      const modeInput=dialog.querySelector('[name="mode"]'), boxes=[...dialog.querySelectorAll("[data-mode-bit]")];
      const update=()=>boxes.forEach(box=>box.checked=Boolean(parseInt(modeInput.value,8)&Number(box.dataset.modeBit))); update(); modeInput.oninput=update;
      boxes.forEach(box=>box.onchange=()=>{modeInput.value="0"+boxes.reduce((mode,b)=>mode+(b.checked ? Number(b.dataset.modeBit) : 0),0).toString(8).padStart(3,"0");}); return;
    }
    if (name==="archive") { openDialog({title:"Compress selection",description:`Create a ZIP containing ${items.length} selected item(s).`,fields:field("Archive name","name","archive.zip"),submit:"Create ZIP",perform:async form=>{let target=filename(form.get("name"));if (!target.toLowerCase().endsWith(".zip"))target+=".zip";await api("/api/files/archive","POST",{paths:items.map(x=>x.path),destination:join(currentPath,target)});await refresh();}}); return; }
    if (name==="extract") { openDialog({title:"Extract ZIP archive",description:`Extract ${item.name}. Files with matching names may be replaced. Review the destination before continuing.`,fields:field("Destination folder","destination",currentPath,"Relative to /public. Empty means the website root."),submit:"Extract archive",perform:async form=>{await api("/api/files/extract","POST",{path:item.path,destination:normalizePath(form.get("destination"))});folders.clear();await refresh();}}); return; }
    if (name==="delete") { openDialog({title:"Move selection to Trash?",description:`Move ${items.length} item(s) to private Trash storage. You can restore them later.`,fields:`<ul class="fm-confirm-list">${items.map(x=>`<li>${esc(x.name)}</li>`).join("")}</ul>`,submit:"Move to Trash",danger:true,perform:async()=>{try {for(const entry of items)await api("/api/files","DELETE",{path:entry.path});} finally {folders.clear();await refresh();}}}); return; }
    if (name==="empty-trash") openDialog({title:"Permanently empty Trash?",description:"Every item in this website’s Trash will be permanently removed. This cannot be undone.",fields:field("Type EMPTY to confirm","confirmation"),submit:"Delete permanently",danger:true,perform:async form=>{if(form.get("confirmation")!=="EMPTY")throw new Error("Type EMPTY exactly to confirm.");await api("/api/files/trash","DELETE",{id:""});await showTrash();}});
  }
  async function showTrash() {
    trashPanel.classList.remove("hidden"); $("#trashTable").innerHTML='<div class="fm-empty"><p>Loading Trash…</p></div>'; trashPanel.scrollIntoView({behavior:"smooth",block:"nearest"});
    try {const result=cp.demo ? {entries:[]} : await cp.api(`/api/files/trash?siteId=${encodeURIComponent(siteID())}`);$("#trashTable").innerHTML=result.entries.length ? `<table class="fm-table"><thead><tr><th>Original location</th><th>Size</th><th>Moved to Trash</th><th>Actions</th></tr></thead><tbody>${result.entries.map(x=>`<tr><td>${esc(x.originalPath)}</td><td>${cp.formatBytes(x.size)}</td><td>${esc(cp.formatDateTime(x.deletedAt))}</td><td><button class="button button-secondary" data-fm-restore="${esc(x.id)}">Restore</button><button class="button fm-danger" data-fm-purge="${esc(x.id)}">Delete permanently</button></td></tr>`).join("")}</tbody></table>` : '<div class="fm-empty"><h3>Trash is empty</h3><p>Items moved to Trash will appear here.</p></div>';}
    catch(error){$("#trashTable").textContent=error.message;}
  }
  function upload(files) {
    if (!files.length) return;
    if (files.length>4 || files.some(file=>file.size>512*1024*1024)) {cp.showToast("Upload limit","Choose up to 4 files, each no larger than 512 MiB.","error");return;}
    const current=cp.path(), activeSite=siteID();
    if (!activeSite) return;
    const conflicts=files.filter(file=>listing?.entries.some(x=>x.name===file.name));
    openDialog({title:"Upload files",description:`Destination: /public${current ? "/"+current : ""}.${conflicts.length ? " Existing names detected: a single-file upload may replace its existing file; multi-file uploads reject conflicts." : " Review the files before uploading."}`,fields:`<ul class="fm-upload-list">${files.map(file=>`<li>${icon("file")}<span>${esc(file.name)}</span><small>${cp.formatBytes(file.size)}</small></li>`).join("")}</ul><progress id="fmUploadProgress" value="0" max="100"></progress><p id="fmUploadStatus" role="status">Ready to upload ${files.length} file(s).</p>`,submit:"Upload files",perform:()=>new Promise((resolve,reject)=>{
      if (cp.demo) {reject(new Error("Uploads are unavailable in demo mode."));return;}
      const data=new FormData();data.append("siteId",activeSite);data.append("path",current);files.forEach(file=>data.append("files",file,file.name));
      const xhr=new XMLHttpRequest();xhr.open("POST","/api/files/upload");xhr.responseType="json";
      xhr.upload.onprogress=event=>{if(event.lengthComputable){const percent=Math.round(event.loaded/event.total*100);$("#fmUploadProgress").value=percent;$("#fmUploadStatus").textContent=percent===100 ? "Transfer complete. Saving files…" : `Uploading… ${percent}%`;}};
      xhr.onerror=()=>reject(new Error("Connection interrupted. Refresh the folder to check uploaded files."));
      xhr.onload=async()=>{if(xhr.status<200 || xhr.status>=300){reject(new Error(xhr.response?.error || "Upload failed. Refresh the folder and try again."));return;}try{await refresh();cp.showToast("Upload complete",`${files.length} file(s) uploaded.`);resolve();}catch(error){reject(error);}};
      xhr.send(data);
    })});
  }
  function run(name) { action(name).catch(error=>cp.handleError(error)); }
  page.addEventListener("click",event=>{
    const control=event.target.closest("[data-fm-action]");if(control){event.preventDefault();event.stopImmediatePropagation();run(control.dataset.fmAction);return;}
    const folder=event.target.closest("[data-fm-folder]");if(folder){cp.goToFilePath(folder.dataset.fmFolder);return;}
    const expand=event.target.closest("[data-fm-expand]");if(expand){const path=expand.dataset.fmExpand;if(expanded.has(path))expanded.delete(path);else{expanded.add(path);if(!folders.has(path))loadFolder(path);}renderTree();return;}
    const more=event.target.closest("[data-fm-menu]");if(more){showMenu(more.dataset.fmMenu,event.clientX,event.clientY);return;}
    const permissions=event.target.closest("[data-fm-permissions]");if(permissions){selectOne(permissions.dataset.fmPermissions);run("chmod");return;}
    const row=event.target.closest("[data-fm-row]");if(row){const entry=listing.entries.find(x=>x.path===row.dataset.fmRow);if(event.target.closest("[data-fm-open]") && entry.type==="directory"){cp.goToFilePath(entry.path);return;}choose(entry.path,event);return;}
    const restore=event.target.closest("[data-fm-restore]"), purge=event.target.closest("[data-fm-purge]");
    if(restore || purge){const activeSite=siteID();openDialog({title:restore ? "Restore from Trash" : "Permanently delete item?",description:restore ? "Restore to its original location, or enter a new path. The parent folder must exist; existing files are not overwritten." : "This item will be permanently removed and cannot be recovered.",fields:restore ? field("Restore path (optional)","destination") : field("Type DELETE to confirm","confirmation"),submit:restore ? "Restore item" : "Delete permanently",danger:!restore,perform:async form=>{if(purge && form.get("confirmation")!=="DELETE")throw new Error("Type DELETE exactly to confirm.");await cp.api("/api/files/trash",{method:restore ? "POST" : "DELETE",body:{siteId:activeSite,id:(restore || purge).dataset[restore ? "fmRestore" : "fmPurge"],destination:restore ? normalizePath(form.get("destination")) : ""}});await showTrash();await refresh();}});}
  },true);
  menu.addEventListener("click",event=>{const b=event.target.closest("[data-fm-action]");if(b)run(b.dataset.fmAction);});
  menu.addEventListener("keydown",event=>{if(event.key==="Escape"){closeMenu();$(".fm-content").focus();}else if(event.key==="ArrowDown" || event.key==="ArrowUp"){event.preventDefault();const buttons=[...menu.querySelectorAll("button:not(:disabled)")],index=buttons.indexOf(document.activeElement);buttons[(index+(event.key==="ArrowDown" ? 1 : buttons.length-1))%buttons.length]?.focus();}});
  document.addEventListener("click",event=>{if(!event.target.closest(".fm-context,[data-fm-menu]"))closeMenu();});
  window.addEventListener("resize",closeMenu);window.addEventListener("scroll",closeMenu,true);
  $("#fileTable").addEventListener("dblclick",event=>{const row=event.target.closest("[data-fm-row]");if(!row || event.target.closest("input,button.fm-more,.fm-mode"))return;selectOne(row.dataset.fmRow);const entry=picked()[0];if(entry.type==="directory")cp.goToFilePath(entry.path);else run("edit");});
  $("#fileTable").addEventListener("contextmenu",event=>{const row=event.target.closest("[data-fm-row]");if(!row)return;event.preventDefault();showMenu(row.dataset.fmRow,event.clientX,event.clientY);});
  $("#fileTable").addEventListener("change",event=>{if(event.target.id==="fmSelectAll"){selected=event.target.checked ? new Set(rows().map(x=>x.path)) : new Set();syncSelection();}});
  $("#fmSelectVisible").onchange=event=>{selected=event.target.checked ? new Set(rows().map(x=>x.path)) : new Set();syncSelection();};
  $("#fmAddressForm").onsubmit=event=>{event.preventDefault();try{cp.goToFilePath(normalizePath($("#fmAddress").value));}catch(error){cp.handleError(error);}};
  ["fileSearch","showHidden","fileSort"].forEach(id=>$("#"+id).addEventListener(id==="fileSearch" ? "input" : "change",()=>{selected.clear();lastIndex=-1;if(id==="fileSort")descending=["size","modified"].includes($("#fileSort").value);if(listing)render(listing);}));
  $("#fmOrder").onclick=()=>{descending=!descending;$("#fmOrder").textContent=descending ? "↓" : "↑";if(listing)render(listing);};
  $("#fileUploadInput").onchange=event=>{upload([...event.target.files]);event.target.value="";};
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
