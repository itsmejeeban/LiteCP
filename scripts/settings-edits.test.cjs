const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync(require('node:path').join(__dirname,'../web/app.js'),'utf8');
test('state refresh preserves dirty settings and updates untouched forms',()=>{
 const controls={};for(const key of ['#settingsEmail','#settingsTimezone','#settingsRetention']){const form={dataset:{}};controls[key]={value:'draft',closest:()=>form};}
 controls['#settingsEmail'].closest().dataset.dirty='true';
 const code=source.slice(source.indexOf('  function fillSettings('),source.indexOf('  function navigate('));
 const c=vm.createContext({$:key=>controls[key]});vm.runInContext(code+';this.fill=fillSettings;',c);
 c.fill({email:'saved@example.test',timezone:'UTC',backupRetention:0});
 assert.equal(controls['#settingsEmail'].value,'draft');assert.equal(controls['#settingsTimezone'].value,'UTC');assert.equal(controls['#settingsRetention'].value,0);
});
