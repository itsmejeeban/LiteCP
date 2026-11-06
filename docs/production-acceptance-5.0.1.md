# LiteCP 5.0.1 deployed acceptance - 6 November 2026

## Decision

Conditional acceptance only. No newly confirmed source defect was found in this focused recheck. This is not an assertion that all possible defects have been eliminated or that every production prerequisite is complete.

## Current verification

- Deployed version: 5.0.1, revision 53066f48feacb3cfdf06f43ec40a22bcfe9e7a9e.
- Chrome trusted-hostname dashboard and Security Dashboard render. Required hosting services report running. Website count and backup ages load.
- Server checks: panel, broker, Nginx, MySQL, Postfix, Dovecot, OpenDKIM and authoritative DNS active. Nginx configuration passes. SELinux Enforcing. Web service runs as litecp-web, NoNewPrivileges=yes, empty capability bounding set.
- Unauthenticated /api/state returns 401. Panel responses have CSP, no-store, frame denial, nosniff, referrer and permission policies.
- No warning-priority panel/broker journal entries in the inspected 20-minute window.
- Resource audit boundary initially retained an older memory.oom.group verification error and its five-minute cooldown. The monitor executable checksum matches the installed binary. At 20:44 IST it automatically returned to Active, cleared the error and recovery deadline, and both website workers became active. No policy was relaxed or cooldown bypassed. Two historical failed CPU test units were preserved.
- HTTP access to the audit website returns 403 with 'HTTPS is required for website verification'; this is the configured CAPTCHA transport requirement, not evidence that resource recovery failed.
- Current frontend suite: 184/184 passed. Authorized isolated Linux backend regression suite passed (cached) and go vet passed. These checks do not perform a fresh installation or an off-server recovery.
- Existing Makefile retained. No build, package, deployment, data deletion or security-policy change performed in this pass.

## Production actions still needed

1. Administrator: user reports 2FA functionality tested, but the current live Security Dashboard reports authenticator enrollment disabled. Enroll for the actual production administrator and retain recovery codes privately.
2. SSH: root/password login is still permitted. Validate keys and independent console/recovery access before restricting it; the updater currently depends on password access.
3. Mail: public MX, SPF, DMARC (monitor policy) and PTR exist. The default DKIM selector lookup for qa-backup-audit.litimcsellcop.com is empty. Publish the generated selector/key in the authoritative provider and verify real signed delivery. Do not silently publish records or send mail without the necessary destination details.
4. Restore: verify a complete recovery on a separate destination, including database/mail credentials and workers. A destination has not been supplied.
5. Review integrity changes, fresh-install/reboot/rollback behavior on advertised operating systems and external security-log retention. Do not approve a changed integrity baseline automatically.

No new source repair was justified by these results. Production sign-off remains pending the concrete actions above.
