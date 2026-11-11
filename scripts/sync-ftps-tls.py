#!/usr/bin/env python3
"""Update only LiteCP-managed FTPS bundles from the active panel ACME lineage."""

import fcntl
import hashlib
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import time


def run(*args, timeout=25, **kwargs):
    return subprocess.run(args, check=True, timeout=timeout, **kwargs)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def atomic_private_file(path, data):
    with tempfile.NamedTemporaryFile(dir=path.parent, delete=False) as pending:
        temp = Path(pending.name)
    try:
        with temp.open("wb") as pending:
            pending.write(data)
            pending.flush()
            os.fsync(pending.fileno())
        os.replace(temp, path)
    finally:
        temp.unlink(missing_ok=True)


def panel_lineage(config):
    text = config.read_text()
    hosts = re.findall(r"^\s*server_name\s+([\w.-]+)(?:\s+_)?\s*;", text, re.MULTILINE)
    certs = re.findall(r"^\s*ssl_certificate\s+(/etc/letsencrypt/live/[\w.-]+)/fullchain\.pem\s*;", text, re.MULTILINE)
    if len(hosts) == len(certs) == 1 and "." in hosts[0] and ".." not in hosts[0] and ".." not in certs[0].split("/"):
        return hosts[0], Path(certs[0])
    return None


def trusted_bundle(host, lineage):
    fullchain, key = lineage / "fullchain.pem", lineage / "privkey.pem"
    run("openssl", "verify", "-purpose", "sslserver", "-verify_hostname", host,
        "-untrusted", str(fullchain), str(fullchain), stdout=subprocess.DEVNULL)
    cert_key = run("openssl", "x509", "-in", str(fullchain), "-pubkey", "-noout",
                   stdout=subprocess.PIPE).stdout
    private_key = run("openssl", "pkey", "-in", str(key), "-pubout",
                      stdout=subprocess.PIPE).stdout
    if cert_key != private_key:
        raise ValueError("FTPS certificate does not match its private key")
    return key.read_bytes() + fullchain.read_bytes()


def verify_listener(host):
    for attempt in range(5):
        try:
            run("openssl", "s_client", "-starttls", "ftp", "-connect", "127.0.0.1:21",
                "-servername", host, "-verify_hostname", host, "-verify_return_error",
                stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                timeout=4)
            return
        except subprocess.SubprocessError:
            if attempt == 4:
                raise
            time.sleep(0.5)


def sync(root, bootstrap, config, renewed_lineage=None):
    bundle, marker = root / "tls.pem", root / "panel-tls.sha256"
    if not bundle.is_file() or not config.is_file():
        return
    if root.is_symlink() or bundle.is_symlink() or marker.is_symlink():
        raise ValueError("Unexpected symbolic link in FTPS TLS storage")
    source = panel_lineage(config)
    if not source:
        return
    host, lineage = source
    if renewed_lineage and Path(renewed_lineage) != lineage:
        return
    with (root / "panel-tls.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        original = bundle.read_bytes()
        current = digest(original)
        known = marker.read_text().splitlines() if marker.exists() else []
        if current not in known:
            key, cert = bootstrap / "litecp.key", bootstrap / "litecp.crt"
            if not key.is_file() or not cert.is_file() or original != key.read_bytes() + cert.read_bytes():
                print("FTPS is using a custom TLS bundle; leaving it in place.")
                return
        replacement = trusted_bundle(host, lineage)
        if replacement == original:
            return
        active = subprocess.run(["systemctl", "is-active", "--quiet", "litecp-ftps.service"],
                                timeout=10).returncode == 0
        # Record both digests before switching so an interrupted run remains recognizable.
        atomic_private_file(marker, f"{current}\n{digest(replacement)}\n".encode("ascii"))
        restorecon = shutil.which("restorecon")
        try:
            atomic_private_file(bundle, replacement)
            if restorecon:
                run(restorecon, str(bundle))
            if active:
                run("systemctl", "restart", "litecp-ftps.service")
                verify_listener(host)
        except (OSError, subprocess.SubprocessError):
            atomic_private_file(bundle, original)
            if restorecon:
                run(restorecon, str(bundle))
            if active:
                run("systemctl", "restart", "litecp-ftps.service")
            raise
        print(f"Installed the trusted FTPS certificate chain for {host}.")


if __name__ == "__main__":
    os.umask(0o077)
    if os.geteuid() != 0:
        sys.exit("Run FTPS TLS synchronization as root.")
    configs = [Path("/etc/nginx/conf.d/litecp-panel.conf"),
               Path("/etc/nginx/sites-available/litecp-panel.conf")]
    config = next((path for path in configs if path.is_file()), configs[0])
    try:
        sync(Path("/var/lib/litecp/ftp"), Path("/etc/litecp/tls"), config, os.getenv("RENEWED_LINEAGE"))
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        sys.exit(f"Could not synchronize the FTPS certificate: {error}")
