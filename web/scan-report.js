/* Structured, report-only ClamAV results. All scanner text is rendered as text. */
(() => {
 function parse(raw='') {
  const findings=[],summary={};
  for(const line of raw.split(/\r?\n/)) {
   const found=line.match(/^(.*): (.+) FOUND\s*$/);
   if(found){const path=found[1];findings.push({path,directory:path.slice(0,path.lastIndexOf('/'))||'/',threat:found[2]});continue;}
   const item=line.match(/^([A-Za-z ]+):\s*(.+)$/);if(item)summary[item[1].trim()]=item[2].trim();
  }
  return {findings,summary};
 }
 function render(target,raw,state='completed') {
  const {findings,summary}=parse(raw),running=state==='running',error=state==='error';
  const count=summary['Infected files']??findings.length;
  const root=document.createElement('section');root.className='malware-report';
  const add=(parent,tag,text,cls)=>{const e=document.createElement(tag);e.textContent=text;if(cls)e.className=cls;parent.append(e);return e;};
  const banner=add(root,'div','',`malware-banner ${error?'scan-error':Number(count)>0?'scan-threat':running?'scan-running':'scan-clean'}`);
  add(banner,'h3',running?'Scan in progress':error?'Scan needs attention':Number(count)>0?'Potential threats detected':'Scan completed');
  add(banner,'p',running?'Checking files. Results update automatically.':error?'The scan did not finish successfully. Review the technical details below.':Number(count)>0?'Review the affected files below. No files were deleted or quarantined.':'No threats reported by this scan. Some files may have been skipped.');
  const metrics=add(root,'div','','malware-metrics');
  for(const [label,value] of [['Infected files',count],['Files scanned',summary['Scanned files']??'Not reported'],['Directories scanned',summary['Scanned directories']??'Not reported'],['Duration',summary.Time??'Not reported']]) {const box=add(metrics,'div','','malware-metric');add(box,'span',label);add(box,'strong',String(value));}
  if(findings.length){
   add(root,'h3','Affected files');
   add(root,'p','A detection can be a test sample or a false positive. Verify the file before taking action.');
   const wrap=add(root,'div','','malware-files');const table=add(wrap,'table','');const head=add(table,'thead','');const tr=add(head,'tr','');for(const label of ['File / directory','Detection','Action taken'])add(tr,'th',label);
   const body=add(table,'tbody','');for(const f of findings){const row=add(body,'tr','');const cell=add(row,'td','');add(cell,'strong',f.path.split('/').pop());add(cell,'div',f.directory,'malware-path');add(row,'td',f.threat);add(row,'td','Reported only');}
   if(Number(count)>findings.length)add(root,'p','The summary reports more detections than the retained output lists. Review the full server scan log.');
  }
  const details=add(root,'details','');add(details,'summary','Technical scan details');add(details,'pre',raw||'No scanner output yet.');
  target.hidden=false;target.classList.remove("hidden");target.replaceChildren(root);
 }
 window.LiteCPScanReport={parse,render};
})();
