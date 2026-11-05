# Resource enforcement live audit - 2026-11-05

Candidate: 4.0.132. Installed panel was not replaced. Tests used only the disposable
qa-backup-audit.litimcsellcop.com website on the authorized test server, with SELinux
enforcing and ext4 project quotas. Test binaries and generated audit data were retained.

Verified:

- CPU at 25% was kernel-throttled and blocked after approximately 12.5 seconds of
  the bounded request (ten consecutive monitor seconds at the allowance).
- Memory exhaustion produced website-local OOM events and HTTP 503; recovery returned 200.
- Real five-minute automatic recovery returned the website to 200 at 301 seconds.
- Managed cron entered the same website slice.
- Process cap rejected additional forks with EAGAIN; the website remained active.
- Disk writes were rejected with EDQUOT; disk exhaustion blocked the website and
  retry did not reopen it while over the recovery threshold.
- Manual suspension remained effective across resource recovery requests.
- The comparison website returned 200 during isolation tests. Panel, Nginx and
  MySQL remained active. Disabling restored the audit site's original runtime.

Focused Go resource regression tests and nine JavaScript/theme/encoding tests passed.
The Linux candidate compiled successfully. The existing Makefile was retained.
Chrome access was disabled in Work settings, so the new controls were not visually
verified in Chrome. No release archive was created and the panel was not deployed.

This is native PHP/static coverage, not complete CloudLinux equivalence. Application
containers, PHP 7.3, XFS activation and optional I/O rates remain unsupported and are
explicitly rejected. Reboot, backup restoration, every PHP version and all lifecycle
combinations have not been certified. Audit temporary files remain on the test server.
