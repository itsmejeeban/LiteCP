# Test-server acceptance review — 27 October 2026

Target: the authorized LiteCP test server and `litimcsellcop.com`. Existing WordPress content and database are preserved. Isolated audit websites, databases, mailboxes, FTP accounts, files, containers and backup stages are retained for review. No email is sent.

## Confirmed fixes

- Scoped database restore now preserves the host's MySQL socket configuration while using the target application's restricted credentials. Empty-target validation includes stored routines and events as well as tables/views.
- Backup and FTP popup requests retain their originating website and popup generation. Older responses/actions cannot overwrite or unlock a newly opened popup.
- Mail installation accepts a separate hostname-matching certificate, validates it before modifying services, retains setup artifacts, and reloads mail listeners after certificate renewal. Roundcube's private CA bundle is readable by its service account.
- Application configuration and Disable wait for an active protected Nginx maintenance response before returning success. This avoids reporting success while old Nginx workers still use the previous route.
- Panel startup reconciliation recognizes configured application sites, including disabled ones, and preserves proxy/maintenance routing and current TLS. It no longer attempts to find a PHP socket in their configuration or repeatedly restart PHP before exiting.
- File Manager no longer claims uploads are limited to four files. Resolver settings explain that the requested Google DNS policy replaces temporary custom values after boot/every five minutes. The migration manual describes automatic host-key discovery and trust confirmation.
- Website Security clearly identifies unavailable antivirus/WAF components and disables actions that require them.

## Browser checks

Windows Computer Use was used with the existing Chrome session. New-tab transitions sometimes caused its URL safety check to stop the turn; checks resumed only after a new user request. Those interruptions are distinct from application errors.

| Earlier request | Observed result |
| --- | --- |
| Website link opens site | A new Chrome tab is created. Public-domain DNS was diagnosed separately; origin HTTPS returns 200. |
| Database creation popup and aligned actions | Create database opens the requested name/username/password form. Password and delete actions share a row. |
| phpMyAdmin in a tab, automatically logged in | Confirmed in Chrome: selected WordPress database with all 12 tables visible. No WordPress SQL was modified. |
| Advanced compact File Manager | WordPress listing, search, selection, file details, read-only editor opening and workspace-only fullscreen were verified. Escape restores the panel layout. |
| Hide/show sidebar; IP beside hostname | Both states and the inline server IP were observed. |
| Optional hosting disabled by default | Node.js, Python and Ruby cards loaded disabled with their version lists. |
| Documentation | All 17 sections loaded; FTP search filtered to four relevant sections. |
| PHP extension search/version catalogue | 1,324 entries loaded, Redis search returned ten matching package entries. No package was installed solely for the test. |
| Global time zones | 598 entries loaded; Kolkata search returned two entries with the current selection retained. |
| Recent Activities | Rows, details, timestamps, pagination and Forever retention loaded with correct panel padding. No activity deletion or retention shortening was performed. |
| Dark-mode profile menu | Profile and Account settings remained readable; the action opened Settings. |
| Services, Backups, Logs, Migration, Firewall, Security | Pages and their controls loaded. Required services showed Running; the log viewer returned content. Root-password mode hides manual fingerprint entry. Security engine absence is represented separately from installed protection. |

## Live integration checks

- Nested manual backup generation/download/inspection and file restoration were exercised on separate audit websites. Inner ZIP integrity, hidden content and file hashes matched.
- After the socket fix was deployed, database restore produced the exact expected fixture row. A second restore was refused because the target was no longer empty; the row remained unchanged.
- On the isolated backup website, five-file upload, hidden files, UTF-8 edit/read, rename, copy, move, permissions, ZIP CRC checks, extraction hashes and traversal rejection all passed. No trash or delete action was used.
- Small cPanel and Plesk archive previews selected the correct document roots, hidden files and SQL dumps. Confirmation, stage ownership, traversal and nonempty-file-target guards rejected invalid requests without changing target content.
- Roundcube authenticated mail page, read-only IMAP INBOX access and SMTP authentication on 465/587 passed with a generated audit mailbox. No message was sent.
- FTPS authentication, chroot confinement and encrypted upload/download passed with the exact server certificate pinned through trusted SSH. The FTP service still uses a self-signed certificate; public-trust validation is a separate improvement.
- Node.js 24, Python 3.13 and Ruby 3.4 each passed isolated application startup, reverse-proxy response, stop, resume of the same container, Disable, and feature-off checks. Final feature switches are disabled and containers are stopped with `restart=no`.
- The final panel build starts with all three configured applications present. A subsequent configure returned only after an immediate protected `503` and maintenance marker were active. All three source paths remained protected after startup.

## Final automated and service checks

- Full Linux Go tests with race detector passed after startup reconciliation was fixed (32.800 seconds); `go vet` passed.
- All 24 Node tests passed, including 11 new popup lifecycle tests and a security-capability availability check. Modified JavaScript syntax and Git whitespace checks passed.
- Nginx configuration, PHP 8.3 pool syntax, mail certificate/name/key preflight, CA-file access under a private umask and maintenance activation fixtures passed.
- Deployed administrator API checks passed for all six retained websites, the 1,324-entry PHP catalogue, activities, hosting, migration jobs, backups, FTP and File Manager. Unauthenticated protected requests returned 401. Google resolvers remained configured.
- The updated panel, original WordPress site, database, PHP, mail and FTPS services are active. The previous binary is retained for recovery. No reboot was needed.

## Limits and recommended improvements

- Native root-SSH migration discovery/trust safeguards are tested, but no external cPanel/Plesk account credentials were supplied. Website/SQL restore does not activate all source account services: mail, DNS, SSL, addon sites, reseller/package settings and arbitrary cron still need migration work and verification.
- External mail delivery needs each mail domain's MX/SPF/DKIM/DMARC records and provider PTR. `tmail.hostingtom.org` currently has no public DNS. Successful authentication cannot guarantee inbox placement.
- Add a supported HTTPS webmail entry point on port 443 if proxying through a service that does not support 8444. Keep the current webmail hostname DNS-only meanwhile.
- Replace the panel IP/self-signed certificate and FTPS certificate with publicly trusted, hostname-matching certificates for routine use.
- Add explicit setup/status actions for optional antivirus/WAF components; showing a feature page alone does not install them.
- Fresh operating-system installation, physical reboot, real external mail delivery and remote-backup storage transfer remain separate environment tests. Destructive operations and existing-password changes were not exercised.
- Tests retain artifacts because automatic deletion was not authorized. Review and remove them only after explicit approval.
