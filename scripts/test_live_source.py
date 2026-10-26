import importlib.util
import io
import tarfile
import tempfile
import json
import pathlib
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('live_source',pathlib.Path(__file__).with_name('live-source.py'))
source = importlib.util.module_from_spec(spec)
spec.loader.exec_module(source)

class LiveSourceTests(unittest.TestCase):
    def test_cpanel_portable_keeps_website_sql_and_excludes_mail_links(self):
        with tempfile.TemporaryDirectory() as directory:
            archive=pathlib.Path(directory)/'native.tar.gz'
            portable=pathlib.Path(directory)/'portable.tar.gz'
            with tarfile.open(archive,'w:gz') as target:
                for name in ['cpmove-user/homedir/public_html/index.php','cpmove-user/mysql/user_db.sql','cpmove-user/homedir/mail/new/1:2,S','cpmove-user/mysql.sql']:
                    info=tarfile.TarInfo(name);info.size=4;target.addfile(info,io.BytesIO(b'test'))
                link=tarfile.TarInfo('cpmove-user/homedir/public_html/link');link.type=tarfile.SYMTYPE;link.linkname='/etc/shadow';target.addfile(link)
            self.assertEqual(source.cpanel_portable(archive,portable),(1,1))
            with tarfile.open(portable) as result:
                self.assertEqual(result.getnames(),['public_html/index.php','mysql/user_db.sql'])

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
