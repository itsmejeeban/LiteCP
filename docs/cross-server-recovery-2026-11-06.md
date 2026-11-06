# Cross-server recovery acceptance - 6 November 2026

Destination: 95.217.167.243, fresh AlmaLinux 10.2, LiteCP 5.0.2. Source: 162.55.50.122. Source services/public DNS were not redirected or modified by destination restore testing.

## Authorization and retained recovery data

User approved source/test transfer, the private 11-site/1-database/1-mailbox snapshot, whole-panel destination replacement, in-memory test credential use, controlled rollback and destination-only reboot. No test credentials or raw private mail headers are recorded here.

Destination recovery snapshot: /var/backups/litecp/system-snapshots/before-cross-server-20261106.zip. Source copy: source-dbmail-20261106-cross-server.zip. Both archive validation and matching source/destination SHA256 751dc53d9378d1093346e3f0e980aa45535cfb5741056c8e953b0587340f7f5f passed. Failed stages, SQL exports, previous files and audit binaries were retained. Installed panel binary was not replaced: corrected Go reconciliation ran through an isolated CLI test binary.

## Confirmed defects repaired in local source

- Snapshot inspector rejected current Argon2id administrator hashes. It now accepts the same bounded/canonical formats as the panel, retaining legacy PBKDF2 compatibility.
- Missing destination service accounts caused late restore failure. Preflight now checks the actual mail/webmail/DKIM trees before managed data replacement.
- Historical snapshots omitted HTTP-context WAF rate zones. Reconciliation now regenerates validated policy zones before Nginx validation; new archives include WAF/traffic shared includes and track generated files for rollback.
- Traffic reconciliation tried to reload deliberately stopped Nginx. Snapshot reconciliation validates/writes without reloading; normal callers retain reload behavior.
- Failed restore rollback left imported SQL/user objects on an originally empty server. Rollback now retains failed SQL/grants before removing only validated managed objects in replacement scope, restores original SQL/users, rejects unmanaged collisions and refuses root/system schemas.
- Container restart errors obscured the original restore exception. Error reporting now retains the original cause as well as service failures.

Temporary test certificate configuration initially used /root, hidden by the broker sandbox. Corrected test-only certificate references to managed TLS storage with retained originals; no panel certificate was replaced or public DNS changed.

## Verified results before reboot

- Fresh installation: no failed units; core services active; web runs as litecp-web without capabilities; project quota active on the dedicated 16 GiB ext4 website mount.
- Full isolated Linux backend suite and go vet passed. Snapshot regression suite: 22 passed. Snapshot deployment frontend fixture passed. Internal CLI checksummed and transferred only for testing; no release package generated.
- Failed retry restored original file/settings state and the repaired SQL rollback left no qa_snapshot_1106 schema/user. Failed data retained.
- Third restore completed successfully. 11 sites, 1 database, 1 mailbox restored. 78 website-file hashes match archive. Database password login and row marker verified. Mailbox password login, message checksum 4be7aabd098d3561ab9f94cb2d777acc223d59c4a5d1a7a6463033dc1bf1fbab and 100 MiB quota verified.
- All ten enabled Node/Python/Ruby applications return HTTP 200 on their individual loopback listeners. Core services active and Nginx configuration valid.
- Guarded SELinux enablement passed; persistent enforcing configuration saved with rollback backup. This changes only destination policy. Installed source prerequisites came from its existing official-package installer.
- Destination reboot scheduled with explicit user approval. Post-boot checks pending.

This report does not establish Ubuntu acceptance, external log retention, reviewed integrity baseline or inbox placement. Live restored panel uses source administrator settings/TLS/hostname as designed; access using source credentials, and do not change public DNS merely to test recovery.

## Subsequent reboot findings and fixes

- First reboot persisted the website quota mount, enforcing SELinux, panel, database and mailbox data. It exposed missing OpenDKIM configuration from an older source archive and a broker startup namespace race on /run/litecp-php73.
- Preserve explicit destination service configurations when absent from an older snapshot; continue replacing site/runtime configs. OpenDKIM's existing narrow traversal ACL is reapplied after extraction (ZIP mode metadata does not retain it). Restored the retained destination signer config and traversal ACL live; signer became active under SELinux enforcing.
- Broker and PHP 7.3 units now share/preserve their runtime directory, eliminating early boot namespace failures. Previous destination units retained in its root-only audit directory.
- Second reboot: zero failed units; core services and DKIM active; no warning-priority broker startup entries. SELinux Enforcing and project quotas active. All 78 restored file checksums, database row/password login, mailbox password login/message/quota still matched.
- Application HTTP probes exposed that snapshot reconciliation set restart=always but failed to enable podman-restart.service on a fresh destination. Added boot-service enablement when applications are restored. Live repair starts only the ten configured apps and disables restart on ten retained duplicate recovery containers without deleting them. Third reboot scheduled to verify persistence.
- Current local patch version is 5.0.3 in application and installers. Destination installed panel remains 5.0.2; isolated corrected CLI handled restoration. Final frontend suite 185 passed; snapshot fixtures 23 passed. Final backend/static confirmation and third-boot results follow.

## Final destination acceptance result

- Final boot ID cb32a16e-fe33-46eb-99ae-2014f816905d differs from the recorded pre-test ID. Four approved destination reboot cycles were used to find and fix boot dependencies.
- Final boot: zero failed systemd units; litecp, broker, Nginx, MySQL, Postfix, Dovecot, OpenDKIM, PHP 7.3 and podman-restart all active. No warning-priority broker/Podman boot entries in the inspected boot. SELinux Enforcing and project quotas active.
- Ten configured application listeners all return HTTP 200 after the final reboot. Database/password row check, mailbox/password/message/quota check and all 78 website-file hashes pass again.
- Additional confirmed application boot defect: Podman restart ran before MySQL's mounted socket existed. Added backed-up/idempotent database-service ordering and RequiresMountsFor=/home/litecp drop-in to the common privilege-service installer used by install/update paths. Live destination received the same ordering fix.
- Retained recovery containers now have restart disabled before replacement so they cannot claim active-site ports after reboot. No containers or recovery archives were deleted.
- Final validation: full isolated Linux backend suite passed, go vet passed; 23 snapshot fixtures passed; 186 frontend tests passed; changed shell scripts pass bash syntax checks. The first large inline installer check hit Windows command-length limits; rerunning via stdin passed.
- Existing Makefile remains. Internal CLI-only build succeeded for recovery testing, its checksum was verified and local staging folder opened. No release ZIP/EXE/installer package was produced, no Git push performed and no installed panel binary replaced. Local release values updated to 5.0.3; installed destination version remains 5.0.2 with scoped live repairs.
- Source fixes need normal updater publication/deployment. The successful restore used an isolated corrected reconciliation CLI; later boot fixes were verified live independently. A clean install/update of the complete 5.0.3 patch and repetition on Ubuntu are not claimed.

These tests close the separate-destination managed-data recovery and AlmaLinux reboot checks for this fixture, and exercise failure rollback with retained file/SQL recovery. They do not prove every production workload, database engine, operating system or provider recovery mode. Integrity baseline review and independent log retention remain unapproved. Existing recovery data and the restored destination remain available for review.
