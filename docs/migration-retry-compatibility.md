# Migration retry compatibility (4.0.40)

Retry pending now includes cPanel jobs whose website was committed but whose
HTTP health check failed (`restored-review`). This requires the original
website ID to still match. It resumes service import and health checks without
replacing website files or importing SQL again.

Literal references to the captured cPanel home are relocated to the destination
website home in PHP strings and INI/environment values. Original changed files
are retained outside the web root in the job's `.path-recovery-*` folder. This
does not evaluate PHP or relax filesystem confinement.

Database inspection supports literal local MySQL PDO DSNs. External servers,
ambiguous connections and unsupported options still stop for review. File-based
sites with no detected database dependency may retain unrelated SQL dumps;
missing literal PHP includes and unknown frameworks still require review.

Archive file/directory collisions now identify the affected entry. They are
not silently discarded. A source application with a broken bootstrap path or
an HTTP 500 response remains pending until the underlying problem is corrected.

Verified locally with targeted migration tests and JavaScript syntax checks.
These changes require deployment through `update.bat` before live retries use
the new behavior. They have not yet been verified by a live restore.

## cPanel mail format metadata (4.0.42)

cPanel's `mail/mailbox_format.cpanel` marker is validated as account metadata,
so an account with empty mail directories can restore without creating a
mailbox. Named mailboxes and cron jobs are still discovered normally. The
marker must be a regular file declaring Maildir; symbolic links, unsupported
mail formats and unmatched mailbox data still stop for review.

Pending jobs stopped by this marker can use **Retry pending** after the update;
they do not need a new source capture while their staged files are retained.

## Re-capture after a deleted website (4.0.43)

Jobs stopped at final website checks also offer **Retry capture** while source
credentials remain available. This starts a fresh capture rather than trying
to run health checks against a deleted website's ID. Existing destination
websites still require overwrite confirmation, and existing mail is preserved.
