import types, importlib.util, pathlib, tempfile, unittest, zipfile, json, hashlib, os
spec=importlib.util.spec_from_file_location('snapshot',pathlib.Path(__file__).with_name('system-snapshot.py'));s=importlib.util.module_from_spec(spec);spec.loader.exec_module(s)
class SnapshotTests(unittest.TestCase):
 def setUp(self):
  self.tmp=tempfile.TemporaryDirectory();self.root=pathlib.Path(self.tmp.name);s.BASE=self.root/'snapshots';s.BASE.mkdir();s.ROOTS={'panel':str(self.root/'panel'),'websites':str(self.root/'websites')};s.EXCLUDE={};s.osinfo=lambda:('testos','1')
  self.original_generated=s.GENERATED_CONFIGS;s.GENERATED_CONFIGS=[str(self.root/'generated'/pathlib.Path(p).name) for p in self.original_generated]
  for p in s.ROOTS.values():pathlib.Path(p).mkdir()
  pathlib.Path(s.ROOTS['panel'],'config.json').write_text('{"admin":{"username":"qa","passwordHash":"pbkdf2-sha256$600000$00000000000000000000000000000000$0000000000000000000000000000000000000000000000000000000000000000"},"sites":[],"databases":[],"mailboxes":[]}');pathlib.Path(s.ROOTS['websites'],'index.php').write_text('<?php echo "snapshot";')
  s.config=lambda:json.loads(pathlib.Path(s.ROOTS['panel'],'config.json').read_text());s.managed_configs=lambda:[]
 def tearDown(self):s.GENERATED_CONFIGS=self.original_generated;self.tmp.cleanup()
 def test_argon2_administrator_snapshot_inspection(self):
  import base64
  encoded=lambda value:base64.b64encode(value).decode().rstrip('=')
  password='$argon2id$v=19$m=19456,t=2,p=1$'+encoded(bytes(16))+'$'+encoded(bytes(32))
  config_path=pathlib.Path(s.ROOTS['panel'],'config.json');cfg=json.loads(config_path.read_text());cfg['admin']['passwordHash']=password;config_path.write_text(json.dumps(cfg))
  path=s.BASE/'argon2.zip';s.create(path);self.assertEqual(s.inspect(path)['format'],'litecp-system-snapshot')
 def test_administrator_hash_format_bounds(self):
  import base64
  encoded=lambda value:base64.b64encode(value).decode().rstrip('=')
  valid='$argon2id$v=19$m=19456,t=2,p=1$'+encoded(bytes(16))+'$'+encoded(bytes(32))
  self.assertTrue(s.supported_admin_password_hash(valid))
  for value in [None,{},'',valid.replace('v=19','v=16'),valid.replace('m=19456','m=19455'),valid.replace('m=19456','m=65537'),valid.replace('m=19456','m=019456'),valid.replace('t=2','t=6'),valid.replace('p=1','p=0'),valid.replace('p=1','p=5'),valid+'=',valid[:-1]+'B',valid.replace(encoded(bytes(16)),encoded(bytes(15)),1)]:
   with self.subTest(value=value):self.assertFalse(s.supported_admin_password_hash(value))
  legacy='pbkdf2-sha256$600000$'+'0'*32+'$'+'0'*64
  self.assertTrue(s.supported_admin_password_hash(legacy))
  self.assertFalse(s.supported_admin_password_hash(legacy.replace('600000','99999')))
  self.assertFalse(s.supported_admin_password_hash(legacy.replace('600000','2000001')))
 def test_missing_service_accounts_are_checked_before_replacement(self):
  from unittest.mock import patch
  path=s.BASE/'preflight.zip';s.create(path)
  with patch.object(s,'validate_restore_accounts',side_effect=ValueError('Install required service account opendkim')),patch.object(s,'config',side_effect=AssertionError('destination processing must not start')):
   with self.assertRaisesRegex(ValueError,'required service account opendkim'):s.restore(path,'RESTORE ENTIRE LITECP')
  self.assertEqual(pathlib.Path(s.ROOTS['websites'],'index.php').read_text(),'<?php echo "snapshot";')
 def test_optional_account_requirement_follows_snapshot_contents(self):
  from unittest.mock import patch
  stage=self.root/'preflight';(stage/'data/mail-config/dkim').mkdir(parents=True)
  with patch('pwd.getpwnam',side_effect=KeyError):
   with self.assertRaisesRegex(ValueError,'required service account opendkim'):s.validate_restore_accounts(stage)
  with patch('pwd.getpwnam') as lookup:s.validate_restore_accounts(stage);lookup.assert_called_once_with('opendkim')
 def test_mysql_preflight_rejects_unmanaged_collisions_and_root(self):
  from unittest.mock import patch
  cfg={'databases':[{'name':'qa_import','username':'qa_user'}]}
  with patch.object(s,'mysql_existing',return_value=['qa_import']):
   with self.assertRaisesRegex(ValueError,'unmanaged MySQL'):s.preflight_mysql_replacement(cfg,{})
  with self.assertRaisesRegex(ValueError,'database user'):s.mysql_managed_objects({'databases':[{'name':'qa_import','username':'root'}]})
  with self.assertRaisesRegex(ValueError,'managed database'):s.mysql_managed_objects({'databases':[{'name':'mysql','username':'qa_user'}]})
 def test_mysql_rollback_retains_failed_data_before_drop(self):
  from unittest.mock import patch
  source={'databases':[{'name':'qa_import','username':'qa_user'}]};rollback=self.root/'sql-recovery';rollback.mkdir();recovery=self.root/'mysql-recovery.zip'
  with zipfile.ZipFile(recovery,'x') as archive:
   archive.writestr('mysql/users.sql','-- original users');archive.writestr('mysql/databases.sql','-- original databases')
  commands=[]
  def command(args,**kw):
   query=kw.get('input',b'').decode();commands.append(query)
   if query.startswith('SHOW CREATE USER'):return b"CREATE USER 'qa_user'@'localhost' IDENTIFIED BY 'fixture';\nGRANT USAGE ON *.* TO 'qa_user'@'localhost'\n"
   if query.startswith('DROP '):
    self.assertEqual((rollback/'failed-restored-databases.sql').read_bytes(),b'-- failed database retained')
    self.assertIn('CREATE USER',(rollback/'failed-restored-users.sql').read_text())
   return b''
  def dump(args,**kw):kw['stdout'].write(b'-- failed database retained');kw['stdout'].flush();return types.SimpleNamespace(returncode=0)
  with patch.object(s,'mysql_existing',side_effect=lambda kind,names:['qa_import'] if kind=='databases' else ['qa_user']),patch.object(s,'run',side_effect=command),patch.object(s.subprocess,'run',side_effect=dump):s.rollback_mysql(source,{},recovery,rollback)
  self.assertIn('DROP DATABASE IF EXISTS `qa_import`;',commands)
  self.assertIn("DROP USER IF EXISTS 'qa_user'@'localhost';",commands)
  self.assertEqual(commands[-2:],['-- original users','-- original databases'])
 def test_failed_sql_backup_prevents_rollback_drop(self):
  from unittest.mock import patch
  with patch.object(s,'mysql_existing',return_value=['qa_import']),patch.object(s.subprocess,'run',side_effect=RuntimeError('backup failed')),patch.object(s,'run') as command:
   with self.assertRaisesRegex(RuntimeError,'backup failed'):s.rollback_mysql({'databases':[{'name':'qa_import','username':'qa_user'}]},{},self.root/'none.zip',self.root)
  command.assert_not_called()
 def test_restore_preserves_optional_destination_configuration_missing_from_source(self):
  from unittest.mock import patch
  path=s.BASE/'old-source.zip';s.create(path)
  optional=self.root/'optional-signer.conf';optional.write_text('destination signer config')
  with patch.object(s,'SERVICE_CONFIGS',[str(optional)]),patch.object(s,'managed_configs',return_value=[str(optional)]),patch.object(s,'run',return_value=b''),patch.object(s.subprocess,'run',return_value=types.SimpleNamespace(returncode=1,stdout=b'')),patch.object(s,'php_units',return_value=[]),patch.object(s,'pause_containers',return_value=[]):
   self.assertTrue(s.restore(path,'RESTORE ENTIRE LITECP')['restored'])
  self.assertEqual(optional.read_text(),'destination signer config')
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
 def test_mounted_website_root_is_preserved_on_restore_and_rollback(self):
  from unittest.mock import patch
  for fail in (False,True):
   with self.subTest(rollback=fail):
    website=pathlib.Path(s.ROOTS['websites']);marker=website/'index.php'
    config_path=pathlib.Path(s.ROOTS['panel'],'config.json');cfg=json.loads(config_path.read_text());cfg['settings']={'emailHostingEnabled':True};cfg['mailDomains']=[{'domain':'audit.example'}];config_path.write_text(json.dumps(cfg))
    marker.write_text('snapshot content');path=s.BASE/('mount-'+str(fail)+'.zip');s.create(path)
    marker.write_text('destination content');inode=website.stat().st_ino
    original_move=s.shutil.move
    def move(source,destination,*args,**kwargs):
     self.assertNotEqual(pathlib.Path(source),website,'must not move the mount point')
     return original_move(source,destination,*args,**kwargs)
    commands=[]
    def command(args,**kwargs):
     commands.append(args)
     if fail and args==['nginx','-t']:raise RuntimeError('mounted rollback test')
     return b''
    service_calls=[]
    def subprocess_run(args,**kwargs):
     service_calls.append(args)
     return types.SimpleNamespace(returncode=0 if args==['selinuxenabled'] or args[:2]==['systemctl','start'] else 1,stdout=b'')
    with patch.object(s.zipfile,'ZipFile',wraps=s.zipfile.ZipFile) as archives,patch.object(s.os.path,'ismount',side_effect=lambda p:pathlib.Path(p)==website),patch.object(s.shutil,'move',side_effect=move),patch.object(s.shutil,'which',return_value='/usr/sbin/selinuxenabled'),patch.object(s,'run',side_effect=command),patch.object(s.subprocess,'run',side_effect=subprocess_run),patch.object(s,'php_units',return_value=[]),patch.object(s,'pause_containers',return_value=[]):
     if fail:
      with self.assertRaisesRegex(RuntimeError,'mounted rollback test'):s.restore(path,'RESTORE ENTIRE LITECP')
     else:self.assertTrue(s.restore(path,'RESTORE ENTIRE LITECP')['restored'])
     self.assertLessEqual(archives.call_count,4,'archive directory must not be reopened for each restored file')
    self.assertNotIn(['systemctl','start','opendkim'],service_calls,'do not start an optional signer that is not installed')
    self.assertEqual(website.stat().st_ino,inode)
    relabel=next(i for i,args in enumerate(commands) if args[0]=='restorecon')
    self.assertLess(relabel,commands.index(['systemd-run','--scope','--quiet','/usr/local/bin/litecp','snapshot-reconcile']))
    self.assertEqual(marker.read_text(),'destination content' if fail else 'snapshot content')
 def test_rejects_configuration_manifest_injection(self):
  path=s.BASE/'test.zip';s.create(path);other=s.BASE/'bad.zip'
  with zipfile.ZipFile(path) as source,zipfile.ZipFile(other,'w') as dest:
   for f in source.infolist():
    data=source.read(f)
    if f.filename=='manifest.json':m=json.loads(data);m['configs']=['/etc/shadow'];data=json.dumps(m).encode()
    dest.writestr(f,data)
  with self.assertRaises(ValueError):s.inspect(other)
if __name__=='__main__':unittest.main()
