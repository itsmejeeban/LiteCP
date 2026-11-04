# Live cPanel / Plesk migration

The Migration page supports root SSH account discovery, native backup capture, SHA-256 checked transfer, and review of compatible website files and SQL. A captured backup is not a completed account migration.

## Requirements

- Linux cPanel/WHM or Plesk source, root SSH, Python 3, and working native panel backup tools.
- A dedicated unencrypted SSH private key OR a root password, plus an independently verified SHA256 host-key fingerprint. Password login requires sshpass on LiteCP and root password authentication enabled on the source. Interactive MFA and Plesk for Windows are unsupported. Passwords are encrypted with the LiteCP secret key and sent to sshpass through a private inherited pipe, never command arguments or environment variables.
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
| cPanel named mailboxes, password hashes, quotas and Maildir messages | Portable export plus native backup | Additive import; existing mailbox/forwarder conflicts stop import |
| cPanel account cron jobs | Portable export plus native backup | Compatible schedules run as the destination website user; unsupported syntax is imported disabled |
| Plesk mail/cron, DNS, certificates, forwarders, users, packages, reseller settings | Native backup only when included by provider | Separate review/configuration required |
| Symlinks, hard links, special files | Native backup retained; portable entries skipped with warnings | Manual compatibility review |

A native backup's contents depend on source configuration and successful provider backup completion. A checksum proves transfer integrity, not application consistency or backup completeness. Missing/unsupported portable layouts stop automatic staging rather than being reported as a successful restore. SQL requiring source administrator privileges may fail under the restricted destination user. The source stays authoritative until all required services have been independently verified.

Capture workers run independently of the panel. Failures retain partial files and source logs for investigation. After a cPanel transfer is checksum-verified, LiteCP removes only the two generated source archives; DNS cutover remains manual. Real provider capture must be tested with a source account before production use; fixture tests are not proof of a complete live migration.

## Cancellation and cPanel service import

**Cancel migration** and **Cancel all** include queued/running jobs and jobs awaiting restore, compatibility review or overwrite approval (`review`, `restore-review`, `captured`, `awaiting-overwrite`, `interrupted`). Cancellation is recorded immediately, disables automatic restore and survives late worker writes and panel restarts. Running workers are signalled to stop. The short final filesystem commit cannot safely be interrupted; completed restores are not relabelled as cancelled. Cancellation preserves captures, credentials and website data; temporary-file cleanup is a separate confirmed action.

Captures created before the mailbox/cron export was introduced must be captured again. **Retry capture** queues a fresh capture of the previously selected website using retained credentials, followed by automatic restore with the existing overwrite protections. Repeated retry requests reuse an already queued or running migration for that source website, or its verified backup waiting for overwrite confirmation. The interface opens that attempt and shows **Review overwrite** without expanding its details. Earlier failed/interrupted attempts with a newer migration on the same source website appear in completed history with a link to the newer attempt. Previous capture files and job history are retained; **Retry restore** is not offered against missing artifacts.

cPanel mailbox import currently accepts named domain mailboxes with SHA512-CRYPT passwords. Locked/unsupported password hashes, orphan mail data, disabled destination email hosting and existing mailbox/forwarder conflicts produce an actionable error. The source account's default system mailbox is not activated. Existing mail data is never replaced. A retry after the website commit resumes mailbox/cron import without replacing website files, SQL or messages delivered since the import.

Cron metadata is parsed without executing source commands. Standard numeric schedules and common hourly/daily/weekly/monthly/yearly macros are supported. Source home/PHP executable paths are translated to the destination. Reboot macros, unsupported shell/time-zone environment settings and percent-input commands are saved disabled for administrator review. Commands use the existing per-website UID/GID execution boundary. DNS, SSL, mail delivery and application health still need verification before cutover.

## Temporary file cleanup

After stopping migrations, use **Clean migration temporary files** and confirm the deletion scope. Cleanup removes native/portable captures and extracted staging for stopped jobs, failed restaging copies, and independently uploaded stages with valid migration manifests. Active migrations, restored website data, recovery backups, credentials and job history are preserved. Unrecognized directories and failures are reported rather than silently deleted. Refresh Disk Usages after cleanup to measure the remaining files.

The cleanup POST is serialized by the authentication middleware. The handler must not acquire the same mutation lock again; doing so prevents cleanup from running.

## Provider references

- [cPanel pkgacct](https://docs.cpanel.net/whm/scripts/the-pkgacct-script/124/)
- [cPanel backup tarball contents](https://docs.cpanel.net/knowledge-base/backup/backup-tarball-contents/)
- [Plesk backup CLI](https://docs.plesk.com/en-US/obsidian/cli-linux/using-command-line-utilities/pleskbackup-backing-up-content-and-configuration.74260/)
- [Plesk document-root discovery](https://support.plesk.com/hc/en-us/articles/12377935045655-How-to-restore-the-default-file-directory-permissions-located-inside-domain-s-document-root-in-Plesk-for-Linux)
