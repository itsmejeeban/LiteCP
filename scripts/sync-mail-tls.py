#!/usr/bin/env python3
"""Apply the panel hostname certificate to LiteCP-managed SMTP and IMAP."""

import fcntl
import hashlib
import os
from pathlib import Path
import re
import runpy
import shutil
import socket
import subprocess
import sys
import tempfile
import time


BOOTSTRAP = ("/etc/litecp/tls/litecp.crt", "/etc/litecp/tls/litecp.key")
PANEL_CONFIGS = (
    Path("/etc/nginx/conf.d/litecp-panel.conf"),
    Path("/etc/nginx/sites-available/litecp-panel.conf"),
)
DOVECOT_DIRECTORY = Path("/etc/dovecot/conf.d")
MAIL_DIRECTORY = Path("/etc/litecp/mail")
RECOVERY_DIRECTORY = Path("/var/backups/litecp")


def run(*args, **kwargs):
    return subprocess.run(args, check=True, timeout=25, **kwargs)


def output(*args):
    return run(*args, stdout=subprocess.PIPE, text=True).stdout.strip()


def active(service):
    return subprocess.run(["systemctl", "is-active", "--quiet", service], timeout=10).returncode == 0


def managed_pair(pair, target, hostname):
    # Earlier LiteCP builds could install either the bootstrap pair or a
    # hostname certificate already provisioned by the panel.
    previous_lineage = "/etc/letsencrypt/live/litecp-panel-" + hashlib.sha256(hostname.encode("ascii")).hexdigest()
    previous = (previous_lineage + "/fullchain.pem", previous_lineage + "/privkey.pem")
    return pair in (BOOTSTRAP, target, previous)


def verify_service(host, endpoints):
    for port, protocol, optional in endpoints:
        if optional:
            try:
                with socket.create_connection(("127.0.0.1", port), timeout=2):
                    pass
            except ConnectionRefusedError:
                continue
        command = ["openssl", "s_client", "-connect", f"127.0.0.1:{port}",
                   "-servername", host, "-verify_hostname", host, "-verify_return_error"]
        if protocol:
            command.extend(["-starttls", protocol])
        for attempt in range(4):
            try:
                subprocess.run(command, check=True, timeout=5, stdin=subprocess.DEVNULL,
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                break
            except subprocess.SubprocessError as error:
                if attempt == 3:
                    raise ValueError(f"TLS handshake on port {port} could not be verified") from error
                time.sleep(0.4)


def write_config(path, data):
    previous = path.stat()
    with tempfile.NamedTemporaryFile(dir=path.parent, delete=False) as temporary:
        temp = Path(temporary.name)
        try:
            os.fchown(temporary.fileno(), previous.st_uid, previous.st_gid)
            os.fchmod(temporary.fileno(), previous.st_mode & 0o777)
            temporary.write(data)
            temporary.flush()
            os.fsync(temporary.fileno())
            os.replace(temp, path)
        finally:
            temp.unlink(missing_ok=True)
    if restorecon := shutil.which("restorecon"):
        run(restorecon, str(path))


def apply(service, config, update, check, host, endpoints, change_needed, renewing):
    running = active(service)
    if not change_needed and not renewing:
        if running:
            try:
                verify_service(host, endpoints)
                return
            except ValueError:
                pass  # Configured paths may point to a newer certificate than the running process.
        else:
            return
    if config.is_symlink() or not config.is_file():
        raise ValueError(f"Expected regular managed configuration for {service}")
    original = config.read_bytes()
    if change_needed:
        RECOVERY_DIRECTORY.mkdir(mode=0o700, parents=True, exist_ok=True)
        recovery = Path(tempfile.mkdtemp(prefix="mail-tls-", dir=RECOVERY_DIRECTORY)) / config.name
        recovery.write_bytes(original)
    try:
        if change_needed:
            update()
        run(*check, stdout=subprocess.DEVNULL)
        if running:
            run("systemctl", "reload", service)
            verify_service(host, endpoints)
    except (OSError, ValueError, subprocess.SubprocessError):
        if change_needed:
            write_config(config, original)
            if running:
                run("systemctl", "reload", service)
        raise
    print(f"Configured {service} with the trusted certificate for {host}.")


def sync_postfix(host, hostname, target, renewing, reload_only):
    transport = output("postconf", "-h", "virtual_transport")
    mailboxes = output("postconf", "-h", "virtual_mailbox_maps")
    if transport != "lmtp:unix:private/dovecot-lmtp" or not mailboxes.endswith(":/etc/litecp/mail/mailboxes"):
        print("Postfix is using an independent configuration; leaving it in place.")
        return
    cert_param, key_param, chain_param = "smtpd_tls_cert_file", "smtpd_tls_key_file", "smtpd_tls_chain_files"
    params = (cert_param, key_param, chain_param)
    pair = tuple(output("postconf", "-xh", param) for param in (cert_param, key_param))
    chain_files = output("postconf", "-xh", chain_param)
    chain_parts = [path.strip() for path in chain_files.split(",")]
    chain_target = ", ".join(reversed(target))
    if chain_files and pair == ("", "") and len(chain_parts) == 2:
        pair = tuple(reversed(chain_parts))
    elif chain_files:
        print("Postfix is using a custom TLS chain; leaving it in place.")
        return
    custom_tls = ("smtpd_tls_eccert_file", "smtpd_tls_eckey_file", "smtpd_tls_dcert_file",
                  "smtpd_tls_dkey_file", "tls_server_sni_maps")
    overrides = output("postconf", "-P")
    if (not managed_pair(pair, target, hostname) or any(output("postconf", "-xh", name) for name in custom_tls)
            or any(re.search(rf"/{name}\s*=", overrides) for name in (*params, *custom_tls))):
        print("Postfix is using custom TLS settings; leaving them in place.")
        return
    if reload_only and pair != target:
        return
    config = Path(output("postconf", "-h", "config_directory")) / "main.cf"
    if config != Path("/etc/postfix/main.cf"):
        print("Postfix is using an independent configuration directory; leaving it in place.")
        return
    # The Postfix chain configuration accepts RSA and ECDSA, while the
    # bootstrap smtpd_tls_cert_file setting is specific to RSA.
    apply("postfix.service", config,
          lambda: run("postconf", "-e", f"{cert_param} =", f"{key_param} =", f"{chain_param} = {chain_target}"),
          ("postfix", "check"), host, ((25, "smtp", False), (465, None, True), (587, "smtp", True)),
          not reload_only and (pair != target or not chain_files), renewing)


def sync_dovecot(host, hostname, target, renewing, reload_only):
    base = DOVECOT_DIRECTORY / "99-litecp.conf"
    if not base.is_file() or not base.read_text().startswith("# Managed by LiteCP"):
        print("Dovecot is using an independent configuration; leaving it in place.")
        return
    override = DOVECOT_DIRECTORY / "zz-litecp-webmail-tls.conf"
    config = override if override.is_file() else base
    text = config.read_text()
    assignments = [re.compile(rf"^(ssl_{name}[ \t]*=[ \t]*<)([^\s#]+)", re.MULTILINE) for name in ("cert", "key")]
    values = [assignment.findall(text) for assignment in assignments]
    if any(len(entries) != 1 for entries in values):
        print("Dovecot has no single managed TLS pair; leaving its configuration in place.")
        return
    pair = tuple(entries[0][1] for entries in values)
    if not managed_pair(pair, target, hostname):
        print("Dovecot is using a custom TLS pair; leaving it in place.")
        return
    if reload_only and pair != target:
        return
    updated = text
    for assignment, path in zip(assignments, target):
        updated = assignment.sub(lambda match: match[1] + path, updated)
    apply("dovecot.service", config, lambda: write_config(config, updated.encode("utf-8")),
          ("doveconf", "-n"), host, ((993, None, False), (995, None, True)), pair != target, renewing)


def sync():
    if not (MAIL_DIRECTORY / "mailboxes").is_file() or (MAIL_DIRECTORY / "disabled").exists():
        return
    shared_tls = runpy.run_path(str(Path(__file__).with_name("sync-ftps-tls.py")))
    config = next((path for path in PANEL_CONFIGS if path.is_file()), None)
    source = shared_tls["panel_lineage"](config) if config else None
    if source is None:
        return
    host, lineage = source
    renewed = os.getenv("RENEWED_LINEAGE")
    if renewed and Path(renewed) != lineage:
        return
    reload_only = os.getenv("LITECP_MAIL_TLS_RELOAD_ONLY") == "1"
    hostname = output("postconf", "-h", "myhostname").lower()
    if "." in hostname and not hostname.endswith((".localdomain", ".local")):
        # Keep the configured SMTP identity and require the certificate to
        # cover it. Administrators may use an independent public mail name.
        if reload_only and hostname != host:
            return
        shared_tls["trusted_bundle"](hostname, lineage)
        host = hostname
    else:
        shared_tls["trusted_bundle"](host, lineage)
    target = (str(lineage / "fullchain.pem"), str(lineage / "privkey.pem"))
    failures = []
    with (MAIL_DIRECTORY / "service-tls.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        for service, configure in (("Postfix", sync_postfix), ("Dovecot", sync_dovecot)):
            try:
                configure(host, hostname, target, bool(renewed), reload_only)
            except (OSError, ValueError, subprocess.SubprocessError) as error:
                failures.append(f"{service}: {error}")
    if failures:
        raise ValueError("; ".join(failures))


if __name__ == "__main__":
    os.umask(0o077)
    if os.geteuid() != 0:
        sys.exit("Run mail TLS synchronization as root.")
    try:
        sync()
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        sys.exit(f"Could not synchronize mail certificates: {error}")
