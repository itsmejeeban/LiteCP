# LiteCP security audit — 28 October 2026

## Executive summary

The source audit found a root-running web service, permissive SELinux and permitted root/password SSH access on the test server. The first source hardening slice adds optional administrator 2FA, safer sign-in behavior, certificate-status checks and a read-only Security Dashboard. These changes passed isolated tests but are not live. The remaining architectural and service-specific work below is substantial; production security readiness cannot be certified from this review.

## Scope and confidence

This report covers the Go control panel and the AlmaLinux 10 test server at `2.31.109.187`. Source changes described here are in the workspace and an isolated Linux test checkout. **They are not deployed to the running panel.** The live checks below were read-only. A running service is not proof that its policy, data flow, or recovery procedure is correct. Nothing here guarantees a secure system.

The existing panel, website, mail and backup data were not removed. No live service was restarted, no firewall/SSH policy was changed, and no root terminal was enabled. The pending changes do not include unrelated preexisting workspace edits.

## Confirmed live state

| Check | Observed state | Consequence |
| --- | --- | --- |
| Panel service | `User=root`, `Group=root`; `NoNewPrivileges=yes`, `ProtectSystem=strict`, `ProtectHome=tmpfs` | The process retains broad root authority through its explicit privileged operations. Unit sandboxing does not make it unprivileged. |
| SELinux | Permissive | Enforcement must be staged with a policy that covers hosting, mail and panel operations. |
| SSH | Root login and password login permitted; maximum six authentication tries | Restrict only after an independent access route and rollback have been tested. |
| Firewall and Fail2ban | Both active; Fail2ban lists `sshd`, `dovecot`, `postfix`, `postfix-sasl` | Effective jail actions, recent bans, and coverage of panel port 8443 still need validation. |
| Mail | Postfix, Dovecot and OpenDKIM active; Rspamd inactive | Mail delivery and anti-spam behavior have not been end-to-end tested. |
| Local secrets | `/var/lib/litecp` root-only (0700); config, key and environment file 0600 | Protects against ordinary local users, not a compromised root process. |

## Implemented in source and isolated-test evidence

- Administrator sign-in always performs password verification before comparing the username, closing the confirmed fast wrong-username path. Failed sign-ins emit fixed, IP-only journal events without passwords, codes, usernames, cookies or request bodies.
- Optional administrator TOTP enrollment uses local QR generation, encrypted pending and active seeds, a ten-minute pending window, replay rejection, ten hashed one-use recovery codes, fresh password proof for changes, and session revocation after factor changes. Trusted-browser developer mode cannot bypass enabled 2FA and now requires current-password proof to create new trust. Enrollment is off until the administrator completes it.
- Certificate status now checks its validity window and the site hostname rather than expiry alone. This is still not a full TLS chain, private-key or endpoint validation.
- A read-only Security Dashboard reports observed, missing and unknown conditions without inventing a security score or claiming all checks passed. Its probes have fixed commands and bounded timeouts.
- A panel Fail2ban filter and disabled jail definition are prepared; they have not been installed or activated. See `deploy/fail2ban-litecp-admin.conf` and `deploy/fail2ban-litecp-admin.local`.
- The optional Rspamd installer now explicitly disables its outbound DKIM signing module, leaving OpenDKIM as the sole signer. The installer passed `bash -n` in the isolated checkout; it was not executed on the live server.
- Isolated AlmaLinux test checkout: Go package tests and `go vet` passed; targeted authentication tests passed with the race detector. Frontend navigation/settings workflow checks passed 16/16. These checks validate source behavior, not production deployment or real mail, WAF, backup-restore and browser enrollment flows.

## Feature-by-feature status

| Requested area | Current assessment and remaining work |
| --- | --- |
| Administrator 2FA | **Source implemented, isolated tests passed, live not deployed.** Need browser enrollment acceptance, offline recovery-code handoff and approved deployment/restart. A root-level emergency recovery process is not yet documented. |
| Brute-force protection | Existing per-IP limit and Fail2ban service confirmed. Source audit events and disabled 8443 jail prepared. Check real journal matching, allowlist/ban safety, reverse-proxy client IP and unban procedure before enabling. |
| Firewall | Existing firewalld controls identified; active service confirmed. Timed rules, current-client/SSH lockout prevention and automatic rollback are not implemented. |
| Malware Scan | Existing bounded scanner and UI retained. Scheduling, quarantine/restore, signature freshness and scan history are incomplete or unverified. No automatic deletion should be added. |
| WAF | Existing ModSecurity/CRS controls retained. Effective per-site rules, exclusions, false-positive handling and live block tests were not verified. |
| SSH security | Current root/password policy confirmed. No live SSH change; alternate access, saved config and scheduled rollback are prerequisites. |
| File integrity monitoring | **Not implemented.** A trusted baseline and change-history storage scheme must be designed; local data cannot be tamper-resistant against root. |
| DDoS controls | **Not verified/partially absent.** Nginx rate controls and attack telemetry need review. Volumetric attacks require upstream network protection. |
| Website Tools SSL | Existing issuance flow retained; certificate-status hostname/time fix in source. Chain, renewal job, failure alerts and per-site browser checks remain. |
| Mail security | Postfix/Dovecot/OpenDKIM active. Existing TLS/auth and permissions were inspected; open-relay and end-to-end send/receive tests remain. Rspamd is inactive. Its optional installer has a source-only change to disable Rspamd signing and keep OpenDKIM as sole signer. Relay settings require actual provider details. |
| Backup security | Existing private storage, archive validation, manifest and scoped restore logic found. Backup archives are not encrypted; off-server restore, runtime round trip, retention and transport acceptance remain unverified. |
| Administrator access and privilege | **Critical:** panel service runs as root. A typed allowlisted privileged broker, unprivileged service user, file ownership model and SELinux policy require staged implementation and compatibility tests. Changing only `User=` would break existing operations. |
| Audit logs | Existing activity events are user-manageable. New fixed authentication journal events are source-only. Independent retention, export and tamper resistance need implementation; local logs cannot withstand root compromise. |
| Secrets | Existing at-rest secret encryption and root-only files were observed. Recovery hashes are one-use in the new source. Repository/history leak scans, rotation procedures and key-loss recovery need separate review. |
| Updates | Existing panel updater and rollback path found. Independent release signature verification and maintenance policy remain. Installer currently enables automatic OS security installation; the requested detect/review/install separation is not implemented. No automatic update policy was changed live. |
| Security Dashboard | **Source implemented, isolated API tests passed.** Deploy and validate in an authenticated browser before treating it as live. Several items intentionally remain warning/unknown. |
| Web terminal | **Not implemented; root activation not approved.** Requires an unprivileged account, fresh password/2FA proof, bound one-use attach tickets, origin checks, command/session limits, process cleanup, auditing and a separately reviewed privileged broker. No terminal endpoint should be exposed until that threat model passes. |

### Per-feature evidence and implementation status

Each entry records existing status, finding, implementation required, changes made, files, tests, verification and residual risk. Source tests and live verification are stated separately.

1. **Administrator 2FA — PARTIALLY IMPLEMENTED.** Existing: password/session authentication, no TOTP. Finding: no second factor. Required: deploy, browser enrollment and recovery runbook. Made: encrypted TOTP, QR, replay/recovery/session controls. Files: `main.go`, `store.go`, `two_factor.go`, `developer_mode.go`, `web/two-factor.js`, `web/index.html`, `web/app.js`, `two_factor_test.go`. Tests: TOTP vectors, login/replay, recovery concurrency and persistence, targeted race test passed. Verified: isolated source only. Risk: live account remains single-factor until enrollment.
2. **Brute-force protection — PARTIALLY IMPLEMENTED.** Existing: per-IP limit and active Fail2ban with SSH/mail jails. Finding: no active panel jail. Required: journal/ban safety and rollback. Made: fixed sign-in journal events and disabled panel jail/filter. Files: `authentication_events.go`, `main.go`, `deploy/fail2ban-litecp-admin.conf`, `deploy/fail2ban-litecp-admin.local`, `security_foundation_test.go`. Tests: event/no-secret and rate-limit regression passed; `fail2ban-regex` matched a synthetic failure and ignored a synthetic success. Verified: isolated source/fixture; no live ban test. Risk: 8443 remains without network-level jail until approved.
3. **Firewall — PARTIALLY IMPLEMENTED.** Existing: panel/firewalld controls; service active. Finding: no proven timed-rule/current-client rollback. Required: safeguards, staging and live access test. Made: read-only status in `security_posture.go`; no firewall policy change. Tests: probe failure handling passed. Verified: active service only. Risk: administrative lockout from unsafe rule changes.
4. **Malware Scan — NOT VERIFIED.** Existing: bounded scanner/UI in `security_features.go`. Finding: history, schedule and quarantine incomplete. Required: controlled job lifecycle and safe restore. Made: read-only scanner-availability status in `security_posture.go`. Tests: source-level availability path only. Verified: no live scan. Risk: stale signatures or missed infections.
5. **WAF — NOT VERIFIED.** Existing: ModSecurity/CRS integration in `security_features.go`. Finding: rules/false positives and per-site operation unverified. Required: ruleset, exclusion and live negative/positive tests. Made: read-only engine status in `security_posture.go`. Tests: no live WAF acceptance. Verified: engine detection only. Risk: overblocking or missed attacks.
6. **SSH security — BLOCKED — APPROVAL REQUIRED.** Existing: password/root SSH and sshd Fail2ban jail. Finding: high-value root login exposure. Required: independent access, rollback, staged hardened settings. Made: read-only effective-config warning in `security_posture.go`. Tests: exact parsing regression passed. Verified: global sshd settings read only. Risk: root credential attack and lockout if changed unsafely.
7. **File integrity monitoring — NOT VERIFIED.** Existing: no trusted baseline found. Finding: no change-detection history. Required: baseline, approval, scan and alert design. Made: visible missing-state dashboard entry in `security_posture.go`; no FIM protection. Tests: no FIM execution. Verified: source gap only. Risk: undetected filesystem tampering.
8. **DDoS protection — NOT VERIFIED.** Existing: firewall/Nginx infrastructure. Finding: no validated attack telemetry or upstream protection. Required: scoped rate rules, observability and provider mitigation. Made: unknown-state dashboard entry. Files: `security_posture.go`. Tests: unknown-state regression passed. Verified: no live attack test. Risk: availability during traffic floods.
9. **SSL/TLS — EXISTING BUT IMPROVED.** Existing: Website Tools issuance/management. Finding: status accepted some invalid or wrong-host certificates. Required: endpoint/chain/renewal acceptance. Made: hostname and validity checks in `system.go`. Tests: `certificate_status_test.go` passed. Verified: isolated source only. Risk: endpoint may still serve a different chain/key.
10. **Email security — EXISTING BUT IMPROVED.** Existing: Postfix/Dovecot/OpenDKIM active. Finding: optional Rspamd installer did not explicitly disable its signer; Rspamd inactive. Required: delivery/open-relay tests, relay details, safe Rspamd rollout. Made: signer disable in `scripts/install-features.sh`. Tests: isolated `bash -n` passed. Verified: source syntax and live service status only. Risk: mail policy/deliverability unknown until runtime tests.
11. **Backup security — NOT VERIFIED.** Existing: private archive/manifest/scoped restore paths in `infrastructure.go` and backup handlers. Finding: unencrypted archives; no off-server restore acceptance. Required: encryption format and verified round trip. Made: dashboard warning only. Files: `security_posture.go`. Tests: no new backup restore test in this slice. Verified: source inspection only. Risk: leaked archive contains customer content.
12. **Administrator access control — BLOCKED — APPROVAL REQUIRED.** Existing: root web service and root host commands. Finding: one app compromise can reach root operations. Required: broker, normal service user, ownership/SELinux compatibility matrix. Made: critical dashboard status and plan. Files: `security_posture.go`, `docs/security-implementation-plan-2026-10-28.md`. Tests: root-status regression passed. Verified: live unit read; no split deployed. Risk: critical privilege concentration.
13. **Security audit logs — PARTIALLY IMPLEMENTED.** Existing: user-manageable activity events. Finding: no independent tamper-resistant security stream. Required: journal retention/export and independent sink. Made: fixed sign-in events in `authentication_events.go`. Tests: secret-redaction/log-injection regression passed. Verified: isolated source only. Risk: root can alter local logs.
14. **Secret management — PARTIALLY IMPLEMENTED.** Existing: root-only config/key files and encrypted selected secrets. Finding: no complete rotation/backup key-loss runbook. Required: history/leak review and rotation procedures. Made: encrypted TOTP seed and hashed recovery codes in `two_factor.go`. Tests: plaintext-absence/persistence tests passed. Verified: local permissions live, factor secrets isolated only. Risk: root compromise exposes keys.
15. **Security updates — NOT VERIFIED.** Existing: panel updater with rollback; installer enables automatic OS security installation. Finding: authenticity and detection/installation separation incomplete. Required: signed releases and staged update policy. Made: documented gate/rollback plan only. Files: `docs/security-implementation-plan-2026-10-28.md`. Tests: no update executed. Verified: source inspection. Risk: unverified update provenance and unexpected OS changes.
16. **Security Dashboard — NEWLY IMPLEMENTED.** Existing: Security menu. Finding: no combined honest status view. Required: live authenticated browser check. Made: read-only API/UI and menu link. Files: `security_posture.go`, `web/security-dashboard.js`, `web/security-dashboard.css`, `web/index.html`, `web/sidebar-groups.js`. Tests: API auth/GET/demo and probe-state tests passed. Verified: isolated source only. Risk: probe coverage is intentionally incomplete.
17. **Secure web terminal — BLOCKED — APPROVAL REQUIRED.** Existing: no terminal endpoint. Finding: root app prevents a defensible root terminal boundary. Required: unprivileged worker, bound attach tickets, reauth, 2FA, broker and threat review. Made: threat model and explicit approval gates in `docs/security-implementation-plan-2026-10-28.md`. Tests: none; no endpoint exists. Verified: not implemented. Risk: severe if a terminal is added before privilege separation.

## Confirmed and suspected vulnerabilities

**Confirmed:** the live panel runs as root, SELinux is permissive, root/password SSH is permitted, the old username path returned before expensive password verification, and certificate status lacked hostname/NotBefore checks. The latter two have source fixes but are not deployed. Existing local activity history is user-deletable by design and cannot provide independent tamper resistance.

**Suspected or unverified:** mail relay openness, WAF rule effectiveness, Rspamd signer behavior on a future install, backup restoration from an off-server copy, and archive exposure through external storage. These are investigation items, not claims of exploitability.

## Tests passed and failed

Passed: isolated Go package tests and `go vet`; targeted authentication race tests; 16 frontend workflow tests; optional installer `bash -n`; synthetic `fail2ban-regex` fixture (one failed event matched, one successful event missed); workspace `git diff --check`. An initial stale settings test harness failed because it expected the previously removed Backup settings submenu; the harness was updated to the current eight-tab behavior and then passed. No unresolved test failure is known in the executed checks. Scanners `govulncheck` and `gosec` were unavailable in checked paths, so no vulnerability-scan result is claimed.

## Production readiness assessment

The currently deployed test server works but does not meet the requested least-privilege or SELinux-enforcing target. The prepared source change is suitable for review and an approved staging deployment; it has not passed live acceptance. Root terminal activation, live SSH/firewall changes and an unprivileged service cutover require separate explicit approval and rollback testing.

The isolated candidate compiled with the updated frontend embedded, with version `0.5.1`, and had SHA-256 `09a44f4bc89e2bb88989af76e0d70c116ad1d425221347a81492ce3e4af44d7d`. It is not the running binary, whose observed SHA-256 was `e4242ec3afebca9836edc1def786090558840d51be147bf9b3193fe59b5e7eeb` at the time of this review. Repeat the hash check immediately before deployment.

## Test and verification limits

The isolated checkout contains the committed baseline plus only the new security source files; it does not contain unrelated dirty workspace files. `go test` and `go vet` passed there. The targeted tests passed under `go test -race`. The JavaScript workflow tests passed locally. No authenticated browser verification of this new UI was done because the running service has not been updated. No live scan, malware deletion, firewall ban, SSH policy change, mail send, backup restore, service restart or destructive test was performed. `govulncheck`/`gosec` were not available in the checked host paths; absence is a tool limitation, not a clean finding.

## Deployment and rollback gates

1. Review the source diff and keep the existing binary, service unit and configuration backup. Build the candidate in isolation and verify its hash.
2. Obtain approval for a panel restart. Deploy the candidate without changing SSH, firewall, SELinux, mail or root-service policy. Check sign-in, site state, static assets, dashboard probes, and 2FA setup in the browser. Restore the previous binary if any acceptance check fails.
3. Obtain separate approval for any panel jail activation or SSH/firewall change. Keep an independent access route and a scheduled rollback before changing network access.
4. Treat the privilege split, SELinux enforcing mode and any root terminal as separate staged changes. The complete compatibility and threat-model plan is in `docs/security-implementation-plan-2026-10-28.md`.

## Highest-priority remaining risks

The live root web process and permissive SELinux remain the main confirmed architectural risks. Root/password SSH access is another confirmed exposure. A compromised root process can also alter local logs and secrets. The unfinished areas above are recorded as incomplete rather than presented as working protections.
