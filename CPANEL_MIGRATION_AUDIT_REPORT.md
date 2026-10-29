# LiteCP cPanel Migration Audit

## Release

- Version: 1.0.11
- Audit date: 2026-10-29
- Scope: cPanel discovery, backup capture, transfer, validation, staging, restore review, overwrite protection, progress reporting, and migration job recovery.

## Verified workflow

1. The administrator connects with root SSH key or password authentication.
2. LiteCP retrieves the SSH host key and requires explicit fingerprint trust.
3. cPanel accounts are discovered without changing the source server.
4. Selected accounts are queued and processed one at a time.
5. cPanel creates a native account backup with `pkgacct`.
6. LiteCP streams the archive over pinned SSH, enforces the size limit, and verifies SHA-256.
7. The compatible website archive is inspected with traversal, symlink, special-file, archive-size, and free-space checks.
8. Existing LiteCP domains stop for review. Overwrite requires the exact domain and creates a recovery backup first.
9. Website files and a matching supported database are restored, application database credentials are rewritten, and a local HTTP health check runs.
10. DNS is never changed automatically. Mail, DNS, SSL, package settings, and unsupported custom cron jobs remain explicit review items.

## Repairs in 1.0.11

- Added explicit persisted stages and stage timestamps for connection, selection, capture, transfer, validation, extraction, preflight, restore, and result.
- Reconciled orphaned running jobs in both single-job and job-list responses after a panel restart.
- Added terminal completion timestamps for failed, interrupted, and restored-review jobs.
- Prevented source-server filesystem paths from being returned by the migration API or displayed in the browser.
- Kept a safe boolean indicator when a source export is retained for recovery review.
- Added a nine-step visual stage tracker and clarified that numeric percentages apply only to the currently measured file transfer.
- Active transfer percentages remain capped at 99%; completion is represented by the final result stage.
- Simplified website selection and restore actions for beginner use while retaining explicit overwrite confirmation.

## Security controls confirmed

- Strict SSH host-key pinning and disabled agent/forwarding behavior.
- Passwords use an inherited pipe and encrypted server-side storage; they are not passed in command arguments or the browser state.
- Migration state and credentials use root-only permissions.
- Remote archive paths, checksums, sizes, and local extraction paths are validated.
- Archives cannot escape the staging directory through traversal, links, or special files.
- Existing domains are preserved until an explicit overwrite confirmation.
- DNS cutover is always manual.

## Automated verification

- Full Go test suite on AlmaLinux: passed.
- Production candidate build on AlmaLinux: passed.
- Browser JavaScript syntax check: passed.
- Added regression coverage for stage lifecycle, orphaned list reconciliation, and internal-path redaction.

## Known compatibility boundaries

- Fully automatic database restore currently supports recognized WordPress and WHMCS literal configuration formats with one unambiguous matching SQL dump.
- Custom application database mappings, multiple website roots, or ambiguous dumps stop for manual review rather than guessing.
- Mailboxes, DNS zones, SSL certificates, package/account limits, and arbitrary cPanel cron jobs are retained in the native backup but are not automatically activated on LiteCP.
