import types, importlib.util, pathlib, tempfile, unittest, zipfile, json, hashlib, os
spec=importlib.util.spec_from_file_location('snapshot',pathlib.Path(__file__).with_name('system-snapshot.py'));s=importlib.util.module_from_spec(spec);spec.loader.exec_module(s)
class SnapshotTests(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory();self.root=pathlib.Path(self.tmp.name);s.BASE=self.root/'snapshots';s.BASE.mkdir();s.ROOTS={'panel':str(self.root/'panel'),'websites':str(self.root/'websites')};s.EXCLUDE={};s.osinfo=lambda:('testos','1')
  for p in s.ROOTS.values():pathlib.Path(p).mkdir()
  pathlib.Path(s.ROOTS['panel'],'config.json').write_text('{"admin":{"username":"qa","passwordHash":"pbkdf2-sha256$600000$00000000000000000000000000000000$0000000000000000000000000000000000000000000000000000000000000000"},"sites":[],"databases":[],"mailboxes":[]}');pathlib.Path(s.ROOTS['websites'],'index.php').write_text('<?php echo "snapshot";')
  s.config=lambda:json.loads(pathlib.Path(s.ROOTS['panel'],'config.json').read_text());s.managed_configs=lambda:[]
 def tearDown(self):self.tmp.cleanup()
 def test_creation_keeps_panel_proxy_running(self):
  original=s.php_units
  try:
   s.php_units=lambda:['php83-php-fpm.service']
   units=s.creation_pause_units()
   self.assertNotIn('nginx',units)
   self.assertNotIn('litecp',units)
   self.assertIn('php83-php-fpm.service',units)
   self.assertIn('dovecot',units)
  finally:s.php_units=original
 def test_partial_pause_retains_recovery_list(self):
  original=s.run;original_which=s.shutil.which;ids=[]
  try:
   s.shutil.which=lambda _:True
   def command(args,**kw):
    if args[1]=='ps':return b'first litecp-app-one\nsecond litecp-app-two\n'
    if args[1]=='stop' and args[-1]=='second':raise RuntimeError('stop failed')
    if args[1]=='inspect':return b'running'
    return b''
   s.run=command
   with self.assertRaises(RuntimeError):s.pause_containers(ids)
   self.assertEqual(ids,['first','second'])
  finally:s.run=original;s.shutil.which=original_which
 def test_stop_error_after_exit_is_verified(self):
  original=s.run;original_which=s.shutil.which
  try:
   s.shutil.which=lambda _:True
   def command(args,**kw):
    if args[1]=='ps':return b'first litecp-app-one\n'
    if args[1]=='stop':raise RuntimeError('conmon exited')
    return b'exited'
   s.run=command;self.assertEqual(s.pause_containers(),['first'])
   self.assertEqual(s.resume_containers(['first']),['first'])
  finally:s.run=original;s.shutil.which=original_which
 def test_round_trip_checksums_modes_and_stage(self):
  path=s.BASE/'test.zip';s.create(path);stage=self.root/'stage';stage.mkdir();m=s.inspect(path,stage);self.assertEqual(m['format'],'litecp-system-snapshot');self.assertEqual((stage/'data/websites/index.php').read_text(),'<?php echo "snapshot";')
 def test_live_quota_image_is_not_captured(self):
  pathlib.Path(s.ROOTS['panel'],'website-storage.ext4').write_bytes(b'live filesystem image')
  path=s.BASE/'quota.zip';s.create(path);s.inspect(path)
  with zipfile.ZipFile(path) as z:
   self.assertNotIn('data/panel/website-storage.ext4',z.namelist())
   self.assertIn('data/websites/index.php',z.namelist())
 def test_legacy_snapshot_cannot_replace_live_quota_image(self):
  path=s.BASE/'original.zip';s.create(path);other=s.BASE/'legacy.zip';name='data/panel/website-storage.ext4';data=b'live filesystem image'
  with zipfile.ZipFile(path) as source,zipfile.ZipFile(other,'w') as dest:
   for item in source.infolist():
    content=source.read(item)
    if item.filename=='manifest.json':
     m=json.loads(content);m['files'][name]={'mode':0o600,'uid':0,'gid':0,'size':len(data),'sha256':hashlib.sha256(data).hexdigest()};content=json.dumps(m).encode()
    dest.writestr(item,content)
   dest.writestr(name,data)
  with self.assertRaisesRegex(ValueError,'Invalid data location'):s.inspect(other)
 def test_snapshot_settings_do_not_replay_source_upload(self):
  settings=pathlib.Path(s.ROOTS['panel'],'snapshot-settings.json');settings.write_text(json.dumps({'enabled':True,'pending':'old.zip','uploadStatus':'Uploading','destination':{'host':'backup.example'}}),encoding='utf-8')
  path=s.BASE/'test.zip';s.create(path);s.inspect(path)
  with zipfile.ZipFile(path) as z:captured=json.loads(z.read('data/panel/snapshot-settings.json'))
  self.assertFalse(captured['enabled']);self.assertNotIn('pending',captured);self.assertEqual(captured['destination']['host'],'backup.example')
  self.assertTrue(json.loads(settings.read_text())['enabled'])
 def test_rejects_corrupt_and_unlisted_members(self):
  path=s.BASE/'test.zip';s.create(path)
  with zipfile.ZipFile(path,'a') as z:z.writestr('../../escape','bad')
  with self.assertRaises(ValueError):s.inspect(path)
 def test_rejects_external_symlink(self):
  pathlib.Path(s.ROOTS['websites'],'escape').symlink_to('/etc/shadow')
  with self.assertRaises(ValueError):s.create(s.BASE/'test.zip')
 def test_rejects_os_mismatch(self):
  path=s.BASE/'test.zip';s.create(path);s.osinfo=lambda:('other','1')
  with self.assertRaises(ValueError):s.inspect(path)
 def test_restore_replaces_only_fixture_and_retains_recovery(self):
  path=s.BASE/'test.zip';s.create(path);pathlib.Path(s.ROOTS['websites'],'index.php').write_text('destination old data')
  original_run=s.run;original_sub=s.subprocess.run;original_php=s.php_units;original_pause=s.pause_containers
  try:
   s.run=lambda *a,**kw:b'';s.subprocess.run=lambda *a,**kw:types.SimpleNamespace(returncode=1,stdout=b'');s.php_units=lambda:[];s.pause_containers=lambda ids=None:[]
   result=s.restore(path,'RESTORE ENTIRE LITECP')
   self.assertTrue(result['restored']);self.assertEqual(pathlib.Path(s.ROOTS['websites'],'index.php').read_text(),'<?php echo "snapshot";');self.assertTrue(pathlib.Path(result['recovery']).exists())
  finally:s.run=original_run;s.subprocess.run=original_sub;s.php_units=original_php;s.pause_containers=original_pause
 def test_restore_failure_recovers_fixture_files(self):
  path=s.BASE/'test.zip';s.create(path);pathlib.Path(s.ROOTS['websites'],'index.php').write_text('destination old data')
  original_run=s.run;original_sub=s.subprocess.run;original_php=s.php_units;original_pause=s.pause_containers
  try:
   def command(args,**kw):
    if args==['nginx','-t']:raise RuntimeError('test failure')
    return b''
   s.run=command;s.subprocess.run=lambda *a,**kw:types.SimpleNamespace(returncode=1,stdout=b'');s.php_units=lambda:[];s.pause_containers=lambda ids=None:[]
   with self.assertRaises(RuntimeError):s.restore(path,'RESTORE ENTIRE LITECP')
   self.assertEqual(pathlib.Path(s.ROOTS['websites'],'index.php').read_text(),'destination old data')
  finally:s.run=original_run;s.subprocess.run=original_sub;s.php_units=original_php;s.pause_containers=original_pause
 def test_requires_exact_restore_confirmation(self):
  with self.assertRaises(ValueError):s.restore(s.BASE/'none.zip','yes')
 def test_rejects_configuration_manifest_injection(self):
  path=s.BASE/'test.zip';s.create(path);other=s.BASE/'bad.zip'
  with zipfile.ZipFile(path) as source,zipfile.ZipFile(other,'w') as dest:
   for f in source.infolist():
    data=source.read(f)
    if f.filename=='manifest.json':m=json.loads(data);m['configs']=['/etc/shadow'];data=json.dumps(m).encode()
    dest.writestr(f,data)
  with self.assertRaises(ValueError):s.inspect(other)
if __name__=='__main__':unittest.main()
