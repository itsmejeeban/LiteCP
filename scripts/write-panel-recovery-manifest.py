"""Record a previous panel binary for the independent rollback helper."""

import datetime
import hashlib
import ipaddress
import json
import pathlib
import re
import sys
import urllib.request


def frontend_endpoint(config):
    bind = "127.0.0.1:9090"
    if config.is_file():
        for line in config.read_text().splitlines():
            if line.startswith("LITECP_BIND="):
                bind = line.split("=", 1)[1].strip().strip("\"'")
    host, port = bind.rsplit(":", 1)
    address = ipaddress.ip_address(host.strip("[]"))
    if not address.is_loopback or not port.isdecimal() or not 1 <= int(port) <= 65535:
        raise ValueError("Invalid panel health-check endpoint")
    return f"[{address}]:{port}" if address.version == 6 else f"{address}:{port}"


def write_manifest(root, bind):
    with urllib.request.urlopen(f"http://{bind}/api/version", timeout=3) as response:
        current = json.load(response)
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", current.get("version", "")):
        raise ValueError("The running panel did not report a stable version")
    digest = hashlib.sha256()
    with (root / "previous-litecp").open("rb") as binary:
        for chunk in iter(lambda: binary.read(1024 * 1024), b""):
            digest.update(chunk)
    current["sha256"] = digest.hexdigest()
    current["healthBind"] = bind
    current["savedAt"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    with (root / "recovery-manifest.json").open("x") as manifest:
        json.dump(current, manifest)


if __name__ == "__main__":
    write_manifest(pathlib.Path(sys.argv[1]), frontend_endpoint(pathlib.Path("/etc/litecp/frontend.env")))
