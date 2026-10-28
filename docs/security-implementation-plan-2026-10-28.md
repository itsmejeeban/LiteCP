# Security implementation plan - 28 October 2026

This plan preserves the current Go application, single administrator, navigation, Postfix/Dovecot/OpenDKIM and existing backup/storage systems. No live service or security-policy changes are authorized by this document.

1. Record source/runtime evidence, protect unrelated local edits, run baseline analysis in an isolated test checkout.
2. Fix confirmed authentication timing, secret/log exposure, certificate-status and validation defects with regression tests. No automatic enrollment or credential changes.
3. Add one read-only Security Dashboard inside the existing Security group, using observed/unknown/not-implemented states rather than a security score.
4. Implement administrator TOTP enrollment, encrypted secrets, replay protection, recovery codes, reauthentication, revocation and UI in the existing authentication flow. Test isolated fixtures before an approved service restart; enrollment remains an administrator action.
5. Extend existing brute-force and Fail2ban integration. Prepare filters/jails but obtain approval before changing live bans or SSH policy. Never impose a permanent account lock that unauthenticated requests can trigger.
6. Extend existing firewall controls with timed rules, SSH/current-client safety, preview/confirmation and rollback. Apply only explicitly approved rules.
7. Extend existing malware and WAF pages: history, bounded schedules, manual quarantine/restore, exclusions and validation. Never delete customer files automatically. Optional service installation/reload requires approval.
8. Add bounded integrity baselines and change history; protect baseline approval and document limitations against root tampering. Reuse activity/security events.
9. Add network/HTTP monitoring and per-site limits using existing Nginx/firewall paths, initially observation only. Upstream network protection is required for volumetric attacks.
10. Improve existing Website Tools SSL status, expiry/renewal auditing and alerts without relocating it or duplicating issuance.
11. Audit current mail restrictions/TLS/permissions and preserve OpenDKIM; keep optional Rspamd signing disabled when OpenDKIM signs. Do not replace live mail configuration. Provider relay configuration must be explicitly supplied.
12. Audit existing backups/restore/encryption/transport/integrity and secret storage; introduce format changes only with backward compatibility and isolated round-trip tests.
13. Extend append-only security event handling/export and permission checks. Treat local tamper resistance as limited while the web service runs as root.
14. Prepare unprivileged web-service migration and a typed, allowlisted privileged broker. Do not switch User=root to a normal user until every existing privileged operation is routed and staged acceptance passes.
15. Audit updates; separate security-update detection from installation, add authenticity verification and maintenance/rollback controls. Do not change live automatic-update policy without approval.
16. Terminal threat model: a stolen admin session, cross-origin WebSocket, leaked attach ticket or broker bug must not confer root. Normal-user sessions need fresh password, one-use bound tickets, origin/CSRF checks, timeouts, limits, resize bounds, process cleanup and start/end auditing. Root access stays absent/disabled until 2FA, broker isolation and explicit threat-model approval are in place.
17. Complete SECURITY_AUDIT_REPORT.md with evidence and per-feature status. Distinguish implemented source, isolated tests, deployed code and live verification. Never claim complete security.

## Approval gates / rollback

- Live deployment: retain the current binary and affected configuration, validate an independently built binary, approve panel restart, health-check authentication/state/static assets, restore the previous binary if checks fail.
- SSH/firewall: confirm out-of-band console or a second tested access route first; save effective configuration, syntax-check, schedule an automatic revert, keep the existing session open and verify a new session before cancelling rollback.
- SELinux: currently Permissive. First collect policy denials in staging and write narrow policy; Enforcing must not be switched blindly on this hosting server.
- Privilege split: retain existing service unit/configuration and current binary; stage broker socket permissions, peer credentials, operation allowlists, site ownership and SELinux policy; approve a maintenance window; rollback both unit and binary together.
- Root terminal: explicit separate approval only; not bundled into ordinary web-terminal approval.
