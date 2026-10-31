# Backups > Restore

Restore lists complete local website ZIP backups and recorded backup-history
archives. It verifies local regular files and checks exact recorded objects on
attached remote destinations. A failed remote check is shown as a warning;
unavailable objects are not presented as available. Files placed manually in
remote storage without a LiteCP history record are not discovered yet.

Restore asks for replacement confirmation once, starts a server-side job and
shows stage progress. The general create/upload backup dialog is not opened.
The job continues after the browser disconnects, provided LiteCP stays running.
The existing recovery-backup and staged website restore engine is reused.
Previous public files and replaced home directories are retained for recovery.
No DNS changes are made.

WordPress and WHMCS database configuration can be mapped automatically. Custom
applications with database dumps stop for review rather than guessing credentials.
A website must exist before its backup can be restored. Panel restarts during a
restore require operator review; this is not a restart-resumable job system.

Verify application pages and database connectivity after restoration. A local
HTTP health check is included, but it cannot prove every application feature.


Website removal retains backup history and remote objects. Before deleting a managed website home, LiteCP moves its backup directory to `/var/backups/litecp/retained-websites/<site-id>/backups` and saves site metadata beside it. If preservation cannot finish (including a cross-filesystem rename or destination conflict), removal stops; it does not discard the backups. Orphaned website backup history is excluded from age-based pruning. Existing backups already deleted by an older version cannot be recovered by this change.

Restore accepts a retained or recorded backup even when its original website is absent. After confirmation, the normal restore worker provisions a missing domain and restores supported files and application data. Existing domains continue to require the replacement confirmation and recovery backup. Unknown custom database layouts still require an explicit adapter or manual mapping; the panel does not guess credentials. Remote copies are removed only by a separate explicit backup-removal action.
