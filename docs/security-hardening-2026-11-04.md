# Security dashboard hardening verification

The live test server was inspected on 4 November 2026. SELinux was enabled but
Permissive. A five-minute systemd rollback timer was armed before changing the
runtime to Enforcing. Checks passed before persistence and timer cancellation:

- Panel version endpoint and authenticated Security Dashboard responded.
- All three configured websites returned HTTPS 200 using their valid certificates.
- LiteCP, Nginx, MySQL, Postfix, Dovecot and Fail2ban remained active.
- SMTP/IMAP listeners remained present; no recent AVC/USER_AVC denials appeared.

The boot configuration now specifies `SELINUX=enforcing`. The prior configuration
was retained at `/var/backups/litecp/selinux-before-enforcing-20261104T172453-709993.config`.
No reboot, policy allow-all module, service restart or relabel was performed.
These checks do not establish coverage of every application or mail delivery path.

Follow-up: a reload-only failure was subsequently found while testing brute-force
protection. Existing workers had continued serving, but vendor WAF rules retained
`var_t` labels and could not be loaded by confined Nginx. Scoped configuration
labels and vendor ownership were repaired, and an actual reload plus HTTP 200/403
checks passed with SELinux enforcing. See `brute-force-live-test-2026-11-05.md`.

`scripts/enable-selinux.sh` now also arms an independent systemd rollback timer.
It preserves the previous configuration and runtime mode on unsuccessful checks.
Run only after application-specific staging acceptance as its existing header says.

## Remaining privilege separation work

The HTTP service still runs as root and the dashboard warning remains accurate.
Changing `User=root` alone breaks existing privileged file, service, firewall,
mail, backup, restore and terminal operations. A root broker must expose typed,
allowlisted operations, authenticate Unix-socket peer identity, validate site
ownership and paths, and refuse arbitrary command execution. The HTTP process
must not retain root-equivalent sudo, container socket or terminal access.
All privileged call sites and background workflows need migration and staged
Linux acceptance before switching the service account. This architectural change
has not been implemented or claimed complete by this SELinux repair.

The existing Makefile build entry point is retained. No build or package was
generated for this operational verification.
