import importlib.util
import pathlib
import sys
import types
import unittest
from unittest.mock import patch
# Windows validation does not execute the Linux-only lock or worker entry point.
if sys.platform == 'win32': sys.modules['fcntl'] = types.SimpleNamespace()
spec=importlib.util.spec_from_file_location('system_update',pathlib.Path(__file__).with_name('system-update.py'))
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
class SystemUpdateTests(unittest.TestCase):
 def test_dnf_version_comparison(self):
  def command(args,*unused):
   if args[0]=='rpm': return 'nginx.x86_64\t0:1.26.3-1\nopenssl.x86_64\t1:3.2.0-1\n'
   if 'repoquery' in args: return 'nginx.x86_64\t0:1.26.3-2\n'
   return ''
  with patch.object(module,'manager',return_value='dnf'),patch.object(module,'run',side_effect=command),patch.object(module,'save'):
   result=module.inventory();items={item['name']:item for item in result['items']}
   self.assertTrue(items['nginx.x86_64']['updateAvailable']);self.assertEqual(items['nginx.x86_64']['latest'],'0:1.26.3-2');self.assertFalse(items['openssl.x86_64']['updateAvailable'])
 def test_apt_selected_preserves_configs_and_forbids_removal(self):
  with patch.object(module,'inventory',return_value={'manager':'apt','items':[{'name':'nginx','updateAvailable':True}]}),patch.object(module,'run') as run:
   module.update({'packages':['nginx']});args=run.call_args.args[0]
   self.assertIn('--no-remove',args);self.assertIn('--only-upgrade',args);self.assertIn('Dpkg::Options::=--force-confold',args);self.assertEqual(args[-1],'nginx')
 def test_rejects_unknown_package_and_injection(self):
  with patch.object(module,'inventory',return_value={'manager':'dnf','items':[{'name':'nginx','updateAvailable':True}]}),patch.object(module,'run') as run:
   for name in ['--allowerasing','nginx; reboot','unknown']:
    with self.assertRaises(RuntimeError): module.update({'packages':[name]})
   run.assert_not_called()
 def test_source_waf_blocks_nginx_transaction(self):
  inventory={'manager':'dnf','items':[{'name':'nginx.x86_64','updateAvailable':True,'blocked':True},{'name':'openssl.x86_64','updateAvailable':True,'blocked':False}]}
  with patch.object(module,'inventory',return_value=inventory),patch.object(module,'run') as run:
   module.update({});self.assertIn('--exclude=nginx*',run.call_args.args[0])
   with self.assertRaises(RuntimeError):module.update({'packages':['nginx.x86_64']})
 def test_no_updates_runs_no_transaction(self):
  with patch.object(module,'inventory',return_value={'manager':'dnf','items':[]}),patch.object(module,'run') as run:
   module.update({});run.assert_not_called()
 def test_schedule_rejects_invalid_time(self):
  with self.assertRaises(RuntimeError):module.schedule({'frequency':'daily','time':'03:00\nreboot'})
if __name__=='__main__':unittest.main()
