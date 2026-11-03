# Source security review - 3 November 2026

Scope: focused review of HTTP authentication/origin handling, sessions, terminal ownership, URL downloads, internal sign-on tickets, file path validation and archive path validation. Local source only; no live penetration test or production certification.

## Fixed in 4.0.22

URL upload address checks accepted IPv6 addresses outside ordinary globally allocated unicast, including the local-use NAT64 prefix 64:ff9b:1::/48. On a server with translation routing this can allow a download to reach internal IPv4 services, contrary to the public-URL restriction. Require IPv6 2000::/3 and exclude special transition/protocol and documentation ranges. Existing DNS resolution pinning, redirect checks, private IPv4 blocking and standard-port restrictions are retained. Added regression addresses covering translation, discard, transition and documentation networks.

## Remaining risks and boundaries

- High impact: deploy/litecp.service still runs the HTTP service as root. An authenticated administrator also has an interactive server terminal. Stolen administrator credentials/session therefore expose server administration. The systemd sandbox reduces some exposure but is not an unprivileged web process plus narrowly typed privileged broker. A safe privilege split requires a separate staged implementation and Linux acceptance checks; simply changing User is insufficient.
- File isolation clarification: the main Linux file manager operations already use descriptor-relative openat2/openat helpers with no-follow checks (securepath_linux.go), in addition to resolveManagedPath. The earlier warning based on resolveManagedPath alone was too broad. Full Linux concurrent-rename/mount boundary acceptance remains unverified; the non-Linux fallback does not offer equivalent isolation and is not the production hosting target.
- Backup files contain sensitive data. Transport encryption and private permissions do not provide archive encryption at rest. Protect local/remote storage access and verify recovery independently.
- No claim that all bugs or vulnerabilities were found. No live firewall, SSH, service, credential or website changes were made.

## Verification

Focused Go checks passed for URL upload safety, security/origin/session/second-factor/terminal checks selected by test name. A second focused run covers login admission, password-work concurrency, bounded sessions, proxy/origin trust and internal sign-on checks. Existing Makefile retained. No installer, updater, deployment or distributable build executed.

## Follow-up hardening in 4.0.23

- Logout removes the owning browser's terminal sessions immediately and closes/signals the PTY bridge. Other browsers are unaffected. A five-second watchdog also checks session/trusted-browser validity, so expired sessions and administrator credential/2FA revocation no longer leave shells usable until idle expiry. Idle or exited terminals only remove their own session. The bridge's existing shutdown handler sends SIGHUP to its shell process group. This is lifecycle hardening, not a replacement for privilege separation or a guarantee against intentionally detached administrator processes.
- Edited-file reads now reject data that grows beyond the editing cap during the read, instead of returning a capped partial document that could subsequently overwrite the original.
- Root web-process architecture and archive encryption at rest are still open. They require full implementations, compatibility decisions and Linux acceptance testing; this patch does not claim them resolved.
