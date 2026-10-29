# SSH port

Fresh LiteCP installations configure OpenSSH on TCP 369. Re-running the
installer on an existing configured LiteCP installation preserves its SSH port.
Security → SSH Port shows the configured listener port and changes it after
confirmation. The local firewall and SELinux mapping are prepared first.
The SSH service uses a dedicated systemd override; the original sshd_config
remains untouched. Previous overrides are retained in the panel data directory.
A local SSH banner check verifies the new listener, with rollback on failure.
Ubuntu socket activation is disabled only after the service listener is verified.

Before changing ports, allow the desired TCP port in any provider firewall.
After changing ports, reconnect SSH using that port. For update.bat, add:

    set "LITECP_UPDATE_PORT=369"

to the private, ignored update-local.bat file (use your chosen port).
Existing local firewall rules are preserved. Remove obsolete firewall access
only after confirming an external SSH connection works on the new port.

Requires systemd, OpenSSH server, Python 3, and UFW or firewalld. SELinux hosts
also require semanage. An already assigned incompatible SELinux port fails
without changing the SSH service. Panel updates alone do not change SSH ports.
