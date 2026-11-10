import importlib.util
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch


spec = importlib.util.spec_from_file_location("restore_points", Path(__file__).with_name("panel-restore-points.py"))
restore_points = importlib.util.module_from_spec(spec)
with patch.dict(sys.modules, {"fcntl": Mock()} if sys.platform == "win32" else {}):
    spec.loader.exec_module(restore_points)


class RestorePointsTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        overrides = patch.multiple(
            restore_points,
            ROOT=self.root,
            trusted=lambda info, kind: kind(info.st_mode),
            read_small_file=lambda path, _: path.read_text(),
        )
        overrides.start()
        self.addCleanup(overrides.stop)

    def create_workspace(self, name, timestamp=None):
        path = self.root / name
        path.mkdir()
        (path / "panel-data").mkdir()
        if timestamp:
            marker = path / restore_points.VERIFIED_FILE
            marker.touch()
            os.utime(marker, ns=(timestamp, timestamp))
        return path

    def test_retention_uses_verified_time_and_preserves_legacy(self):
        older = self.create_workspace("update-ZZZZZZZZ", 1_000_000_000)
        newer = self.create_workspace("update-AAAAAAAA", 2_000_000_000)
        legacy = self.create_workspace("update-BBBBBBBB")
        (legacy / restore_points.VERIFIED_FILE).write_text("untrusted marker")
        other = self.create_workspace("rollback-CCCCCCCC")
        with patch.object(restore_points, "remove_workspace") as remove:
            restore_points.prune_verified(1, 9)
        remove.assert_called_once_with(older, 9)
        with patch.object(restore_points, "remove_workspace") as remove:
            restore_points.prune_verified(0, 9)
        self.assertEqual([call.args[0] for call in remove.call_args_list], [newer, older])
        self.assertTrue(legacy.is_dir() and other.is_dir())

    def test_finish_records_success_but_preserves_workspace_if_policy_is_invalid(self):
        path = self.create_workspace("update-1234abcd")
        (self.root / restore_points.LIMIT_FILE).write_text("invalid")
        lock = self.root / "lock"
        with lock.open("wb") as stream, patch.multiple(
            restore_points, LOCK=lock, UPDATE_LOCK_FD=stream.fileno(),
        ), patch.object(restore_points, "remove_workspace") as remove:
            with self.assertRaises(ValueError):
                restore_points.finish(path.name)
        self.assertTrue((path / restore_points.VERIFIED_FILE).is_file())
        remove.assert_not_called()

    def test_delete_rejects_a_nested_same_device_mount(self):
        path = self.create_workspace("update-1234abcd")
        mount = str(path.resolve() / "nested mount").replace("\\", "\\134").replace(" ", "\\040")
        mountinfo = f"27 20 0:25 / {mount} rw - ext4 /dev/sda1 rw\n"
        with patch.object(Path, "read_text", return_value=mountinfo), patch.object(restore_points, "remove_paths") as remove:
            with self.assertRaises(restore_points.Conflict):
                restore_points.remove_workspace(path, 9)
        remove.assert_not_called()


if __name__ == "__main__":
    unittest.main()
