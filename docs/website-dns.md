# Website DNS and migration restore fixes (4.0.35)

## Website DNS

Website Tools > DNS and the DNS action beside each website open its zone editor.
Records are saved with the website configuration and therefore included in panel
settings snapshots. Export zone downloads a BIND zone file for another provider.
Edit names, record types, TTL and values, or import existing zone text. Publishing
validates all zones with BIND before switching configuration. Previous revisions
are retained for recovery. Renaming a website updates zone owner/target names;
removing it unpublishes its zone, retaining old revision files.

New installations install a separate authoritative BIND service by default.
Set LITECP_INSTALL_DNS=0 to opt out. For existing installations, run
`sudo bash scripts/install-dns.sh` from this source checkout, then restart LiteCP
once to apply its DNS configuration-directory sandbox allowance. Without this
service, the editor saves drafts and supports export. It never reports a draft
as published. The installer refuses an existing active or enabled distribution
DNS service; it does not stop or replace it. Ubuntu distribution named/bind9
units are masked so only litecp-dns owns the new listener. AppArmor and SELinux
remain enabled, with narrow DNS configuration/runtime permissions.

Allow inbound TCP and UDP 53 in the provider firewall. The installer handles the
active host firewall; Apply LiteCP Firewall also includes DNS when installed.
Delegate the domain and configure glue at the registrar to use this server.
Saving a zone does not alter Cloudflare or the registrar. Confirm public IPv4/
IPv6 addresses and NS/MX/SPF records before switching. Repointing source-server
A/AAAA records during migration preserves unrelated external service addresses.
Source DNSSEC signing records require separate key migration and re-signing;
LiteCP stops for review instead of publishing invalid signatures. Additional
cPanel addon-domain zones require separate hosted-domain setup and review.

## Restore repairs

- Empty default cPanel Maildir folders no longer masquerade as mail domains.
- Nonempty default-account messages are retained in a locked mailbox. Set its
  password in LiteCP before use. Named SHA512-CRYPT mailbox passwords are retained.
- Unmatched mailbox data and unsupported password hashes remain explicit review
  errors. Existing imported messages are not overwritten on retry.
- Laravel literal .env values support CRLF. Scoped PHP db/database configuration
  arrays support name/user/pass while preserving unrelated application secrets.
  Multiple/dynamic database configurations still require explicit mapping.
- Repeated directory headers are accepted; duplicate files, traversal and links
  remain rejected. Live captures allow up to one million archive entries within
  the existing 64 GiB expanded-size limit. Manual archive limits are unchanged.
- Supported cron schedules and commands migrate with website paths/PHP rewritten.
  Unsupported environment/macros remain disabled for review, never executed just
  to inspect a backup.
- cPanel captures now carry the primary domain DNS zone. Old captures without a
  zone must be recaptured to import it; existing saved zones are preserved.
- A successful local HTTP check marks a restore completed. HTTP failures remain
  pending review. This check is not proof of mail delivery or every application
  page. DNS, SSL, public mail routing and application acceptance need verification.

Existing pending jobs are unchanged until Retry restore (or a new capture when
staging was cleaned). Existing-domain replacement still requires confirmation.
No automatic destructive live retries were performed during development.

## Validation

Run `make test` on supported Linux, or targeted Go tests for WebsiteDNS,
CPanel, MigrationConfigCRLFAndScopedPHP and MigrationRepeatedDirectoriesAndDuplicateFiles.
Run `python -m unittest discover -s scripts -p test_live_source.py` for the exporter.
Run `node --test scripts/website-dns.test.cjs` for editor pagination and draft
preservation checks.
The established Makefile remains the build/test entry point; update.bat remains
the user-triggered publish/deploy workflow. No update or deployment is automatic.

Local verification: targeted Go regression tests passed; exporter tests ran 11
tests with one platform-specific skip; all three DNS editor tests passed.
Changed JavaScript syntax checks and `git diff --check` passed. The broader
frontend-workflows test file has four failures also reproduced against HEAD
before these changes. The full Windows Go suite has Linux-specific fixture
limitations. The DNS installer has not been executed, and authoritative queries,
live browser workflows and real mailbox/cron delivery need Linux acceptance
testing after the user-triggered update. No production-readiness claim is made.
