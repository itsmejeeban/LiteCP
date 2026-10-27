const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const source=fs.readFileSync('web/panel-settings.js','utf8'),start=source.indexOf('  function timezoneLabel('),end=source.indexOf('  function populateTimezones()',start);
const label=vm.runInNewContext(source.slice(start,end)+'\ntimezoneLabel',{Intl});
test('timezone labels show UTC plus/minus offsets including fractional hours',()=>{
 const date=new Date('2026-01-15T12:00:00Z');
 for(const [zone,offset] of [['UTC','+00:00'],['Asia/Kolkata','+05:30'],['Asia/Kathmandu','+05:45'],['America/St_Johns','-03:30'],['Pacific/Chatham','+13:45']])assert.equal(label(zone,date),`${zone} (UTC${offset})`);
});
test('offsets follow daylight-saving time for the requested date',()=>{
 assert.equal(label('America/New_York',new Date('2026-01-15T12:00:00Z')),'America/New_York (UTC-05:00)');
 assert.equal(label('America/New_York',new Date('2026-07-15T12:00:00Z')),'America/New_York (UTC-04:00)');
 assert.equal(label('Europe/London',new Date('2026-01-15T12:00:00Z')),'Europe/London (UTC+00:00)');
 assert.equal(label('Europe/London',new Date('2026-07-15T12:00:00Z')),'Europe/London (UTC+01:00)');
});
test('unsupported zones retain their original label instead of breaking the list',()=>{assert.equal(label('Invalid/Zone',new Date()),'Invalid/Zone');});
