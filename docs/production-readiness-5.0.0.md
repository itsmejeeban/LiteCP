# LiteCP 5.0.0 production-readiness review

Date: 6 November 2026. Decision: **not approved for general production release yet**. The local version is 5.0.0; the installed server was not deployed or restarted during this review.

## Scope

A clean checkout was inspected, along with the existing security/feature audit evidence. The complete Linux Go regression suite, all frontend test files, all top-level JavaScript syntax, Python script syntax and exporter fixtures were checked. Chrome was used for Security Dashboard, File Integrity Monitoring, Overview and Services. Read-only server inspection resolved the PHP/DNS/DKIM warnings. This is not a claim that every source line or all possible security attacks were independently proven safe.

The version was updated consistently in main.go, scripts/install.sh and scripts/bootstrap-install.sh. Historical reports and asset cache identifiers were preserved; changing an unchanged asset's cache identifier is not required for the version endpoint to report the new release.

## Verification

| Check | Result |
| --- | --- |
| Complete Linux backend suite | `go test -p=1 ./...` passed in a separate source copy, including the local 5.0.0 version values. |
| Go static analysis | `go vet ./...` passed locally and on Linux. |
| Frontend test suite | **181 tests: 158 passed, 23 failed. Release test gate is not green.** |
| JavaScript syntax | All 70 top-level web/*.js files passed node --check. |
| Python syntax | All 11 scripts/*.py files parsed. |
| Linux migration exporter fixtures | All 20 tests passed. |
| Published shell syntax | Installer, bootstrap and deploy script passed bash -n with the LF endings required by .gitattributes. The initial direct Windows-file copy retained CRLF and failed shell parsing; the isolated copy was normalized to the actual publishing policy and rechecked. No live installer was run. |
| Dependency exposure | Prior source govulncheck v1.9.0 scan reported no known reachable advisories. Dependencies are unchanged in this version-only update; this is reused evidence, not a new package/OS vulnerability certification. |
| Recent deployed security repair | Previous live 4.0.160 retest returned complete 401 JSON responses in 20/20 withheld-body checks and rejected oversized JSON. Not repeated during this review. |
| Current Chrome/server state | Unprivileged web frontend and enforcing SELinux remain reported; running panel and core database/web/mail/FTP services are visible. Specific unresolved warnings are listed below. |

## Required before production sign-off

### 1. Repair the frontend release test gate

Several failures are demonstrably stale tests or incomplete browser mocks, rather than evidence of a broken feature:

- Appearance tests require an old cache version and multi-theme presets, although the requested product now has only Default. A test VM also omits Event.
- Backup popup fixtures omit window.addEventListener, which the browser implementation uses.
- Settings/navigation and file editor extraction tests omit dependencies such as $ or $$.
- Confirmation tests expect old labels (Enable SSL / Detach website) while the current labels are Install SSL / Remove permanently.

These tests must be updated to exercise the current intended behavior without weakening meaningful safety assertions. Other failures still need individual triage; do not equate all 23 failures with harmless mock drift or with 23 confirmed product defects. The complete failing-check list is below. No tests were edited simply to make this review green.

### 2. Correct the PHP health-reporting discrepancy

Chrome Overview and Services show Multi-PHP FPM as Degraded. Read-only systemd inspection found every listed PHP 7.3/7.4/8.1/8.2/8.3/8.4/8.5 service active/running. The resource-isolated audit website has no shared Remi FPM socket and does have its dedicated resource runtime php.sock. All other inspected websites have their shared socket.

Source phpStackState in system.go checks only sitePHPSocket for every site, while resourceNativeConfigsAt in resource_runtime.go deliberately switches isolated sites to their dedicated socket. This is a confirmed health-display bug, not proof that every PHP worker is down. The health checker should understand dedicated resource runtimes and their intentionally blocked/recovering states before claiming service health. No runtime behavior was changed in this review.

### 3. Resolve authoritative DNS availability

litecp-dns.service is failed. The retained journal reports loading configuration: permission denied and repeated-start failure. This needs a scoped ownership/permissions/SELinux investigation and a DNS query test after repair, not an unrestricted chmod or blind SELinux disable. Do not depend on LiteCP authoritative DNS in production while it is stopped. Existing external DNS deployments are a separate configuration.

### 4. Complete production administrator and TLS setup

- Chrome shows administrator 2FA not enabled. Enroll and verify recovery access before exposing routine administration.
- Prior live evidence shows root/password SSH permitted. Verify a key and alternate console access before restricting them; do not lock the operator out.
- The panel IP endpoint uses the LiteCP installation certificate. Configure a managed hostname and trusted TLS certificate for production administration.

No administrator credentials, MFA enrollment, SSH/firewall settings or certificate settings were changed by this review.

### 5. Finish mail and integrity acceptance

- DKIM signer is not merely idle: systemd reports opendkim.service as not-found. If hosted email is a production feature, install/configure the signer and verify DKIM DNS plus actual signed delivery. SMTP/IMAP being active alone does not prove mail production readiness.
- The integrity dashboard shows **37 changes** since the 5 November baseline. Entries include audit-created sites, mail maps, WAF/resource units and the updated binary. These may reflect authorized work, but this review did not compare/approve every changed file. Review the differences before accepting a new trusted baseline. No baseline was silently approved.
- Webmail is reported not configured. Configure and verify it if included in the production offering.

### 6. Close release/operations acceptance gaps

- Exercise a complete off-server recovery on a separate disposable destination, including database/mailbox credentials and background workers; existing same-server tests are not a disaster-recovery substitute.
- Record fresh installation, reboot, upgrade and rollback acceptance for each advertised supported OS. Only syntax and isolated regression checks were performed here.
- Use maintained PHP versions for new production sites; isolate or retire legacy versions according to a documented compatibility policy.
- Retain independent backups and security logs. Existing archive permissions/transport do not provide general archive encryption at rest.
- Upstream DDoS protection remains necessary for volumetric attacks; CAPTCHA is not a replacement.
- The requested Argon2id/bcrypt hash-policy migration is still open. Current PBKDF2-HMAC-SHA256 uses 600,000 iterations and random salt; it is not plaintext, and a policy mismatch is not evidence that hashes are already compromised.

## Failed frontend checks

- changes in another tab synchronize the selected theme
- every preset has styles and appearance has a routable settings entry
- Recent Activities loads when navigation uses pushState without a hashchange
- Recent Activities ignores an older response and preserves a focused policy choice
- file editor saves to its captured website and stays open during the save
- mail action enhancements stay single across repeated retained-table refreshes
- integrity page is admitted, grouped and loaded after dashboard
- keyboard selection, Enter, Escape and Ctrl+K work without invoking settings actions
- keyboard shortcut reveals hidden navigation and narrow results stay inside the viewport
- all generated password entry points use the shared copy control
- Generate is a secondary button and still fills password confirmation
- backups: a closed/reopened popup ignores the previous website's list
- backups: reopening the same website rejects old errors and closed responses
- backups: an old mutation success cannot refresh or unlock the newer popup's mutation
- backups: an old mutation error cannot refresh or unlock the newer popup's mutation
- backups: obsolete inspect completion cannot replace review or refresh the current website
- backups: obsolete upload completion cannot replace review or refresh the current website
- backups: obsolete restore completion cannot replace review or refresh the current website
- security actions reflect installed capabilities and recover after installation
- Settings expands and collapses eight ordered sub-tabs without changing the open page
- main navigation retains settings sub-routes while activating the Settings page
- SSL requests wait for the validated email dialog; cancelling sends no request
- detach keeps its destructive scope and waits for explicit styled confirmation

## Disposition

The backend regression results support continued controlled staging/pilot use. They do not justify an unconditional public production-ready claim while the release test gate and operational items above remain unresolved. Version 5.0.0 is a version update, not a security certification.

The existing Makefile is retained. No production build, release ZIP/EXE/installer, package, live deployment, service restart, data deletion or database mutation was performed. Test fixtures were created only in isolated audit copies; no release output folder was opened because no release build was requested.
