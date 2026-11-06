import os, pathlib, stat, subprocess, sys, types, unittest
from unittest.mock import MagicMock, patch

source = pathlib.Path(__file__).with_name('update-panel.sh').read_text()
block = source.split('python3 - "$tag" "$work/source.tar.gz" <<\'PY\'\n', 1)[1].split('\nPY\n', 1)[0]

class PrivateUpdateDownloadTests(unittest.TestCase):
    def setUp(self):
        # The worker targets Linux; mock absent flags for Windows unit tests.
        self.flags = patch.multiple(os, O_NOFOLLOW=getattr(os,'O_NOFOLLOW',0), O_NONBLOCK=getattr(os,'O_NONBLOCK',0), O_CLOEXEC=getattr(os,'O_CLOEXEC',0), create=True)
        self.flags.start()
        self.addCleanup(self.flags.stop)

    def test_public_download_uses_https_and_no_authorization(self):
        with patch('os.open', side_effect=FileNotFoundError), patch.object(subprocess, 'run') as run, patch.object(sys, 'argv', ['worker', 'v5.0.6', '/private/source.tar.gz']):
            exec(compile(block, 'update-download', 'exec'), {})
        args = run.call_args.args[0]
        self.assertEqual(run.call_args.kwargs['input'], '')
        self.assertEqual(args[args.index('--proto-redir') + 1], '=https')
        self.assertNotIn('--location-trusted', args)
        self.assertEqual(args[-3], 'https://api.github.com/repos/itsmejeeban/LiteCP/tarball/v5.0.6')

    def test_unreadable_credential_does_not_fall_back_to_public(self):
        with patch('os.open', side_effect=PermissionError), patch.object(subprocess, 'run') as run, patch.object(sys, 'argv', ['worker', 'v5.0.6', '/private/source.tar.gz']):
            with self.assertRaises(PermissionError):
                exec(compile(block, 'update-download', 'exec'), {})
        run.assert_not_called()

    def test_private_token_goes_only_to_curl_stdin(self):
        token='test_read_only_token_123456789'
        stream=MagicMock()
        stream.__enter__.return_value=stream
        stream.read.return_value=token.encode()
        info=types.SimpleNamespace(st_mode=stat.S_IFREG|0o600,st_uid=0)
        with patch('os.open',return_value=55),patch('os.fdopen',return_value=stream),patch('os.fstat',return_value=info),patch.object(subprocess,'run') as run,patch.object(sys,'argv',['worker','v5.0.6','/private/source.tar.gz']):
            exec(compile(block,'update-download','exec'),{})
        self.assertIn('Authorization: Bearer '+token,run.call_args.kwargs['input'])
        self.assertNotIn(token,' '.join(run.call_args.args[0]))
        self.assertNotIn('env',run.call_args.kwargs)

    def test_insecure_private_credentials_are_rejected(self):
        for mode,uid,data in [(0o644,0,b'test_read_only_token_123456789'),(0o600,1,b'test_read_only_token_123456789'),(0o600,0,b'test_token_123456789\nInjectedHeader')]:
            with self.subTest(mode=mode,uid=uid):
                stream=MagicMock();stream.__enter__.return_value=stream;stream.read.return_value=data
                info=types.SimpleNamespace(st_mode=stat.S_IFREG|mode,st_uid=uid)
                with patch('os.open',return_value=55),patch('os.fdopen',return_value=stream),patch('os.fstat',return_value=info),patch.object(subprocess,'run') as run,patch.object(sys,'argv',['worker','v5.0.6','/private/source.tar.gz']):
                    with self.assertRaises(SystemExit):exec(compile(block,'update-download','exec'),{})
                run.assert_not_called()

if __name__ == '__main__':
    unittest.main()
