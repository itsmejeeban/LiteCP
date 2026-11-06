import importlib.util
from pathlib import Path
import tempfile
import unittest

spec=importlib.util.spec_from_file_location('dns_repair',Path(__file__).with_name('repair-dns-runtime.py'))
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)

class DNSRuntimeRepairTests(unittest.TestCase):
 def test_repair_preserves_zones_backup_and_is_idempotent(self):
  with tempfile.TemporaryDirectory() as directory:
   config=Path(directory)/'named.conf'; original=b'options { directory "/etc/litecp/dns"; };\nzone "example.test" { file "/etc/litecp/dns/revision/a.zone"; };\n';config.write_bytes(original)
   checks=[];self.assertTrue(module.repair(config,lambda path:checks.append(path.read_bytes())))
   self.assertIn(b'directory "/run/litecp-dns";',config.read_bytes());self.assertIn(b'/etc/litecp/dns/revision/a.zone',config.read_bytes())
   backups=list(Path(directory).glob('dns-runtime-repair-*/previous-named.conf'));self.assertEqual(len(backups),1);self.assertEqual(backups[0].read_bytes(),original)
   self.assertFalse(module.repair(config,lambda path:self.fail('unexpected validation')));self.assertEqual(len(checks),1)
 def test_validation_failure_keeps_live_configuration(self):
  with tempfile.TemporaryDirectory() as directory:
   config=Path(directory)/'named.conf';original=b'options { directory "/etc/litecp/dns"; };';config.write_bytes(original)
   def reject(path):raise ValueError('invalid candidate')
   with self.assertRaises(ValueError):module.repair(config,reject)
   self.assertEqual(config.read_bytes(),original)
 def test_ambiguous_configuration_is_not_changed(self):
  with tempfile.TemporaryDirectory() as directory:
   config=Path(directory)/'named.conf';original=b'directory "/etc/litecp/dns"; directory "/etc/litecp/dns";';config.write_bytes(original)
   with self.assertRaises(RuntimeError):module.repair(config,lambda path:None)
   self.assertEqual(config.read_bytes(),original)

if __name__=='__main__':unittest.main()
