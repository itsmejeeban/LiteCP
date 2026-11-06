const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
test('integrity page is admitted, grouped and loaded after dashboard',()=>{
 const index=fs.readFileSync('web/index.html','utf8');
 assert.ok(index.indexOf('integrity.js?')>index.indexOf('security-dashboard.js?'));
 assert.match(fs.readFileSync('web/app.js','utf8'),/"security-dashboard"[^\n]*"file-integrity"/);
 assert.match(fs.readFileSync('web/sidebar-groups.js','utf8'),/"security-dashboard"[^\n]*"file-integrity"/);
 assert.match(fs.readFileSync('web/security-dashboard.js','utf8'),/href="#file-integrity"/);
 for(const tag of index.matchAll(/<script\s+([^>]+)>/g))assert.match(tag[1],/src="[^"<>]+"/);
});
test('integrity reports escape paths and paginate',()=>{
 const source=fs.readFileSync('web/integrity.js','utf8'),start=source.indexOf(' function report(){'),end=source.indexOf(' function draw()',start);
 const elements={};const context={data:{settings:{changes:[{path:'<script>bad</script>',kind:'modified'},...Array.from({length:55},(_,i)=>({path:'/etc/config-'+i,kind:'added'}))]}},offset:0,esc:value=>String(value).replaceAll('<','&lt;'),$:selector=>elements[selector]??(elements[selector]={})};
 vm.runInNewContext(source.slice(start,end)+';this.report=report;',context);context.report();
 assert.doesNotMatch(elements['[data-report]'].innerHTML,/<script>/);assert.match(elements['[data-count]'].textContent,/1–50 of 56/);assert.equal(elements['[data-prev]'].disabled,true);assert.equal(elements['[data-next]'].disabled,false);
 context.offset=50;context.report();assert.equal(elements['[data-next]'].disabled,true);assert.equal(elements['[data-prev]'].disabled,false);
});
