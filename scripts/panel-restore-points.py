"""Manage LiteCP update recovery workspaces under the panel-update lock."""

import fcntl
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import tempfile
from datetime import datetime, timezone


ROOT = Path("/usr/local/lib/litecp-updates")
LOCK = Path("/run/lock/litecp-panel-update.lock")
WORKSPACE = re.compile(r"update-[A-Za-z0-9]{8}")
LIMIT_FILE = "restore-point-limit"
VERIFIED_FILE = "verified-success"
UPDATE_LOCK_FD = 9


class Conflict(ValueError):
    pass


def trusted(info, kind):
    return kind(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o022


def verify_directory(path):
    if not trusted(path.lstat(), stat.S_ISDIR):
        raise ValueError("Unsafe ownership or permissions on update storage.")


def read_small_file(path, max_bytes):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as file:
        if not trusted(os.fstat(file.fileno()), stat.S_ISREG):
            raise ValueError("Unsafe ownership or permissions on a restore point file.")
        data = file.read(max_bytes + 1)
    if len(data) > max_bytes:
        raise ValueError("Restore point file exceeds its maximum size.")
    return data.decode("utf-8")


def parse_limit(text):
    if not re.fullmatch(r"(?:[0-9]|10)", text):
        raise ValueError("Maximum restore points must be a whole number from 0 to 10.")
    return int(text)


def read_limit():
    try:
        return parse_limit(read_small_file(ROOT / LIMIT_FILE, 3).removesuffix("\n"))
    except FileNotFoundError:
        return 0


def write_limit(limit):
    with tempfile.NamedTemporaryFile(mode="w", dir=ROOT, delete=False) as file:
        temporary = Path(file.name)
        try:
            file.write(f"{limit}\n")
            file.flush()
            os.fsync(file.fileno())
            os.replace(temporary, ROOT / LIMIT_FILE)
        finally:
            temporary.unlink(missing_ok=True)


def workspaces():
    result = []
    for entry in ROOT.iterdir():
        if not WORKSPACE.fullmatch(entry.name):
            continue
        try:
            info = entry.lstat()
        except FileNotFoundError:
            continue
        if not trusted(info, stat.S_ISDIR):
            continue
        try:
            marker = (entry / VERIFIED_FILE).lstat()
            verified = trusted(marker, stat.S_ISREG) and marker.st_size == 0
        except FileNotFoundError:
            verified = False
        timestamp = marker.st_mtime_ns if verified else info.st_mtime_ns
        result.append((timestamp, entry, verified))
    return sorted(result, reverse=True)


def workspace(name):
    if not WORKSPACE.fullmatch(name):
        raise ValueError("Invalid restore point ID.")
    path = ROOT / name
    verify_directory(path)
    return path


def check_mounts(path):
    resolved = path.resolve(strict=True)
    if resolved.parent != ROOT.resolve(strict=True):
        raise Conflict("Restore point is outside the update storage directory.")
    # Compare mount paths as well as device IDs to catch same-device bind mounts.
    for line in Path("/proc/self/mountinfo").read_text().splitlines():
        field = line.split()[4]
        mount = Path(re.sub(r"\\([0-7]{3})", lambda match: chr(int(match[1], 8)), field))
        if mount == resolved or resolved in mount.parents:
            raise Conflict("A mounted path is inside the restore point. Cleanup was stopped.")


def remove_paths(paths, lock_fd):
    # Keep the flock held if the request times out while rm is still running.
    subprocess.run(
        ["rm", "-rf", "--one-file-system", "--", *(str(path) for path in paths)],
        check=True, capture_output=True, pass_fds=(lock_fd,),
    )


def remove_workspace(path, lock_fd):
    verify_directory(path)
    check_mounts(path)
    remove_paths([path], lock_fd)


def prune_verified(limit, lock_fd):
    verified = [path for _, path, success in workspaces() if success]
    for path in verified[limit:]:
        remove_workspace(path, lock_fd)


def finish(name):
    # The updater already holds LOCK and has recorded successful health checks.
    lock = LOCK.lstat()
    if not trusted(lock, stat.S_ISREG) or not os.path.samestat(os.fstat(UPDATE_LOCK_FD), lock):
        raise ValueError("The updater lock was not inherited.")
    path = workspace(name)
    with (path / VERIFIED_FILE).open("xb"):
        pass
    limit = read_limit()
    prune_verified(limit, UPDATE_LOCK_FD)
    if limit and path.is_dir():
        # Build artifacts are not used by either recovery path.
        check_mounts(path)
        build_artifacts = [path / entry for entry in ("go", "go-build", "next-litecp", "source.tar.gz")]
        remove_paths(build_artifacts, UPDATE_LOCK_FD)


def point_sizes(points):
    if not points:
        return {}
    try:
        sizes = subprocess.run(
            ["du", "-sx", "-B1", "--", *(str(path) for _, path, _ in points)],
            capture_output=True, text=True, timeout=8,
        ).stdout
        return {path: int(size) for size, path in (line.split("\t", 1) for line in sizes.splitlines())}
    except (OSError, subprocess.SubprocessError, ValueError):
        return {}


def inventory(warning=None):
    try:
        verify_directory(ROOT)
    except FileNotFoundError:
        print('{"limit":0,"points":[]}')
        return
    try:
        limit = read_limit()
    except (OSError, ValueError):
        limit = 0
        warning = "The saved retention limit could not be read. Save it again to resume automatic cleanup."
    points = workspaces()
    sizes = point_sizes(points)
    result = {"limit": limit, "points": []}
    if warning:
        result["warning"] = warning
    for timestamp, path, verified in points:
        try:
            manifest = json.loads(read_small_file(path / "recovery-manifest.json", 4096))
            version = manifest.get("version", "") if isinstance(manifest, dict) else ""
            if not isinstance(version, str) or not re.fullmatch(r"v?\d+\.\d+\.\d+", version):
                version = ""
        except (OSError, ValueError):
            version = ""
        result["points"].append({
            "id": path.name,
            "version": version,
            "savedAt": datetime.fromtimestamp(timestamp // 1_000_000_000, timezone.utc).isoformat(),
            "sizeBytes": sizes.get(str(path)),
            "state": "verified" if verified else "legacy",
        })
    print(json.dumps(result))


def manage(command, args):
    if command == "finish" and len(args) == 1:
        verify_directory(ROOT)
        finish(args[0])
        return
    if (command, len(args)) not in (("list", 0), ("set-limit", 1), ("delete", 1)):
        raise ValueError("Invalid restore point command.")
    if command == "list":
        # Measuring folder sizes can take seconds. Never hold the update lock
        # for inventory, and tolerate folders disappearing during cleanup.
        inventory()
        return
    warning = None
    fd = os.open(LOCK, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
    with os.fdopen(fd, "rb+") as lock:
        if not trusted(os.fstat(lock.fileno()), stat.S_ISREG):
            raise ValueError("Unsafe ownership or permissions on the panel update lock.")
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise Conflict("A panel update or recovery operation is running. Try again when it finishes.") from None
        if command == "set-limit":
            limit = parse_limit(args[0])
            ROOT.mkdir(mode=0o700, exist_ok=True)
        verify_directory(ROOT)
        if command == "set-limit":
            write_limit(limit)
            try:
                prune_verified(limit, lock.fileno())
            except (OSError, ValueError, subprocess.SubprocessError):
                warning = "The retention limit was saved, but some verified restore points could not be removed."
        elif command == "delete":
            remove_workspace(workspace(args[0]), lock.fileno())
    inventory(warning)


if __name__ == "__main__":
    try:
        if os.geteuid() != 0:
            raise ValueError("Restore point management requires root privileges.")
        if len(sys.argv) < 2:
            raise ValueError("Restore point command is required.")
        manage(sys.argv[1], sys.argv[2:])
    except Conflict as error:
        print(error, file=sys.stderr)
        sys.exit(3)
    except FileNotFoundError:
        print("Restore point storage or the selected restore point no longer exists.", file=sys.stderr)
        sys.exit(4)
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        print(f"Could not manage restore points. {error}", file=sys.stderr)
        sys.exit(1)
