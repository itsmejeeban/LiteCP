# Update recovery - 5 November 2026

The attempted 4.0.97 deployment rolled back to 4.0.94 because the unprivileged
web unit failed at systemd's NAMESPACE step. SELinux denied mounting systemd's
inaccessible regular-file placeholder over `/etc/shadow` (`shadow_t`). Static unit
validation and the earlier raw subprocess tests did not exercise this mount.

The redundant shadow-file mask was removed. The dedicated UID, empty capability
sets, NoNewPrivileges, strict filesystem protection and other private-data masks
remain. Runtime sandbox testing confirmed that shadow and panel private files
cannot be opened, with UID 990 and no capabilities, while SELinux stays Enforcing.
No permissive SELinux switch or broad policy allow rule was used.

## Live recovery

The retained binary at
`/root/litecp-manual-update-c3b0ae2d8bab4fb8a108a008b272b3ca/next-litecp`
was reused; no new production binary was built. The current binary, configuration
and service units were preserved in `/root/litecp-privilege-recovery-xnbCQVov`.
Recovery had a binary-and-unit rollback trap and version/identity health checks.

Verified live:

- 4.0.97, revision `567ede51157a1cb905c8df6602f80cdc3e9a28ce`.
- `litecp.service`: real/effective/saved UID 990, GID 989, CapEff/CapPrm zero,
  NoNewPrivs 1; broker and socket active.
- SELinux Enforcing; Nginx and Fail2ban active; no recent service errors observed.
- Chrome Brute Force Protection loads with no stale read error.
- Chrome Security Dashboard reports the unprivileged web service accurately.
- All three websites and their storage/database-size values load in Chrome.
- Normal test website response 200; existing WAF-blocked test path 403.

Screenshot: `update-recovery-4.0.97-2026-11-05.jpg`.

## Permanent source changes (4.0.98, not yet published)

`scripts/check-web-sandbox.sh` runs the candidate unit's sandbox settings in a
short-lived systemd service, without broker dependencies or application startup.
It checks the actual UID, private-file access, capability fields and NNP. It runs
before the service installer replaces units and, through `--prepare-only`, before
either update entry point stops the current panel. A failed deployment now prints
service status/journal details before rollback.

Five Node installation-policy regressions and Linux runtime sandbox checks passed.
The existing Makefile remains; no release archive or package was generated.
