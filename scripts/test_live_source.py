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
    def test_plesk_discovery_supports_python36_subprocess(self):
        def legacy_run(args, **kwargs):
            self.assertNotIn('text', kwargs)
            self.assertNotIn('capture_output', kwargs)
            self.assertTrue(kwargs['universal_newlines'])
            self.assertEqual(args, ['/usr/sbin/plesk', 'bin', 'subscription', '--list'])
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
                self.assertEqual(result.getnames(),['public_html/index.php','mysql/user_db.sql','private_home/sitemap-app/src/Crawler.php','private_home/config.php'])

    def test_cpanel_portable_keeps_laravel_env_without_account_secrets(self):
        with tempfile.TemporaryDirectory() as directory:
            archive=pathlib.Path(directory)/'native.tar.gz'
            portable=pathlib.Path(directory)/'portable.tar.gz'
            with tarfile.open(archive,'w:gz') as target:
                for name in ['account/homedir/.env','account/homedir/.ssh/id_rsa','account/homedir/public_html/index.php']:
                    info=tarfile.TarInfo(name); info.size=4; target.addfile(info,io.BytesIO(b'test'))
            source.cpanel_portable(archive,portable)
            with tarfile.open(portable) as result:
                self.assertEqual(result.getnames(),['private_home/.env','public_html/index.php'])

    @patch.object(source.os,'geteuid',return_value=0)
    @patch.object(source.pathlib.Path,'is_file',return_value=True)
    @patch.object(source,'command')
    def test_cpanel_discovery_parses_native_account_inventory(self,command,*unused):
        command.return_value=json.dumps({'metadata':{'result':1},'data':{'acct':[{'user':'example','domain':'example.test'}]}})
        result=source.export({'platform':'cpanel','mode':'discover'})
        self.assertEqual(result['accounts'],[{'id':'example','domain':'example.test'}])
        self.assertEqual(command.call_args.args[0],['/usr/local/cpanel/bin/whmapi1','--output=json','listaccts'])

    @patch.object(source.os,'geteuid',return_value=0)
    @patch.object(source.pathlib.Path,'is_file',return_value=True)
    @patch.object(source,'command',return_value='{"metadata":{"result":0}}')
    def test_failed_provider_discovery_does_not_invent_accounts(self,*unused):
        with self.assertRaisesRegex(RuntimeError,'rejected'):
            source.export({'platform':'cpanel','mode':'discover'})

    @patch.object(source.os,'geteuid',return_value=0)
    @patch.object(source.shutil,'which',return_value='/usr/sbin/plesk')
    @patch.object(source,'command',return_value='example.test\nsecond.test\n')
    def test_plesk_subscription_inventory(self,command,*unused):
        result=source.export({'platform':'plesk','mode':'discover'})
        self.assertEqual([x['id'] for x in result['accounts']],['example.test','second.test'])
        self.assertEqual(command.call_args.args[0],['/usr/sbin/plesk','bin','subscription','--list'])

    @patch.object(source.os,'geteuid',return_value=0)
    @patch.object(source.pathlib.Path,'is_file',return_value=True)
    @patch.object(source,'command',return_value='{"metadata":{"result":1},"data":{"acct":[{"user":"example"}]}}')
    @patch.object(source.pathlib.Path,'mkdir')
    def test_unknown_account_cannot_start_source_backup(self,mkdir,*unused):
        with self.assertRaisesRegex(RuntimeError,'discovered'):
            source.export({'platform':'cpanel','mode':'capture','account':'other;command','job':'abcdefghijklmnop'})
        mkdir.assert_not_called()

    @patch.object(source.os,'geteuid',return_value=1000)
    def test_non_root_source_is_rejected(self,*unused):
        with self.assertRaisesRegex(RuntimeError,'Root SSH'):
            source.export({'platform':'cpanel','mode':'discover'})

if __name__=='__main__':
    unittest.main()
