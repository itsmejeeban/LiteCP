(() => {
  "use strict";
  const host=document.getElementById('interactiveTerminal');if(!host)return;
  const connect=document.getElementById('terminalConnect'),disconnect=document.getElementById('terminalDisconnect'),status=document.getElementById('terminalStatus'),panel=host.closest('.terminal-panel');
  const feedback=document.createElement('span');feedback.id='terminalFeedback';feedback.setAttribute('role','status');status.after(feedback);
  const terminal=new Terminal({cursorBlink:false,cursorStyle:"block",cursorInactiveStyle:"outline",fontSize:14,fontFamily:'Consolas, "Liberation Mono", monospace',scrollback:10000,convertEol:false,theme:{background:'#0b1017',foreground:'#dce4ed',cursor:'#f8fafc',cursorAccent:'#0b1017',selectionBackground:'#385672'}});
  const fit=new FitAddon.FitAddon();terminal.loadAddon(fit);terminal.open(host);
  host.addEventListener("pointerdown",event=>{if(event.button===0)terminal.focus();});
  let id=null,cursor=0,timer=null,pending='',sending=false,generation=0,available=false;
  connect.disabled=true;status.textContent='Checking terminal availability…';
  fetch('/api/shell-capabilities',{credentials:'same-origin'}).then(async response=>{
    const value=await response.json();if(!response.ok)throw new Error(value.error||'Cannot check terminal availability. Reload the page to retry.');
    available=value.available===true;connect.disabled=!available;
    status.textContent=available?'Disconnected':value.reason||'Browser terminal is unavailable. Use SSH for shell access.';
  }).catch(error=>{status.textContent=error.message;});
  const menu=document.createElement('div');menu.id='terminalContextMenu';menu.hidden=true;menu.setAttribute('role','menu');
  menu.innerHTML='<button type="button" role="menuitem" data-terminal-menu="select">Select All</button><button type="button" role="menuitem" data-terminal-menu="copy">Copy <small>Ctrl+C</small></button><button type="button" role="menuitem" data-terminal-menu="paste">Paste <small>Ctrl+V</small></button>';
  panel.append(menu);
  function closeMenu(){menu.hidden=true;}
  async function copySelection(){
    const text=terminal.getSelection();if(!text){feedback.textContent='Select terminal text to copy.';return;}
    try{
      if(navigator.clipboard?.writeText)await navigator.clipboard.writeText(text);
      else {
        const input=document.createElement('textarea');input.value=text;input.style.position='fixed';input.style.opacity='0';panel.append(input);input.select();
        try{if(!document.execCommand('copy'))throw new Error('Clipboard unavailable');}finally{input.remove();terminal.focus();}
      }
      feedback.textContent='Selected text copied.';
    }catch(error){feedback.textContent='Copy was blocked by the browser. Allow clipboard access and try again.';}
  }
  function manualPaste(){
    const dialog=document.createElement('dialog');dialog.className='terminal-paste-dialog';
    dialog.innerHTML='<form method="dialog"><h2>Paste into terminal</h2><p>Press Ctrl+V in the box, then choose Paste.</p><textarea aria-label="Text to paste" rows="5"></textarea><div><button value="cancel">Cancel</button><button value="paste">Paste</button></div></form>';
    panel.append(dialog);dialog.addEventListener('close',()=>{if(dialog.returnValue==='paste'&&id)terminal.paste(dialog.querySelector('textarea').value);dialog.remove();terminal.focus();});dialog.showModal();dialog.querySelector('textarea').focus();
  }
  async function pasteClipboard(){
    if(!id){feedback.textContent='Connect the terminal before pasting.';return;}
    try{const text=await navigator.clipboard.readText();if(!id)return;terminal.paste(text);terminal.focus();}
    catch(error){manualPaste();}
  }
  terminal.attachCustomKeyEventHandler(event=>{
    if(event.type==='keydown'&&(event.ctrlKey||event.metaKey)&&!event.altKey&&event.key.toLowerCase()==='c'&&terminal.hasSelection()){
      event.preventDefault();copySelection();return false;
    }
    // Leave Ctrl+V to xterm's native paste event, and Ctrl+C without a
    // selection to the shell's interrupt handler. This avoids duplicate input.
    return true;
  });
  host.addEventListener('contextmenu',event=>{
    event.preventDefault();menu.querySelector('[data-terminal-menu="copy"]').disabled=!terminal.hasSelection();menu.querySelector('[data-terminal-menu="paste"]').disabled=!id;
    menu.hidden=false;const bounds=menu.getBoundingClientRect();menu.style.left=Math.max(4,Math.min(event.clientX,window.innerWidth-bounds.width-4))+'px';menu.style.top=Math.max(4,Math.min(event.clientY,window.innerHeight-bounds.height-4))+'px';
    menu.querySelector('button:not(:disabled)').focus();
  });
  menu.addEventListener('pointerdown',event=>event.stopPropagation());
  menu.addEventListener('click',event=>{const action=event.target.closest('[data-terminal-menu]')?.dataset.terminalMenu;if(!action)return;closeMenu();if(action==='select'){terminal.selectAll();terminal.focus();}else if(action==='copy')copySelection();else pasteClipboard();});
  menu.addEventListener('keydown',event=>{if(event.key==='Escape'){closeMenu();terminal.focus();}if(event.key==='ArrowDown'||event.key==='ArrowUp'){event.preventDefault();const items=[...menu.querySelectorAll('button:not(:disabled)')],index=items.indexOf(document.activeElement);items[(index+(event.key==='ArrowDown'?1:items.length-1))%items.length]?.focus();}});
  document.addEventListener('pointerdown',event=>{if(!menu.contains(event.target))closeMenu();});window.addEventListener('resize',closeMenu);document.addEventListener('scroll',closeMenu,true);

  const encode=text=>{const bytes=new TextEncoder().encode(text);let binary='';for(const byte of bytes)binary+=String.fromCharCode(byte);return btoa(binary);};
  async function api(method,body,query=''){const response=await fetch('/api/terminal/session'+query,{method,credentials:'same-origin',headers:body?{'Content-Type':'application/json'}:{},body:body?JSON.stringify(body):undefined});const value=await response.json();if(!response.ok)throw new Error(value.error||'Terminal connection failed');return value;}
  function resize(){if(!host.getBoundingClientRect().width)return;fit.fit();if(id)api('POST',{cols:terminal.cols,rows:terminal.rows},'?id='+encodeURIComponent(id)).catch(()=>{});}
  new ResizeObserver(resize).observe(host);
  async function flush(){if(sending||!id||!pending)return;sending=true;const token=generation,session=id,text=pending.slice(0,12000);pending=pending.slice(text.length);try{await api('POST',{data:encode(text)},'?id='+encodeURIComponent(session));}catch(error){if(token===generation){pending='';status.textContent=error.message;}}finally{sending=false;if(pending)flush();}}
  terminal.onData(data=>{if(id){pending+=data;flush();}});
  async function poll(token){if(!id||token!==generation)return;try{const result=await api('GET',null,'?id='+encodeURIComponent(id)+'&cursor='+cursor);if(token!==generation)return;if(result.reset)terminal.write('\r\n[Older output was trimmed]\r\n');if(result.data)terminal.write(Uint8Array.from(atob(result.data),char=>char.charCodeAt(0)));cursor=result.cursor;if(result.closed){id=null;disconnect.disabled=true;connect.disabled=false;status.textContent='Shell exited';return;}timer=setTimeout(()=>poll(token),100);}catch(error){if(token!==generation)return;status.textContent=error.message;id=null;disconnect.disabled=true;connect.disabled=false;}}
  connect.onclick=async()=>{if(!available||connect.disabled)return;connect.disabled=true;status.textContent='Connecting…';try{const session=await api('POST',{});id=session.id;cursor=0;pending='';terminal.reset();disconnect.disabled=false;status.textContent='Connected';resize();terminal.focus();poll(++generation);}catch(error){status.textContent=error.message;connect.disabled=false;}};
  disconnect.onclick=async()=>{if(!id)return;disconnect.disabled=true;try{await api('DELETE',null,'?id='+encodeURIComponent(id));generation++;clearTimeout(timer);id=null;pending='';status.textContent='Disconnected';connect.disabled=false;}catch(error){disconnect.disabled=false;status.textContent=error.message;}};
  document.getElementById('terminalClear').onclick=()=>{terminal.clear();terminal.focus();};
  function toggleExpandedTerminal() {
    const expanded=panel.classList.toggle('terminal-expanded');
    document.getElementById('terminalFullscreen').textContent=expanded?'Exit full screen':'Full screen';
    feedback.textContent=expanded?'Expanded within this browser window. Press Escape to exit.':'';
    resize();terminal.focus();
  }
  document.getElementById('terminalFullscreen').onclick=async()=>{
    if(panel.classList.contains('terminal-expanded')){toggleExpandedTerminal();return;}
    try{
      if(document.fullscreenElement===panel)await document.exitFullscreen();else await panel.requestFullscreen();
      feedback.textContent='';resize();terminal.focus();
    }catch(error){toggleExpandedTerminal();}
  };
  document.addEventListener('keydown',event=>{if(event.key==='Escape'&&panel.classList.contains('terminal-expanded'))toggleExpandedTerminal();});
  document.addEventListener('fullscreenchange',()=>{document.getElementById('terminalFullscreen').textContent=document.fullscreenElement===panel?'Exit full screen':'Full screen';resize();});
  window.addEventListener('beforeunload',()=>{if(id)fetch('/api/terminal/session?id='+encodeURIComponent(id),{method:'DELETE',credentials:'same-origin',keepalive:true}).catch(()=>{});});
})();
