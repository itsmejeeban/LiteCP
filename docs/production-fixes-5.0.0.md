# 5.0.0 hardening and layout follow-up - 6 November 2026

This follows the original production-readiness report; it does not erase its historical findings.

## Fixed in local source

- Restored Ctrl+K panel search, including desktop collapsed-sidebar and mobile navigation behavior.
- Fixed PHP health reporting for dedicated resource-enforced workers and intentional suspension/resource blocking.
- Fixed BIND working directory and systemd RuntimeDirectory ownership. Configuration remains root-owned; named receives only CAP_NET_BIND_SERVICE. Added idempotent retained-backup repair to both update paths.
- New passwords use Argon2id (19 MiB, 2 passes, 1 lane). Legacy PBKDF2 verification remains supported, with conditional rehash after successful authentication; failed persistence retains the previous hash and logs a non-sensitive warning.
- Updated stale frontend browser stubs and expectations without removing cancellation, stale-response, escaping or destructive-confirmation assertions. Added mismatched-domain rejection coverage.
- Reduced website summary-column widths (Application 90px, Runtime 110px, Size 72px, Database Size 105px); extra space is assigned to actions, and icons keep their existing sizing.

## Verified live operational repairs

- Existing authoritative DNS now starts and refuses recursive queries. No zone records or registrar delegation were changed.
- Production hostname litecp.litimcsellcop.com resolves to this server. A trusted certificate was issued using the existing ACME account; Nginx routes were preserved, validated and reloaded with retained backups. Chrome opens the hostname login page without a certificate interstitial.
- Panel URL: https://litecp.litimcsellcop.com:4321/
- Webmail URL: https://litecp.litimcsellcop.com:8444/ (HTTPS 200).
- OpenDKIM installed/configured through the existing mail feature installer. Postfix/Dovecot/DKIM, DNS, panel, broker, Nginx and MySQL are active. Configuration backups are retained by the installer.
- DKIM public DNS lookup for the existing audit mail domain fails; the signing service alone does not establish delivery readiness. Publish the generated key in the authoritative DNS provider before claiming signed delivery.

## Validation

- Frontend: 182/182 checks passed.
- Earlier complete Linux backend suite after DNS/PHP repairs passed; 23 Python fixtures and shell syntax checks passed.
- Local selected password/PHP/DNS/parser tests and go vet passed after final edits. Go telemetry could not write its sandbox token; tests/static analysis completed successfully.
- Official govulncheck reports no reachable/imported-package vulnerabilities. It reports advisories in unused packages of the compatibility-pinned crypto module; this is not a claim that every module/package or OS is advisory-free.
- Final Linux full-suite/race confirmation is recorded separately in the final handoff after completion.

## Still required for unconditional production sign-off

- User authenticator enrollment and recovery verification (MFA cannot be safely enrolled on the user's behalf).
- Verified SSH key/second access route before disabling root/password login; the current updater still depends on password SSH.
- Public DKIM/SPF/DMARC/MX/PTR publication and a real mail-delivery acceptance test. Domain provider/registrar access was not supplied.
- Separate-destination disaster recovery, fresh installation/reboot/rollback acceptance on all advertised OS versions, and review of integrity changes before approving a new baseline.
- General archive-at-rest encryption and external tamper-resistant security logging remain separate design/operations boundaries.

The local version remains the requested 5.0.0. The live binary is still 4.0.160 until the user runs the updater. No production binary/package was generated or deployed. Existing Makefile retained. No user website, database, mailbox data, backups or audit baselines were deleted or silently reset.

## Updated-panel acceptance - 6 November 2026

- Live version endpoint reports 5.0.0, build 908122a81cc0083aba6156480ae6bff37e344d02. This supersedes the older deployment-status paragraph above.
- Read-only service checks confirm panel, broker, Nginx, MySQL, Postfix, Dovecot, OpenDKIM and authoritative DNS active. Webmail returns HTTPS 200; DNS runs as named with CAP_NET_BIND_SERVICE and its writable runtime directory.
- Chrome on the trusted hostname confirms the dashboard renders, including running PHP state and backup ages. Website summary widths are 270/90/110/72/105 pixels, and Ctrl+K focuses panelSearchInput.
- Found and fixed locally: enabled Node.js/Python/Ruby websites incorrectly displayed the PHP application logo. Rendering now uses the active runtime icon and escaped version title; disabled/unknown runtimes preserve the configured application icon. app.js cache version updated to 5.0.0. This new source change is not deployed by this audit.
- Complete frontend suite after this repair: 184/184 passed. app.js syntax check passed. Existing Makefile retained; no build, release package or deployment performed in this acceptance pass.
- User reports successful 2FA testing. Enrollment was not changed by this audit; prior server inspection still reported the current administrator enrollment disabled. Functional testing and actual production enrollment are separate checks.
- Production acceptance remains conditional on public mail DNS/delivery, key-based SSH with an independent recovery route, separate-destination restore acceptance, fresh-install/reboot/rollback coverage and integrity baseline review. DNS provider and separate recovery destination have not been supplied. Do not present these as completed.
