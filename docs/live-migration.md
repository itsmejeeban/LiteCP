# Live cPanel / Plesk migration

The Migration page supports root SSH account discovery, native backup capture, SHA-256 checked transfer, and review of compatible website files and SQL. A captured backup is not a completed account migration.

## Requirements

- Linux cPanel/WHM or Plesk source, root SSH, Python 3, and working native panel backup tools.
- A dedicated unencrypted SSH private key and independently verified SHA256 host-key fingerprint. Password-only SSH and Plesk for Windows are unsupported.
- Enough free source space for both native and portable exports. LiteCP retains source exports under `/root/litecp-export-JOB_ID` and local exports under its private `live-migrations/JOB_ID` directory.
- Each transferred archive must be at most 8 GiB. Extraction is capped at 8 GiB / 100,000 entries and requires 9 GiB free before starting. Local transfer also reserves twice the archive size plus 1 GiB.
- At most three retained native backups or credential-bearing connections. Failed credentials can be explicitly forgotten. No automatic native-backup cleanup is provided.

## Workflow

1. Verify the source fingerprint independently; for example, run `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub -E sha256` on the source.
2. Connect from Migration and select a discovered cPanel account or Plesk subscription.
3. Confirm source backup creation. LiteCP invokes cPanel `pkgacct` or Plesk `pleskbackup`, retains the original native archive, and verifies the downloaded bytes against the source SHA256.
4. Review warnings and the website/SQL preview. Choose an empty, unused LiteCP website and compatible PHP runtime. Existing files are never overwritten by the import workflow.
5. Restore the chosen SQL into the website's empty managed database; use the additional database form for every other required database. Review application configuration and update database credentials separately.
6. Verify the application using a temporary host mapping before changing DNS. Arrange a maintenance window and final data synchronization for actively written databases, files and mail. This tool does not freeze the source or provide incremental replication.
7. Explicitly forget the stored source credentials after capture; revoke the dedicated key on the source independently. Download and retain the native backup according to your own retention policy.

## Compatibility boundaries

| Data | Capture | Automatic LiteCP activation |
| --- | --- | --- |
| Source native account/subscription backup | Retained as produced by the provider's native tool | No native account restore |
| cPanel primary `public_html` | Portable files, including hidden files | Into a chosen empty site |
| Plesk selected subscription's primary document root | Portable files, including hidden files | Into a chosen empty site |
| Compatible SQL dumps | cPanel account dumps / Plesk primary-domain database dumps | Into chosen empty managed databases |
| Addon domains, subdomains, non-primary roots | Native backup only when included by provider | Separate mapping and review required |
| Mailboxes, messages, DNS, certificates, cron, users, quotas, packages, reseller settings | Native backup only when included by provider | Not activated |
| Symlinks, hard links, special files | Native backup retained; portable entries skipped with warnings | Manual compatibility review |

A native backup's contents depend on source configuration and successful provider backup completion. A checksum proves transfer integrity, not application consistency or backup completeness. Missing/unsupported portable layouts stop automatic staging rather than being reported as a successful restore. SQL requiring source administrator privileges may fail under the restricted destination user. The source stays authoritative until all required services have been independently verified.

Panel restarts interrupt captures; retained files need review. Failures retain partial files and source logs for investigation. No source cleanup or DNS cutover is performed by LiteCP. Real provider capture must be tested with a source account before production use; fixture tests are not proof of a complete live migration.

## Provider references

- [cPanel pkgacct](https://docs.cpanel.net/whm/scripts/the-pkgacct-script/124/)
- [cPanel backup tarball contents](https://docs.cpanel.net/knowledge-base/backup/backup-tarball-contents/)
- [Plesk backup CLI](https://docs.plesk.com/en-US/obsidian/cli-linux/using-command-line-utilities/pleskbackup-backing-up-content-and-configuration.74260/)
- [Plesk document-root discovery](https://support.plesk.com/hc/en-us/articles/12377935045655-How-to-restore-the-default-file-directory-permissions-located-inside-domain-s-document-root-in-Plesk-for-Linux)
