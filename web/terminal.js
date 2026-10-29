(() => {
  "use strict";
  const host=document.getElementById('interactiveTerminal');if(!host)return;
  const connect=document.getElementById('terminalConnect'),disconnect=document.getElementById('terminalDisconnect'),status=document.getElementById('terminalStatus'),panel=host.closest('.terminal-panel');
  const terminal=new Terminal({cursorBlink:true,fontSize:14,fontFamily:'Consolas, "Liberation Mono", monospace',scrollback:10000,convertEol:false,theme:{background:'#0b1017',foreground:'#dce4ed',cursor:'#7dd3fc',selectionBackground:'#385672'}});
  const fit=new FitAddon.FitAddon();terminal.loadAddon(fit);terminal.open(host);
  let id=null,cursor=0,timer=null,pending='',sending=false,generation=0;
  const encode=text=>{const bytes=new TextEncoder().encode(text);let binary='';for(const byte of bytes)binary+=String.fromCharCode(byte);return btoa(binary);};
  async function api(method,body,query=''){const response=await fetch('/api/terminal/session'+query,{method,credentials:'same-origin',headers:body?{'Content-Type':'application/json'}:{},body:body?JSON.stringify(body):undefined});const value=await response.json();if(!response.ok)throw new Error(value.error||'Terminal connection failed');return value;}
  function resize(){if(!host.getBoundingClientRect().width)return;fit.fit();if(id)api('POST',{cols:terminal.cols,rows:terminal.rows},'?id='+encodeURIComponent(id)).catch(()=>{});}
  new ResizeObserver(resize).observe(host);
  async function flush(){if(sending||!id||!pending)return;sending=true;const text=pending.slice(0,12000);pending=pending.slice(text.length);try{await api('POST',{data:encode(text)},'?id='+encodeURIComponent(id));}catch(error){pending='';status.textContent=error.message;}finally{sending=false;if(pending)flush();}}
  terminal.onData(data=>{if(id){pending+=data;flush();}});
  async function poll(token){if(!id||token!==generation)return;try{const result=await api('GET',null,'?id='+encodeURIComponent(id)+'&cursor='+cursor);if(token!==generation)return;if(result.reset)terminal.write('\r\n[Older output was trimmed]\r\n');if(result.data)terminal.write(Uint8Array.from(atob(result.data),char=>char.charCodeAt(0)));cursor=result.cursor;if(result.closed){id=null;disconnect.disabled=true;connect.disabled=false;status.textContent='Shell exited';return;}timer=setTimeout(()=>poll(token),100);}catch(error){if(token!==generation)return;status.textContent=error.message;id=null;disconnect.disabled=true;connect.disabled=false;}}
  connect.onclick=async()=>{connect.disabled=true;status.textContent='Connecting…';try{const session=await api('POST',{});id=session.id;cursor=0;pending='';terminal.reset();disconnect.disabled=false;status.textContent='Connected';resize();terminal.focus();poll(++generation);}catch(error){status.textContent=error.message;connect.disabled=false;}};
  disconnect.onclick=async()=>{if(!id)return;disconnect.disabled=true;try{await api('DELETE',null,'?id='+encodeURIComponent(id));generation++;clearTimeout(timer);id=null;pending='';status.textContent='Disconnected';connect.disabled=false;}catch(error){disconnect.disabled=false;status.textContent=error.message;}};
  document.getElementById('terminalClear').onclick=()=>{terminal.clear();terminal.focus();};
  document.getElementById('terminalFullscreen').onclick=async()=>{try{if(document.fullscreenElement===panel)await document.exitFullscreen();else await panel.requestFullscreen();resize();terminal.focus();}catch(error){status.textContent=error.message;}};
  document.addEventListener('fullscreenchange',()=>{document.getElementById('terminalFullscreen').textContent=document.fullscreenElement===panel?'Exit full screen':'Full screen';resize();});
  window.addEventListener('beforeunload',()=>{if(id)fetch('/api/terminal/session?id='+encodeURIComponent(id),{method:'DELETE',credentials:'same-origin',keepalive:true}).catch(()=>{});});
})();
