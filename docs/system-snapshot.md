# System Snapshot

Backups -> System Snapshot creates a single ZIP containing LiteCP settings and administrator credentials, website home directories (including private files and website backups), managed MySQL databases and user grants, mail messages/accounts/aliases/DKIM, FTP state, cron definitions, TLS certificates, webmail settings and managed service configuration. Application container images are included when configured.

This is an application-data snapshot, not a disk image. Install the same OS major version/architecture and a compatible LiteCP/MySQL version first. Install the source PHP runtimes and optional mail, FTPS, WAF and container services before restoring. The archive does not install operating-system packages, copy SSH access, or replace the destination MySQL root account. Network addressing and firewall access are retained; point domain DNS at the destination after verification.

Create: click **Create system snapshot**. The operation pauses hosted web, PHP, FTP, mail and cron services while capturing consistent data, then resumes them. The panel reconnects when services return. Download the resulting ZIP and keep it private: it contains private keys, password hashes, database data and encrypted credentials with their recovery key.

Restore: upload the ZIP or place it in `/var/backups/litecp/system-snapshots/`, click **Refresh files**, then **Inspect & restore**. Validation checks the archive layout, checksums, links, platform and available space. Type `RESTORE ENTIRE LITECP` to authorize replacement. The source administrator password and two-factor configuration replace the destination's settings, so keep source login credentials available.

A recovery ZIP and previous data are retained in the snapshot directory before overwrite. On failure, the restore attempts to put destination files and database data back; failed restored files are preserved separately. Read operation status in the panel and `journalctl -u litecp-system-snapshot`. Never expose the snapshot directory through a website. Existing browser trust and webmail sessions are not restored.

For portable backups, external symlinks and special files are rejected instead of silently omitted. Databases not registered in LiteCP and data stored outside LiteCP's managed roots are not included. Quiesce application containers and external writers during capture; third-party external services are outside this backup's scope.
