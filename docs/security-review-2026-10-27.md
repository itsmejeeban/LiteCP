# LiteCP feature and security review — 27 October 2026

Reviewed the current source and the installation at `2.31.109.187`. This is a bounded engineering review, not a certification or proof that the panel cannot be compromised.

## Changes deployed

| Finding | Correction |
| --- | --- |
| Concurrent sign-ins could pass the failure check before failures were recorded, allowing extra password checks. | Reserve attempts atomically before verification and allow only one expensive password check at a time. |
| Failed-login addresses and expired sessions could accumulate without a global bound. | Expire old records, cap tracked login sources at 4,096, and retain at most 128 active administrator sessions. |
| A website-created FIFO could block the file manager before its file type was inspected. | Open managed files without blocking and reject special files; ordinary files/directories still work. |
| Origin checks accepted either HTTP or HTTPS, and did not use browser request context when Origin was absent. | Match the actual request scheme, trust HTTPS forwarding only from loopback, reject cross-origin browser context, and validate Referer when available. Headerless non-browser clients remain supported. |
| Nginx could spool an unauthenticated upload before asking the panel to authorize it. | Stream panel request bodies upstream so unauthorized requests can be rejected immediately. |

Login JSON bodies are additionally limited to 16 KiB. The existing long upload allowance remains available to authenticated users. These changes retain the panel's filesystem sandbox and per-site PHP identities.

## Verification

- Complete Go feature/regression suite and `go vet` passed on AlmaLinux 10.2.
- Added concurrent login, source/session bounds, origin/proxy trust, and Linux FIFO regression tests.
- Official Go `govulncheck` binary scan reported **No vulnerabilities found** for the scanned binary and dependency versions. The hardening did not change those dependencies.
- 37 bounded live checks passed after deployment: unauthenticated access rejection, oversized login rejection, five failed attempts followed by throttling, legitimate login, cross-origin rejection, traversal rejection, extension/migration reads, and all six log sources.
- The upload rejection check declared an 8 MiB body, sent only one byte, and received HTTP 401 without sending the remaining body.
- PHP extensions continued to return all 42 entries across six runtimes. Nginx configuration validation and WordPress HTTPS checks passed.
- The previous browser review visited all 13 main panel pages. Destructive controls were not exercised against live data.

## Existing protections confirmed

MySQL and the Go HTTP listener bind to loopback. Firewalld, Fail2ban, and automatic security updates are active. Panel configuration and environment files are root-only. The application already uses authenticated API handlers, secure/SameSite cookies, output escaping, descriptor-confined filesystem operations, archive traversal limits, local database users, and per-site PHP workers.

## Remaining boundaries and recommendations

- The panel runs as root and has a single administrator account. A stolen administrator credential or a serious application vulnerability could therefore affect the server. MFA and an administrator-IP/VPN restriction remain recommended; neither was configured because enrollment and the allowed network need an owner decision. See [OWASP authentication guidance](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html).
- The panel IP endpoint still uses its installation certificate. Configure a dedicated panel hostname and trusted certificate before routine administration. The WordPress site's trusted certificate is separate.
- Rotate credentials shared in chat and migrate SSH administration to keys. No passwords or access rules were changed during this review.
- Legacy PHP 7.3, 7.4, and 8.1 remain installed for compatibility. Avoid assigning them to new websites and retire them when legacy applications are migrated.
- ClamAV and the optional WAF are not installed. Email delivery/DNS, off-server backup transport, optional extension installation/removal, actual migration into a populated website, and destructive actions require separate controlled acceptance checks. They are not certified working by this review.
- No live data was deleted, no public exploit was attempted, and no high-load test was run. Source tests use their own temporary fixtures.

Cross-origin changes follow [OWASP's origin and Fetch Metadata guidance](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html). Re-run security checks after material code, dependency, or deployment changes.
