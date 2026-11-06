#!/usr/bin/env python3
"""Repair only the historical LiteCP DNS working-directory directive; retain backup."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile


def repair(config, validate):
    config = Path(config)
    if not config.exists():
        return False
    if config.is_symlink() or not config.is_file():
        raise RuntimeError('Refusing a symlinked or non-regular DNS configuration')
    previous = config.read_bytes()
    old = b'directory "/etc/litecp/dns";'
    if old not in previous:
        return False
    if previous.count(old) != 1:
        raise RuntimeError('Ambiguous DNS directory configuration; review manually')
    recovery = Path(tempfile.mkdtemp(prefix='dns-runtime-repair-', dir=config.parent))
    os.chmod(recovery, 0o700)
    shutil.copy2(config, recovery / 'previous-named.conf')
    staged = recovery / 'candidate-named.conf'
    staged.write_bytes(previous.replace(old, b'directory "/run/litecp-dns";', 1))
    os.chmod(staged, 0o644)
    validate(staged)
    os.replace(staged, config)
    print('DNS previous configuration retained at', recovery / 'previous-named.conf')
    return True


def main():
    if os.geteuid() != 0:
        raise SystemExit("Run as root")
    import pwd
    config = Path('/etc/litecp/dns/named.conf')
    if not config.is_file():
        return
    runtime = Path('/run/litecp-dns')
    if runtime.is_symlink():
        raise RuntimeError('Refusing a symlinked DNS runtime directory')
    runtime.mkdir(mode=0o755, exist_ok=True)
    user = next((name for name in ('named', 'bind') if _user_exists(name)), None)
    if user is None:
        raise RuntimeError('DNS service account is not installed')
    account = pwd.getpwnam(user)
    os.chown(runtime, account.pw_uid, account.pw_gid)
    changed = repair(config, lambda path: subprocess.run(['named-checkconf', str(path)], check=True))
    # RuntimeDirectory is re-owned before every ExecStartPre/ExecStart. A root
    # unit plus a chown pre-command loses that chown on the next command.
    dropin = Path('/etc/systemd/system/litecp-dns.service.d/20-runtime-owner.conf')
    if dropin.is_symlink():
        raise RuntimeError('Refusing a symlinked DNS runtime ownership policy')
    policy = ('[Service]\nUser=' + user + '\nGroup=' + user +
              '\nAmbientCapabilities=CAP_NET_BIND_SERVICE\nCapabilityBoundingSet=CAP_NET_BIND_SERVICE\n'
              'ExecStartPre=\nExecStartPre=+/bin/sh -c "if command -v restorecon >/dev/null; then restorecon -R /run/litecp-dns; fi"\n')
    if not dropin.exists() or dropin.read_text() != policy:
        dropin.parent.mkdir(mode=0o755, exist_ok=True)
        if dropin.exists():
            previous = Path(tempfile.mkdtemp(prefix='dns-unit-repair-', dir=config.parent))
            shutil.copy2(dropin, previous / 'previous-dropin.conf')
        dropin.write_text(policy)
        os.chmod(dropin, 0o644)
        changed = True
    if shutil.which('restorecon'):
        subprocess.run(['restorecon', str(config), str(runtime)], check=True)
    if changed:
        subprocess.run(['systemctl', 'daemon-reload'], check=True)
        if Path('/etc/litecp/dns/enabled').exists() and subprocess.run(['systemctl', 'is-enabled', '--quiet', 'litecp-dns']).returncode == 0:
            subprocess.run(['systemctl', 'restart', 'litecp-dns'], check=True)
            subprocess.run(['systemctl', 'is-active', '--quiet', 'litecp-dns'], check=True)


def _user_exists(name):
    import pwd
    try:
        pwd.getpwnam(name)
        return True
    except KeyError:
        return False



if __name__ == '__main__':
    main()
