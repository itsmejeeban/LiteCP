# File integrity monitoring

Security > File Integrity Monitoring (also linked from Security Dashboard) records
SHA-256 hashes, permissions and symlink targets for /etc/nginx, /etc/litecp,
/etc/cron.d, /etc/systemd/system and /usr/local/bin/litecp. File contents and
credentials are never included in the report. Symlinks are not followed.

Start by checking that the server is trusted, then choose Create trusted baseline.
This enables hourly monitoring. Administrators can choose every 15 minutes or
daily, disable scheduling, or run Check now. The broker checks schedules every
minute, independently of the browser; persisted settings survive panel restarts.
Only one check runs at a time. The latest report lists added, modified and missing
paths. Scan errors do not replace the baseline or claim a clean result.

Legitimate updates can change monitored files. Review those changes before
approving current files as a new baseline. Approval retains previous baselines.
Baselines and reports are private files beneath /var/lib/litecp/integrity and are
retained without automatic deletion. Include this directory in off-server backup
policy. No monitored file is deleted, quarantined or restored by this feature.

Checks are bounded to two minutes, 50,000 entries, 512 MiB total and 128 MiB per
file. Oversized, unreadable, changing or special files produce an incomplete-check
error rather than being silently skipped. Missing optional roots are permitted;
files previously present there are reported missing. Website content, databases,
logs, ownership, extended attributes and ACLs are outside this initial scope.

This is local change detection, not a malware verdict. A compromised root account
can alter both monitored files and local baselines. Independent remote audit
storage is needed for stronger tamper resistance. No automatic trusted baseline
is made during installation or update.
