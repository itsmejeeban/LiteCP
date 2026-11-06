# LiteCP security gap audit - 6 November 2026

## Scope and evidence rules

User requested the attached security-audit prompt, excluding previously completed panel tests. This audit uses the existing feature/security reports and conversation history as prior evidence; it does not repeat the UI CRUD, restoration, stress, brute-force, CAPTCHA or migration acceptance exercises already performed. Old evidence is identified as historical, not a current certification.

Source at start: 4.0.158, clean working tree. Local repairs: **4.0.159**, not deployed by this audit. The supplied prompt ends in Phase 9, item 10 (file permissions); no unseen later phases were assumed.

Reference framework: [OWASP Top 10:2025](https://top10.owasp.org/2025/0x00_2025-Introduction/) and [ASVS 5.0.0](https://asvs.owasp.org/downloads/). This is a gap-oriented engineering audit, not a completed ASVS certification or a guarantee of security.

## Architecture and attack surface

- Go single-binary backend with embedded HTML/CSS/JavaScript; no framework or public registration.
- Nginx terminates panel TLS and proxies to the loopback frontend. Public static assets and build/version information are intentionally anonymous.
- `litecp-web` has no effective systemd capabilities. A root broker accepts allowlisted routes through a UID-checked Unix socket; it performs authentication/authorization. Browser root-terminal operations are forbidden in separated mode.
- Administrator identity/settings, API rules, inventories and job state use private JSON files. Website databases use local MySQL. Mail is Postfix/Dovecot, with native mailbox password hashes.
- Browser authentication uses bounded in-memory sessions, strict/HTTP-only cookies, origin checks, optional TOTP/recovery codes and trusted-browser authentication. API credentials use random secrets stored as hashes, explicit action rules, expiration/revocation, IP restrictions and rate windows.
- Hosting administration includes website files and archives, SQL import, mail, DNS, cron, FTP, SSL, Redis, resource runtimes, services, firewall, updates, backup destinations and SSH migration sources. These are privileged administrative capabilities, not ordinary tenant permissions.
- Filesystem execution boundaries include per-site users, descriptor-relative Linux file access, staged imports and dedicated runtimes. Application uploads may intentionally contain executable PHP/application code; banning executable uploads globally would break a hosting panel. Administrator authentication and confinement are the relevant boundaries.
- Background surfaces include capture/restore workers, backup schedules, system snapshots, resource enforcement/recovery, scanning, integrity checks, cron, updates and CAPTCHA gateway processes.

The separate [API inventory](security-api-inventory-2026-11-06.csv) records **181 catalog actions and 20 legacy aliases**, methods, paths, access boundaries and availability. Catalog permissions remain explicit; no new operation is automatically granted to an old key.

## Confirmed defects fixed locally

### SGA-01: API body read before credential rejection - Medium

An attacker supplying plausible-length but invalid API headers could cause the JSON transport to read its body before credential validation. On the long-lived HTTP/broker transport, slow unauthenticated bodies could occupy privileged workers. No load attack was run.

- Reproducer: a body reader records whether an invalid key causes any read. Before repair it was read and returned 400; after repair it is not read and returns 401.
- Fix: check secret, enabled state, expiration, revocation and source-IP allowance before parsing a body. The authoritative check remains after the mutation lock so credential/rule changes still take effect.
- Tests cover both JSON and streaming API paths, wrong secrets, disabled/expired/revoked keys and forbidden source IPs.
- This does not claim that an authenticated slow client or every network denial-of-service scenario is eliminated.

### SGA-02: JSON body-limit enforcement gap - Low

`io.LimitReader(limit+1)` allowed a valid object followed by excess whitespace to be accepted because the decoder treated the artificial reader EOF as a real end of body. The reader bounded work; this was a configured-size-policy bypass, not unlimited JSON allocation or remote code execution.

- Reproducer failed before repair and passed afterward.
- Fix: track the limited reader budget and reject consumption of the overflow byte.
- Exact-limit bodies remain accepted. Oversized objects, excess trailing whitespace, additional JSON objects and unknown fields are rejected.

## New verification performed

| Check | Evidence/result |
| --- | --- |
| Management endpoint authentication | New matrix tested **180 distinct protected method/path combinations**; anonymous requests with forged API/grant headers returned 401. Public `/api/version` is excluded intentionally. |
| Mutation CSRF boundary | Every non-GET route in that matrix rejected a valid browser session accompanied by an attacker Origin with 403, before the underlying operation. No management mutations were executed. |
| Early API rejection | New tests on `/api/v1` and `/api/v1/request` confirm invalid/restricted credentials never read the supplied body. |
| JSON parser limits | New exact-boundary, overflow, trailing-object and unknown-field cases passed. |
| Affected API regressions | `go test -run '^TestAudit|^TestAPI' .` passed using isolated configurations. Re-running these impacted tests was necessary to validate the authentication repair. |
| Static analysis | `go vet ./...` passed. |
| Current dependency exposure | Official `golang.org/x/vuln/cmd/govulncheck` v1.9.0, source scan with Go 1.27.3: **No vulnerabilities found**. This means no known reachable advisories reported by this tool/database, not absence of all vulnerabilities or proof about every installed package. |
| Live unauthorized access | Five read-only probes to state, API management, snapshots, integrity and logs all returned 401 without browser credentials. |
| Live service isolation | `litecp.service`: active, `User=litecp-web`, `NoNewPrivileges=yes`, empty capability set. Broker active as root. SELinux Enforcing. |
| Live storage/network configuration | MySQL listener 127.0.0.1:3306; frontend 127.0.0.1:9090; configuration/environment files 0600 root:root. Broker socket 0660 root:litecp-web, parent 0755 root:root. |
| Live HTTP response headers | CSP with script-src self and frame-ancestors none, X-Frame-Options DENY, nosniff, Referrer-Policy and Permissions-Policy observed. No HSTS observed on the tested IP endpoint. |
| Update/abuse services | dnf-automatic-install timer and Fail2ban active; this does not prove all packages are current. |
| SMTP relay rejection | Configuration contains permit_mynetworks / permit_sasl_authenticated / reject_unauth_destination. One external unauthenticated probe received **554 5.7.1 Relay access denied** at RCPT. No DATA command or email message was sent. This is a bounded relay check, not all mail-abuse scenarios. |

## Prior evidence reused, not repeated

- Login concurrency/admission, bounded sessions, timing protections, spoofed non-loopback forwarding/origin checks: security reviews of 27 October and 3 November, security foundation/audit tests.
- Linux FIFO/path isolation, SSRF URL filtering/pinned DNS, archive traversal and migration retention: prior source reviews and existing regression tests.
- Privilege broker peer identity, frame bounds, denied root shells, unprivileged process and stream behavior: `privilege-separation-validation-2026-11-04.md` and prior practical tests. Older reports claiming the web process still runs as root are superseded by the live identity evidence above.
- File Manager, databases, backups/restoration, cron, FTP, SSL, resources, traffic, DNS/PHP/WAF, email, integrity, services, terminal, migrations, analytics, updates, API, CAPTCHA and both color modes have prior feature-specific reports/conversation results. Their basic CRUD/UI tests were not rerun here. Their existence does not establish every adversarial scenario.

## Prompt coverage and remaining acceptance gaps

| Prompt area | Assessment |
| --- | --- |
| Reconnaissance | Architecture, administrative entry points, integrations, workers and API inventory recorded above. |
| Authentication | Prior login/session/MFA tests reused. Current administrator hash is PBKDF2-HMAC-SHA256 with 600,000 iterations and random salt; it is not plaintext. **It does not meet the prompt's exact Argon2id/bcrypt preference.** A compatible hash migration remains open; hashes were not silently replaced and current users were not locked out. No public registration, email-verification registration flow, payment processing or consumer account recovery exists in this panel. |
| Authorization | New route matrix verifies anonymous and cross-origin rejection. Existing API denial/expiry/revocation/grant tests reused and affected regressions rerun. API keys are server-administrator integrations with action scopes, not per-website tenants; per-site API ownership scopes are not implemented. There is no normal panel-user role to impersonate for a two-user IDOR test. |
| Web vulnerabilities | Parser flaw fixed; existing SSRF/traversal/origin/encoding controls reused. This audit is **not** a complete independent stored/DOM-XSS taint analysis, HTTP request-smuggling wire test, or browser/session-hijacking campaign. XML/XXE is not an identified application parser surface. |
| API | All catalog actions inventoried, negative route matrix added, slow unauthenticated-body flaw fixed, impacted credential regressions passed. Operation-specific parameter validation exists in individual handlers but was not exhaustively fuzzed for every action in this gap pass. |
| Abuse prevention | Existing login/job/quota and CAPTCHA tests reused; early invalid-key body rejection repaired. Authenticated resource exhaustion, distributed attacks, disk-filling and sustained concurrency tests were not run against live data. Local-process spoofing of trusted loopback proxy headers remains a threat-model limitation; IP allowlists should not be treated as protection against an already compromised local website. |
| Database | Prior database CRUD/import/credential tests reused; loopback MySQL observed. No live SQL exploitation or destructive database test performed. Arbitrary application SQL and each customer application's database security are separate scopes. |
| Files | Prior upload/traversal/archive/download/isolation evidence reused; current private file modes checked. No adversarial concurrent mount/rename campaign or quarantine/restore deletion was rerun. Stored backups are sensitive and are not generally encrypted at rest by the current archive format. |
| Infrastructure | Read-only identity, SELinux, listeners, file modes, headers, SSH launch settings, mail policy and certificate metadata inspected. External firewall/provider exposure and every installed package/CVE were not independently certified. |

## Operational findings not silently changed

1. **SSH hardening:** root/password login, X11 forwarding and TCP forwarding remain enabled in `sshd -T`; the active service launches sshd with `-p 369`, overriding the file's port 22. Restricting SSH needs a verified key/second access path and rollback plan. No password, SSH, firewall or service changes were made.
2. **Panel certificate:** the IP endpoint presents issuer/subject CN=LiteCP, a self-issued installation certificate. Its validity dates alone do not establish trusted identity. Use a managed panel hostname and publicly trusted certificate before routine remote administration. No certificate-verification control was disabled in the panel. Read-only loopback inspection used curl -k only to read this known test endpoint.
3. **Password-hash policy:** PBKDF2 is not the exact requested Argon2id/bcrypt scheme. Migrate compatibly (retain verification of old hashes and rehash after successful authentication), with bounded cost and login tests, before claiming this policy item complete.
4. **Archive-at-rest protection:** filesystem permissions and encrypted transport do not encrypt snapshot/backup content. Format/key management and recovery compatibility require a separate design.
5. **Broker privilege boundary:** the broker remains root and has broad system capabilities for administration; only the network-facing service is unprivileged. This is not proof that every allowlisted operation is harmless after administrator compromise.

## Reproduction / handoff

- Tests: `go test -run '^TestAudit|^TestAPI' .`
- Static analysis: `go vet ./...`
- Vulnerability scan: `go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 .`
- Findings tests: `security_gap_audit_test.go`.
- Existing Makefile build/test/run entry point retained. No production build, release package, deployment, credential rotation, filesystem cleanup, database mutation or live service restart was performed.
- Local version bumped to 4.0.159. The live server continues to run its existing installation until the user updates it. Full safe adversarial acceptance of the gaps listed above remains outstanding; this report must not be described as 'everything is secure'.
