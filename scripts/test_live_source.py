import importlib.util
import io
import tarfile
import tempfile
import json
import pathlib
import unittest
import types
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('live_source',pathlib.Path(__file__).with_name('live-source.py'))
source = importlib.util.module_from_spec(spec)
spec.loader.exec_module(source)

class LiveSourceTests(unittest.TestCase):
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

    @patch.object(source.os,'geteuid',return_value=0,create=True)
    @patch.object(source.shutil,'which',return_value='/usr/sbin/plesk')
    @patch.object(source,'command',return_value='example.test\nsecond.test\nshop.example.test\nexample.test\n')
    def test_plesk_full_site_inventory(self,command,*unused):
        result=source.export({'platform':'plesk','mode':'discover'})
        self.assertEqual([x['id'] for x in result['accounts']],['example.test','second.test','shop.example.test'])
        self.assertEqual(command.call_args.args[0],['/usr/sbin/plesk','bin','site','--list'])

    @patch.object(source.os,'geteuid',return_value=0,create=True)
    @patch.object(source.pathlib.Path,'is_file',return_value=True)
    @patch.object(source,'command',return_value='{"metadata":{"result":1},"data":{"acct":[{"user":"example"}]}}')
    @patch.object(source.pathlib.Path,'mkdir')
    def test_unknown_account_cannot_start_source_backup(self,mkdir,*unused):
        with self.assertRaisesRegex(RuntimeError,'discovered'):
            source.export({'platform':'cpanel','mode':'capture','account':'other;command','job':'abcdefghijklmnop'})
        mkdir.assert_not_called()

    @patch.object(source.os,'geteuid',return_value=1000,create=True)
    def test_non_root_source_is_rejected(self,*unused):
        with self.assertRaisesRegex(RuntimeError,'Root SSH'):
            source.export({'platform':'cpanel','mode':'discover'})

if __name__=='__main__':
    unittest.main()
