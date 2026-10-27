/* Visual HTML editing stays inside a script-disabled iframe. Source is kept
   byte-for-byte until the user actually edits the visual document. */
(() => {
  "use strict";
  const cp=window.LiteCP, modal=document.querySelector("#editorModal"), body=modal.querySelector(".modal-body");
  const tools=document.createElement("div");tools.className="fm-html-tools hidden";
  const controls=[["bold","B","Bold"],["italic","I","Italic"],["underline","U","Underline"],["strikeThrough","S","Strikethrough"],["insertUnorderedList","• List","Bulleted list"],["insertOrderedList","1. List","Numbered list"],["justifyLeft","⇤","Align left"],["justifyCenter","↔","Align center"],["justifyRight","⇥","Align right"],["undo","↶","Undo"],["redo","↷","Redo"]];
  tools.innerHTML=`<div class="fm-html-tabs"><button type="button" data-html-tab="visual">Visual</button><button type="button" data-html-tab="source">HTML source</button><span id="htmlEncoding"></span></div><div class="fm-html-format"><select aria-label="Text format" id="htmlBlock"><option value="p">Paragraph</option><option value="h1">Heading 1</option><option value="h2">Heading 2</option><option value="h3">Heading 3</option><option value="h4">Heading 4</option><option value="pre">Preformatted</option></select>${controls.map(([command,label,title])=>`<button type="button" data-html-command="${command}" title="${title}" aria-label="${title}">${label}</button>`).join("")}<label title="Text color">Color <input type="color" id="htmlColor" value="#000000"></label><button type="button" id="htmlLink">Link</button><button type="button" data-html-command="unlink">Unlink</button><button type="button" id="htmlImage">Image</button><button type="button" id="htmlTable">Table</button><button type="button" data-html-command="removeFormat">Clear format</button><input type="file" class="hidden" id="htmlImageFile" accept="image/png,image/jpeg,image/gif,image/webp"></div>`;
  const frame=document.createElement("iframe");frame.className="fm-html-canvas hidden";frame.title="Visual HTML editor";frame.setAttribute("sandbox","allow-same-origin");
  body.prepend(tools);body.append(frame);
  let active=false,visual=false,dirty=false,original="",path="",range=null,policyID="",loadGeneration=0,assets=[];
  const $=selector=>tools.querySelector(selector);
  function sourceValue(){return window.LiteCPFeatures?.editorValue() ?? document.querySelector("#editorContent").value;}
  function serialized(){
    if(!dirty)return original;
    const copy=frame.contentDocument.documentElement.cloneNode(true);copy.querySelector(`#${policyID}`)?.remove();
    // Browser extensions can inject their own DOM into the editing frame.
    for(const tag of ["grammarly-desktop-integration","grammarly-extension"]){if(!original.toLowerCase().includes("<"+tag))copy.querySelectorAll(tag).forEach(node=>node.remove());}
    for(const name of ["data-abp-filters-channel","data-new-gr-c-s-check-loaded","data-gr-ext-installed"]){if(!original.includes(name))for(const node of [copy,...copy.querySelectorAll(`[${name}]`)])node.removeAttribute(name);}
    for(const asset of assets){const image=copy.querySelector(`[${asset.marker}]`);if(!image)continue;if(image.getAttribute("src")===asset.preview){image.setAttribute("src",asset.source);if(asset.srcset!==null)image.setAttribute("srcset",asset.srcset);}image.removeAttribute(asset.marker);}
    const doctype=frame.contentDocument.doctype;
    return (doctype ? new XMLSerializer().serializeToString(doctype)+"\n" : "")+copy.outerHTML;
  }
  function selection(){const value=frame.contentWindow.getSelection();if(value?.rangeCount)range=value.getRangeAt(0).cloneRange();}
  async function previewLocalImages(generation){
    const siteID=document.querySelector("#fileSiteSelect").value,site=cp.state()?.sites.find(site=>site.id===siteID),publicRoot=site?.root.endsWith("/public_html") ? "public_html" : "";
    let bytes=0,index=0;
    for(const image of [...frame.contentDocument.images].slice(0,20)){
      if(generation!==loadGeneration)return;
      const source=image.getAttribute("src")||"";if(!source||source.startsWith("data:"))continue;
      try{
        const url=new URL(source,"https://litecp.invalid/"+path);if(url.origin!=="https://litecp.invalid")continue;
        let target=decodeURIComponent(url.pathname).replace(/^\//,"");if(source.startsWith("/")&&publicRoot)target=publicRoot+"/"+target;
        const response=await fetch(`/api/files/download?preview=1&siteId=${encodeURIComponent(siteID)}&path=${encodeURIComponent(target)}`,{credentials:"same-origin"});
        const size=Number(response.headers.get("Content-Length"));if(!response.ok||!/^image\/(png|jpeg|gif|webp|avif|x-icon|vnd.microsoft.icon)(;|$)/i.test(response.headers.get("Content-Type")||"")||!size||size>10*1024*1024||bytes+size>20*1024*1024)continue;
        const blob=await response.blob();bytes+=blob.size;if(generation!==loadGeneration)return;
        const preview=await new Promise(resolve=>{const reader=new FileReader();reader.onload=()=>resolve(reader.result);reader.readAsDataURL(blob);});if(generation!==loadGeneration)return;
        const marker="data-"+policyID+"-asset-"+(index++),srcset=image.getAttribute("srcset");assets.push({marker,source,srcset,preview});image.setAttribute(marker,"");image.removeAttribute("srcset");image.src=preview;
      }catch{/* Missing images remain in the source unchanged. */}
    }
  }
  function execute(command,value=null){
    if(!active||!visual)return;
    frame.contentWindow.focus();const selected=frame.contentWindow.getSelection();
    if(range&&frame.contentDocument.contains(range.commonAncestorContainer)){selected.removeAllRanges();selected.addRange(range);}
    frame.contentDocument.execCommand(command,false,value);dirty=true;selection();
  }
  async function setVisual(enabled){
    if(enabled&&visual)return;
    const generation=++loadGeneration;
    if(enabled){
      original=sourceValue();dirty=false;range=null;assets=[];
      if(/<\?(?:php|=)/i.test(original))throw new Error("This file contains PHP. Use HTML source to preserve the PHP code.");
      policyID="litecp-visual-policy-"+crypto.getRandomValues(new Uint32Array(1))[0].toString(16);
      const policy=`<meta id="${policyID}" http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'none'; img-src data:; style-src 'unsafe-inline'; form-action 'none'; base-uri 'none'; object-src 'none'">`;
      await new Promise((resolve,reject)=>{const timeout=setTimeout(()=>reject(new Error("The visual editor could not load. Use HTML source.")),10000);frame.onload=()=>{
        clearTimeout(timeout);
        if(generation!==loadGeneration){resolve();return;}
        const doc=frame.contentDocument;doc.designMode="on";
        doc.addEventListener("input",()=>{dirty=true;});doc.addEventListener("mouseup",selection);doc.addEventListener("keyup",selection);
        doc.addEventListener("click",event=>{if(event.target.closest?.("a"))event.preventDefault();});doc.addEventListener("submit",event=>event.preventDefault());resolve();previewLocalImages(generation);
      };frame.srcdoc="<!doctype html>"+policy+original;});
    }else if(visual){
      const content=serialized();await window.LiteCPFeatures?.openEditor(path,content);
      document.querySelector("#editorContent").value=content;
    }
    if(generation!==loadGeneration)return;
    visual=enabled;frame.classList.toggle("hidden",!visual);
    modal.querySelector(".editor-tools")?.classList.toggle("hidden",visual);
    document.querySelector("#aceEditor")?.classList.toggle("hidden",visual || !window.ace);
    document.querySelector("#editorContent").classList.toggle("hidden",visual || Boolean(window.ace));
    $(".fm-html-format").classList.toggle("hidden",!visual);
    tools.querySelectorAll("[data-html-tab]").forEach(button=>button.setAttribute("aria-pressed",String((button.dataset.htmlTab==="visual")===visual)));
  }
  tools.querySelectorAll("[data-html-command]").forEach(button=>button.onclick=()=>execute(button.dataset.htmlCommand));
  tools.querySelectorAll("[data-html-tab]").forEach(button=>button.onclick=()=>setVisual(button.dataset.htmlTab==="visual").catch(cp.handleError));
  $("#htmlBlock").onchange=event=>execute("formatBlock",event.target.value);
  $("#htmlColor").oninput=event=>execute("foreColor",event.target.value);
  $("#htmlLink").onclick=async()=>{const url=await window.LiteCPDialogs.prompt("Link URL:","https://");if(url===null)return;if(!/^(https?:\/\/|mailto:|tel:|#|\/|\.\.?\/)/i.test(url.trim())){cp.handleError(new Error("Use an HTTP, HTTPS, email or relative link."));return;}execute("createLink",url.trim());};
  $("#htmlImage").onclick=()=>$("#htmlImageFile").click();
  $("#htmlImageFile").onchange=async event=>{const file=event.target.files[0],generation=loadGeneration;event.target.value="";if(!file)return;if(!["image/png","image/jpeg","image/gif","image/webp"].includes(file.type)||file.size>10*1024*1024){cp.handleError(new Error("Choose a PNG, JPEG, GIF or WebP image up to 10 MiB."));return;}const reader=new FileReader();reader.onload=()=>{if(generation===loadGeneration)execute("insertImage",reader.result);};reader.readAsDataURL(file);};
  $("#htmlTable").onclick=()=>execute("insertHTML",'<table border="1"><tbody><tr><th>Heading</th><th>Heading</th></tr><tr><td>Cell</td><td>Cell</td></tr></tbody></table><p><br></p>');
  window.LiteCPVisualEditor={
    value:()=>active&&visual ? serialized() : null,
    opened:async(file,content,options={})=>{
      loadGeneration++;active=false;visual=false;dirty=false;range=null;frame.classList.add("hidden");tools.classList.add("hidden");
      modal.querySelector(".editor-tools")?.classList.remove("hidden");
      path=file;original=content;
      if(!/\.html?$/i.test(file))return;
      active=true;tools.classList.remove("hidden");$("#htmlEncoding").textContent=options.encoding||"UTF-8";
      await setVisual(Boolean(options.visual));
    }
  };
})();
