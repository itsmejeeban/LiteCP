import importlib.util
import io
import tarfile
import tempfile
import json
import pathlib
import stat
import sys
import unittest
import types
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('live_source',pathlib.Path(__file__).with_name('live-source.py'))
source = importlib.util.module_from_spec(spec)
spec.loader.exec_module(source)

class LiveSourceTests(unittest.TestCase):
    def test_cpanel_supplements_only_missing_account_databases(self):
        inventory = {'result': {'status': 1, 'data': [
            {'database': 'already_exported'}, {'database': 'site_wordpress'},
            {'database': '../unsafe'},
        ]}}

        def dump(args, stdout, stderr, **kwargs):
            self.assertEqual(args[-1], 'site_wordpress')
            if '--no-tablespaces' in args:
                stdout.write(b'incomplete dump')
                stderr.write(b"unknown option '--no-tablespaces'")
                return types.SimpleNamespace(returncode=2)
            stdout.write(b'CREATE TABLE fixture(id INT);')
            return types.SimpleNamespace(returncode=0)

        warnings = []
        buffer = io.BytesIO()
        with patch.object(source, 'command', return_value=json.dumps(inventory)), \
                patch.object(source.shutil, 'which', return_value='/usr/bin/mysqldump'), \
                patch.object(source.subprocess, 'run', side_effect=dump) as run, \
                patch.object(source.tempfile, 'TemporaryFile', side_effect=lambda **kwargs: io.BytesIO()):
            with tarfile.open(fileobj=buffer, mode='w') as target:
                source.supplement_cpanel_databases(target, {'already_exported'}, 'site', None, None, warnings)
        self.assertEqual(run.call_count, 2)
        buffer.seek(0)
        with tarfile.open(fileobj=buffer) as archive:
            self.assertEqual(archive.getnames(), ['mysql/site_wordpress.sql'])
            self.assertEqual(archive.extractfile('mysql/site_wordpress.sql').read(),
                             b'CREATE TABLE fixture(id INT);')

    def test_plesk_subdomain_capture_uses_subscription_and_selected_root(self):
        def query(args, log=None):
            if args[1:]==['bin','site','--list']:
                return 'example.test\napi.example.test\n'
            if args[1:3]==['bin','pleskbackup']:
                return ''
            if 'SELECT name,login,www_root' in args[-1]:
                return 'api.example.test\towner\t/var/www/vhosts/example.test/api\n'
            self.fail('Unexpected Plesk command: '+repr(args))
        with patch.object(source.os,'geteuid',return_value=0,create=True), patch.object(source.shutil,'which',return_value='/usr/sbin/plesk'), patch.object(source,'command',side_effect=query) as command, patch.object(source,'plesk_backup_subscription',return_value='example.test'), patch.object(source,'plesk_website_databases',return_value=['api_db']) as databases, patch.object(source.pathlib.Path,'mkdir'), patch.object(source.pathlib.Path,'is_file',return_value=True), patch.object(source.pathlib.Path,'is_dir',return_value=True), patch.object(source.pathlib.Path,'is_absolute',return_value=True), patch.object(source.pathlib.Path,'is_symlink',return_value=False), patch('builtins.open',unittest.mock.mock_open()), patch.object(source.os,'chmod'), patch.object(source,'describe',return_value={'path':'fixture'}), patch.object(source.tarfile,'open') as archive, patch.object(source.subprocess,'run',return_value=types.SimpleNamespace(returncode=0)) as dump:
            result=source.export({'platform':'plesk','mode':'capture','account':'api.example.test','job':'abcdefghijklmnop'})
            backup=next(call.args[0] for call in command.call_args_list if call.args[0][1:3]==['bin','pleskbackup'])
            self.assertEqual(backup[3:5],['--domains-name','example.test'])
            self.assertIn('-keep-local-backup',backup)
            self.assertEqual(databases.call_args.args[1:3],('api.example.test','example.test'))
            self.assertEqual(databases.call_args.args[3],pathlib.Path('/var/www/vhosts/example.test/api'))
            self.assertEqual(dump.call_args.args[0][:4],['/usr/sbin/plesk','db','dump','api_db'])
            added=archive.return_value.__enter__.return_value.add.call_args_list
            self.assertEqual(added[0].args[0],pathlib.Path('/var/www/vhosts/example.test/api'))
            self.assertEqual([call.kwargs['arcname'] for call in added],['httpdocs','databases/api_db.sql'])
            self.assertIsNotNone(result['native'])
            self.assertIsNotNone(result['portable'])

    def test_cpanel_private_directory_names_are_not_account_metadata(self):
        with tempfile.TemporaryDirectory() as directory:
            archive=pathlib.Path(directory)/'native.tar.gz'
            portable=pathlib.Path(directory)/'portable.tar.gz'
            names=['cpmove-user/homedir/public_html/index.php','cpmove-user/homedir/app/public_html/index.php','cpmove-user/homedir/app/mysql/schema.sql','cpmove-user/mysql/user_db.sql','cpmove-user/other/public_html/injected.php']
            with tarfile.open(archive,'w:gz') as target:
                for name in names:
                    info=tarfile.TarInfo(name);info.size=4;target.addfile(info,io.BytesIO(b'test'))
            self.assertEqual(source.cpanel_portable(archive,portable),(1,2,0))
            with tarfile.open(portable) as result:
                self.assertEqual(result.getnames(),['public_html/index.php','private_home/app/public_html/index.php','private_home/app/mysql/schema.sql','mysql/user_db.sql','cpanel_account/format.json'])

    def test_litecp_urlsafe_identifier_reaches_capture(self):
        for account in ['-vYbtz2ZL38', '_leading_underscore']:
            config = {'sites': [{'id': account, 'domain': 'audit.example.test', 'root': '/home/litecp/audit/public_html'}]}
            with patch.object(source.os, 'geteuid', return_value=0, create=True), patch('builtins.open', unittest.mock.mock_open(read_data=json.dumps(config))), patch.object(source.pathlib.Path, 'mkdir', side_effect=RuntimeError('capture reached')):
                with self.assertRaisesRegex(RuntimeError, 'capture reached'):
                    source.export({'platform':'litecp','mode':'capture','account':account,'job':'abcdefghijklmnop'})

    def test_litecp_blank_website_capture_needs_no_database(self):
        config = {'sites': [{'id': '-blank_site', 'domain': 'blank.example.test', 'root': '/home/litecp/blank.example.test/public_html'}], 'databases': []}
        with patch.object(source.os, 'geteuid', return_value=0, create=True), patch('builtins.open', unittest.mock.mock_open(read_data=json.dumps(config))), patch.object(source.pathlib.Path, 'mkdir'), patch.object(source.pathlib.Path, 'resolve', return_value=pathlib.PurePosixPath('/home/litecp/blank.example.test/public_html')), patch.object(source.pathlib.Path, 'is_symlink', return_value=False), patch.object(source.pathlib.Path, 'is_dir', return_value=True), patch.object(source.os, 'chmod'), patch.object(source.tarfile, 'open') as archive, patch.object(source, 'describe', return_value={'path':'fixture.tar.gz','size':1,'sha256':'fixture'}), patch.object(source.subprocess, 'run') as run:
            result=source.export({'platform':'litecp','mode':'capture','account':'-blank_site','job':'abcdefghijklmnop'})
            run.assert_not_called()
            archive.return_value.__enter__.return_value.add.assert_called_once()
            self.assertEqual(archive.return_value.__enter__.return_value.add.call_args.kwargs['arcname'], 'public_html')
            self.assertEqual(result['native'],result['portable'])

    @patch.object(source, 'litecp_dump_binary', return_value='/usr/bin/mariadb-dump')
    def test_litecp_capture_exports_owned_and_primary_databases_only(self, dump_binary):
        config = {'sites':[{'id':'-audit','domain':'audit.example.test','root':'/home/litecp/audit/public_html','database':'primary_db'}], 'databases':[{'siteId':'-audit','name':'owned_db'},{'siteId':'other','name':'unrelated_db'}]}
        with patch.object(source.os,'geteuid',return_value=0,create=True), patch('builtins.open',unittest.mock.mock_open(read_data=json.dumps(config))), patch.object(source.pathlib.Path,'mkdir'), patch.object(source.pathlib.Path,'resolve',return_value=pathlib.PurePosixPath('/home/litecp/audit/public_html')), patch.object(source.pathlib.Path,'is_symlink',return_value=False), patch.object(source.pathlib.Path,'is_dir',return_value=True), patch.object(source.os,'chmod'), patch.object(source.tarfile,'open') as archive, patch.object(source,'describe',return_value={'path':'fixture.tar.gz'}), patch.object(source.subprocess,'run',return_value=types.SimpleNamespace(returncode=0)) as run:
            source.export({'platform':'litecp','mode':'capture','account':'-audit','job':'abcdefghijklmnop'})
            self.assertEqual([call.args[0][-1] for call in run.call_args_list], ['owned_db','primary_db'])
            self.assertTrue(all(call.args[0][0] == '/usr/bin/mariadb-dump' for call in run.call_args_list))
            names=[call.kwargs['arcname'] for call in archive.return_value.__enter__.return_value.add.call_args_list]
            self.assertEqual(names,['public_html','databases/owned_db.sql','databases/primary_db.sql'])

    def test_litecp_dump_tool_selection(self):
        with patch.object(source.shutil,'which',side_effect=lambda name: '/usr/bin/'+name):
            self.assertEqual(source.litecp_dump_binary(),'/usr/bin/mysqldump')
        with patch.object(source.shutil,'which',side_effect=lambda name: '/usr/bin/mariadb-dump' if name=='mariadb-dump' else None):
            self.assertEqual(source.litecp_dump_binary(),'/usr/bin/mariadb-dump')
        with patch.object(source.shutil,'which',return_value=None):
            with self.assertRaisesRegex(RuntimeError,'requires mysqldump or mariadb-dump'):
                source.litecp_dump_binary()

    def test_plesk_subdomain_selects_referenced_parent_database_only(self):
        inventory = 'securese_jk\tsecureserver.top\nother_db\tsecureserver.top\ndirect_db\tapi.secureserver.top\n'
        with patch.object(source, 'command', return_value=inventory), patch.object(source.os, 'walk', return_value=[('/website', [], ['.env'])]), patch.object(source.pathlib.Path, 'is_symlink', return_value=False), patch.object(source.pathlib.Path, 'is_file', return_value=True), patch.object(source.pathlib.Path, 'stat', return_value=types.SimpleNamespace(st_size=100)), patch.object(source.pathlib.Path, 'read_text', return_value='DB_DATABASE=securese_jk\nDB_PASSWORD=private\n'):
            self.assertEqual(source.plesk_website_databases('/usr/sbin/plesk', 'api.secureserver.top', 'secureserver.top', pathlib.Path('/website')), ['direct_db', 'securese_jk'])

    def test_plesk_database_selection_does_not_export_unrelated_schemas(self):
        with patch.object(source, 'command', return_value='parent_db\tsecureserver.top\n'), patch.object(source.os, 'walk', return_value=[]):
            self.assertEqual(source.plesk_website_databases('/usr/sbin/plesk', 'blank.secureserver.top', 'secureserver.top', pathlib.Path('/website')), [])

    def test_plesk_subdomain_backup_uses_owning_subscription(self):
        with patch.object(source, 'command', return_value='secureserver.top\n') as run:
            self.assertEqual(source.plesk_backup_subscription('/usr/sbin/plesk', 'api.secureserver.top'), 'secureserver.top')
            query = run.call_args.args[0][-1]
            self.assertIn("site.name='api.secureserver.top'", query)
            self.assertIn('site.webspace_id', query)
            self.assertIn('site.parentDomainId', query)
        with patch.object(source, 'command', return_value=''):
            with self.assertRaisesRegex(RuntimeError, 'Cannot determine'):
                source.plesk_backup_subscription('/usr/sbin/plesk', 'api.secureserver.top')

    def test_plesk_discovery_supports_python36_subprocess(self):
        def legacy_run(args, **kwargs):
            self.assertNotIn('text', kwargs)
            self.assertNotIn('capture_output', kwargs)
            self.assertTrue(kwargs['universal_newlines'])
            self.assertEqual(args, ['/usr/sbin/plesk', 'bin', 'site', '--list'])
            return types.SimpleNamespace(returncode=0, stdout='example.com\nshop.example.com\n')
        with patch.object(source.os, 'geteuid', return_value=0, create=True), patch.object(source.shutil, 'which', return_value='/usr/sbin/plesk'), patch.object(source.subprocess, 'run', side_effect=legacy_run):
            result = source.export({'platform':'plesk', 'mode':'discover'})
        self.assertEqual(result['accounts'], [{'id':'example.com','domain':'example.com'}, {'id':'shop.example.com','domain':'shop.example.com'}])


    def test_cpanel_cleanup_removes_only_created_archives(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            native = root/'cpmove-example.tar.gz'
            portable = root/'cpanel-website.tar.gz'
            log = root/'export.log'
            other = root/'unrelated-backup.tar.gz'
            for path in (native, portable, log, other):
                path.write_bytes(b'test')
            self.assertEqual(source.cleanup_cpanel_export(root, 'example'), ['cpmove-example.tar.gz', 'cpanel-website.tar.gz'])
            self.assertFalse(native.exists())
            self.assertFalse(portable.exists())
            self.assertTrue(log.exists())
            self.assertTrue(other.exists())

    def test_cpanel_cleanup_rejects_archive_symlink(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            target = root/'outside.tar.gz'
            target.write_bytes(b'test')
            try:
                (root/'cpmove-example.tar.gz').symlink_to(target)
            except OSError:
                self.skipTest('symbolic links are unavailable')
            with self.assertRaisesRegex(RuntimeError, 'symbolic-link'):
                source.cleanup_cpanel_export(root, 'example')
            self.assertTrue(target.exists())

    def test_cpanel_portable_keeps_website_sql_private_app_and_excludes_account_data(self):
        with tempfile.TemporaryDirectory() as directory:
            archive=pathlib.Path(directory)/'native.tar.gz'
            portable=pathlib.Path(directory)/'portable.tar.gz'
            with tarfile.open(archive,'w:gz') as target:
                for name in ['cpmove-user/homedir/public_html/index.php','cpmove-user/mysql/user_db.sql','cpmove-user/homedir/sitemap-app/src/Crawler.php','cpmove-user/homedir/config.php','cpmove-user/homedir/mail/new/1:2,S','cpmove-user/homedir/.ssh/id_rsa','cpmove-user/mysql.sql']:
                    info=tarfile.TarInfo(name);info.size=4;target.addfile(info,io.BytesIO(b'test'))
                link=tarfile.TarInfo('cpmove-user/homedir/public_html/link');link.type=tarfile.SYMTYPE;link.linkname='/etc/shadow';target.addfile(link)
                conflict = 'cpmove-user/homedir/public_html/wp-content/upgrade/plugin/assets'
                info = tarfile.TarInfo(conflict)
                info.size = 4
                target.addfile(info, io.BytesIO(b'test'))
                info = tarfile.TarInfo(conflict)
                info.type = tarfile.DIRTYPE
                target.addfile(info)
            self.assertEqual(source.cpanel_portable(archive,portable),(1,2,1))
            with tarfile.open(portable) as result:
                self.assertEqual(result.getnames(),['public_html/index.php','mysql/user_db.sql','private_home/sitemap-app/src/Crawler.php','private_home/config.php','cpanel_account/mail/new/1%3A2,S','cpanel_account/format.json'])

    def test_cpanel_portable_keeps_laravel_env_without_account_secrets(self):
        with tempfile.TemporaryDirectory() as directory:
            archive=pathlib.Path(directory)/'native.tar.gz'
            portable=pathlib.Path(directory)/'portable.tar.gz'
            with tarfile.open(archive,'w:gz') as target:
                for name in ['account/homedir/.env','account/homedir/.ssh/id_rsa','account/homedir/public_html/index.php']:
                    info=tarfile.TarInfo(name); info.size=4; target.addfile(info,io.BytesIO(b'test'))
            source.cpanel_portable(archive,portable)
            with tarfile.open(portable) as result:
                self.assertEqual(result.getnames(),['private_home/.env','public_html/index.php','cpanel_account/format.json'])

    def test_cpanel_portable_exports_mail_metadata_messages_and_cron(self):
        with tempfile.TemporaryDirectory() as directory:
            archive = pathlib.Path(directory)/'native.tar.gz'
            portable = pathlib.Path(directory)/'portable.tar.gz'
            entries = {
                'cpmove-user/homedir/public_html/index.php': b'website',
                'cpmove-user/homedir/etc/example.test/shadow': b'sales:hashed:',
                'cpmove-user/homedir/etc/example.test/quota': b'sales:2097152',
                'cpmove-user/homedir/mail/example.test/sales/cur/message:2,S': b'mail',
                'cpmove-user/dnszones/example.test.db': b'@ IN NS ns1.example.test.',
                'cpmove-user/cron/user': b'*/5 * * * * /usr/bin/php task.php',
                'cpmove-user/homedir/etc/private-secret': b'excluded',
                'cpmove-user/homedir/.ssh/id_rsa': b'excluded',
            }
            with tarfile.open(archive, 'w:gz') as target:
                for name, value in entries.items():
                    info = tarfile.TarInfo(name)
                    info.size = len(value)
                    target.addfile(info, io.BytesIO(value))
            source.cpanel_portable(archive, portable, '/home/user')
            with tarfile.open(portable) as result:
                names = result.getnames()
                self.assertIn('cpanel_account/etc/example.test/shadow', names)
                self.assertIn('cpanel_account/mail/example.test/sales/cur/message%3A2,S', names)
                self.assertIn('cpanel_account/cron/user', names)
                self.assertIn('cpanel_account/dns/example.test.db', names)
                self.assertFalse(any('private-secret' in name or 'id_rsa' in name for name in names))
                self.assertEqual(json.load(result.extractfile('cpanel_account/format.json')), {'version':1, 'sourceHome':'/home/user'})

    @patch.object(source.os,'geteuid',return_value=0,create=True)
    @patch.object(source.pathlib.Path,'is_file',return_value=True)
    @patch.object(source,'command')
    def test_cpanel_discovery_parses_native_account_inventory(self,command,*unused):
        command.return_value=json.dumps({'metadata':{'result':1},'data':{'acct':[{'user':'example','domain':'example.test'}]}})
        result=source.export({'platform':'cpanel','mode':'discover'})
        self.assertEqual(result['accounts'],[{'id':'example','domain':'example.test'}])
        self.assertEqual(command.call_args.args[0],['/usr/local/cpanel/bin/whmapi1','--output=json','listaccts'])

    @patch.object(source.os,'geteuid',return_value=0,create=True)
    @patch.object(source.pathlib.Path,'is_file',return_value=True)
    @patch.object(source,'command',return_value='{"metadata":{"result":0}}')
    def test_failed_provider_discovery_does_not_invent_accounts(self,*unused):
        with self.assertRaisesRegex(RuntimeError,'rejected'):
            source.export({'platform':'cpanel','mode':'discover'})

    def test_cpanel_capture_checks_only_the_selected_local_account(self):
        account = types.SimpleNamespace(pw_uid=1001, pw_dir='/home/example')
        pwd = types.SimpleNamespace(getpwnam=unittest.mock.Mock(return_value=account))
        with patch.dict(sys.modules, {'pwd': pwd}), \
                patch.object(source.os, 'geteuid', return_value=0, create=True), \
                patch.object(source.pathlib.Path, 'is_file', return_value=True, autospec=True) as is_file, \
                patch.object(source.pathlib.Path, 'is_symlink', return_value=False), \
                patch.object(source.pathlib.Path, 'mkdir'), \
                patch('builtins.open', unittest.mock.mock_open()), \
                patch.object(source.os, 'chmod'), \
                patch.object(source, 'command') as command, \
                patch.object(source, 'describe', return_value={'path':'fixture'}), \
                patch.object(source, 'cpanel_portable', return_value=(1, 0, 0)) as portable:
            result = source.export({'platform':'cpanel','mode':'capture','account':'example','job':'abcdefghijklmnop'})
        pwd.getpwnam.assert_called_once_with('example')
        self.assertEqual(is_file.call_args_list[0].args[0], pathlib.Path('/var/cpanel/users/example'))
        self.assertEqual(command.call_count, 1)
        self.assertEqual(command.call_args.args[0][:2], ['/usr/local/cpanel/scripts/pkgacct','example'])
        self.assertEqual(portable.call_args.args[2], '/home/example')
        self.assertIsNotNone(result['portable'])

    def test_cpanel_cleanup_uses_only_the_root_owned_job_directory(self):
        request = {'platform':'cpanel','mode':'cleanup','account':'example','job':'abcdefghijklmnop'}
        owner = types.SimpleNamespace(st_mode=stat.S_IFDIR | 0o700, st_uid=0)
        with patch.object(source.os, 'geteuid', return_value=0, create=True), \
                patch.object(source.pathlib.Path, 'lstat', return_value=owner), \
                patch.object(source, 'command') as command, \
                patch.object(source, 'cleanup_cpanel_export', return_value=[]) as cleanup:
            source.export(request)
            cleanup.assert_called_once_with(pathlib.Path('/root/litecp-export-abcdefghijklmnop'), 'example')
            command.assert_not_called()
            owner.st_uid = 1001
            with self.assertRaisesRegex(RuntimeError, 'unsafe'):
                source.export(request)
            self.assertEqual(cleanup.call_count, 1)

    def test_cpanel_capture_reports_pkgacct_storage_failure(self):
        with patch.object(source.os, 'geteuid', return_value=0, create=True), \
                patch.object(source, 'cpanel_account_home', return_value='/home/example'), \
                patch.object(source.pathlib.Path, 'mkdir'), \
                patch('builtins.open', unittest.mock.mock_open()), \
                patch.object(source.os, 'chmod'), \
                patch.object(source, 'command', side_effect=RuntimeError('pkgacct failed')), \
                patch.object(source.pathlib.Path, 'open', return_value=io.BytesIO(b'write: Disk quota exceeded')):
            with self.assertRaisesRegex(RuntimeError, 'cPanel capture failed.*out of disk space or quota'):
                source.export({'platform':'cpanel','mode':'capture','account':'example','job':'abcdefghijklmnop'})

    @patch.object(source.os,'geteuid',return_value=0,create=True)
    @patch.object(source.shutil,'which',return_value='/usr/sbin/plesk')
    @patch.object(source,'command',return_value='example.test\nsecond.test\nshop.example.test\nexample.test\n')
    def test_plesk_full_site_inventory(self,command,*unused):
        result=source.export({'platform':'plesk','mode':'discover'})
        self.assertEqual([x['id'] for x in result['accounts']],['example.test','second.test','shop.example.test'])
        self.assertEqual(command.call_args.args[0],['/usr/sbin/plesk','bin','site','--list'])

    @patch.object(source.os,'geteuid',return_value=0,create=True)
    @patch.object(source.pathlib.Path,'mkdir')
    def test_unsafe_account_cannot_start_source_backup(self,mkdir,*unused):
        with self.assertRaisesRegex(RuntimeError,'Invalid account identifier'):
            source.export({'platform':'cpanel','mode':'capture','account':'other;command','job':'abcdefghijklmnop'})
        mkdir.assert_not_called()

    @patch.object(source.os,'geteuid',return_value=0,create=True)
    @patch.object(source.pathlib.Path,'is_file',return_value=False)
    @patch.object(source.pathlib.Path,'mkdir')
    def test_missing_cpanel_account_cannot_start_source_backup(self,mkdir,*unused):
        with patch.dict(sys.modules, {'pwd': types.SimpleNamespace()}):
            with self.assertRaisesRegex(RuntimeError,'no longer available'):
                source.export({'platform':'cpanel','mode':'capture','account':'missing','job':'abcdefghijklmnop'})
        mkdir.assert_not_called()

    @patch.object(source.os,'geteuid',return_value=1000,create=True)
    def test_non_root_source_is_rejected(self,*unused):
        with self.assertRaisesRegex(RuntimeError,'Root SSH'):
            source.export({'platform':'cpanel','mode':'discover'})

if __name__=='__main__':
    unittest.main()
