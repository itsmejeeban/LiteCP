# LiteCP fresh-install and real-service QA

Date: 28 October 2026. Status: **IN PROGRESS**.

## Authorized environment

- Dedicated test server identity verified over SSH: AlmaLinux 10.2, matching supplied public IP.
- Initial state: freshly reset server, no LiteCP/Nginx/MySQL/mail packages or existing website data. About 924 MiB RAM and 19 GiB disk.
- Requested permanent domain: `litimcsellcop.com`; public A resolution verified to the test server.
- Panel public HTTPS port: **4321**, as requested during this run.
- Nginx architecture retained. LiteSpeed was explicitly excluded by the user and is not part of this audit.
- External mail test recipient authorized by the user. No external messages are sent except the explicit test.
- No project output ZIP requested or created. Installer source was transferred as a temporary TAR because anonymous GitHub archive access returned HTTP 404.

## Feature inventory and initial coverage

| Module | Implemented interface / operations | Current result |
| --- | --- | --- |
| Accounts | One administrator with password, optional TOTP/recovery and trusted-browser mode | Separate customer accounts, roles and subscriptions: NOT IMPLEMENTED |
| Websites | Add, domain edit, PHP version, SSL, application cron, detach | Pending live workflows |
| File Manager | Home/public_html, tree, files/folders, upload/download, text/HTML editor, copy/move/rename, permissions, preview, archives, trash/restore, search/preferences | Pending live workflows |
| Databases | Local MySQL creation/user/password/removal; phpMyAdmin launch | Pending live workflows |
| FTP | Per-website Pure-FTPd virtual accounts, password change/removal, mandatory TLS and passive ports | Pending installation and protocol tests |
| Mail | Postfix, Dovecot, OpenDKIM, domains, mailboxes, password, quota, aliases, DNS suggestions, webmail | Pending installation and protocol tests |
| Website backups | Nested ZIP creation/download/delete/inspect; restore files and SQL into empty targets | Pending live restore |
| Global backups | Local generation, remote SFTP/FTPS/FTP destinations, settings/history | Remote provider tests require authorized destination credentials |
| Security | Posture, firewall, optional malware scanner and WAF | Capability/negative tests pending |
| Application Hosting | Optional container Node.js/Python/Ruby and version selection | Disabled by default; pending tests |
| Migration | Native backup import, live root SSH key/password cPanel/Plesk/LiteCP capture | Live source migration BLOCKED: no source credentials |
| Settings | Administrator password/email, timezone, hostname, DNS, developer mode, update, reboot | Pending safe lifecycle checks |
| Other | Services, logs, activities/retention, analytics, documentation, quick search | Pending browser regression |
| Authoritative DNS | No panel Cloudflare zone-management API identified; resolver settings are local server DNS | Cloudflare records handled separately only if necessary |

The source stores panel state in a protected JSON file; application data uses MySQL. Website runtime UIDs are distinct, but this is not customer-login authorization isolation.

## Confirmed issues and changes

| ID | Severity | Issue / reproduction | Root cause / correction | State |
| --- | --- | --- | --- | --- |
| QA-001 | High | Fresh installer aborted at default resolver verification even though the desired resolvers appeared shortly afterward | NetworkManager `reapply` returns before resolv.conf converges. Added a bounded, cancellable verification wait in `main.go` / `network_features.go`, with delayed-success and cancellation regression tests. | Focused Go tests PASS; installer retest in progress |
| QA-002 | Requested change | Change panel HTTPS port from 8443 to 4321 | Updated installer listener, SELinux port registration, firewall rules, displayed access URL and README; frontend protected-port row follows the current port; backend protects 4321 while retaining legacy-port protection. | Live installation in progress |

## Evidence and boundaries

- Installer output retained in a root-only test-server working directory. Secrets are not included in this report.
- 78 frontend regression tests passed after the port change; JavaScript syntax check passed.
- The first installer run reached MySQL initialization and Go compilation, then failed at resolver verification. The resolver profile was independently read back with exactly 8.8.8.8 and 8.8.4.4 afterward, confirming delayed convergence.
- A resumed install and focused Go tests were started after the correction. SSH subsequently became unreachable; their final results cannot yet be confirmed. TCP checks for ports 22 and 4321 failed. Neither installation completion nor domain creation is claimed.
- Access returned after a server reboot. The focused resolver/firewall Go tests then passed. A 2 GiB temporary swap file was enabled to support compilation on the 1 GiB server. Installation resumed sequentially, with webmail deferred until a matching TLS certificate exists.
- The authenticated Cloudflare page was inspected: apex, wildcard and `litecp` A records all target the designated test server and use DNS-only mode. No records were changed.
- `scripts/hosting-customer-qa.cjs` prepares browser provisioning/file/database checks using test-prefixed websites and sanitized results. It has passed syntax checking, but has NOT been executed against the unfinished installation.
- Unrelated pre-existing local edits are excluded from the reinstall source.
- PASS will be recorded only after executing the operation and verifying the underlying service or stored data. A success notification alone is insufficient.
- Tests use clearly named temporary resources. The requested permanent domain will remain configured.
- Reboot, external delivery, TLS trust, large-upload limits, restore integrity and multi-website isolation need live evidence before production-readiness approval.

## Cloudflare change log

No records changed so far. Existing domain resolution is correct. DNS-only mail records will be evaluated after the mail hostname and DKIM key exist.

## Assessment

Not yet approved for production. Installation and real-service testing are ongoing. Unimplemented customer-account isolation must not be described as a passing multi-tenant hosting test.
