# Cron Jobs audit — 4 November 2026

Live Chrome version: 4.0.77. Local fixes: 4.0.78, not deployed.
Disposable website: qa-placeholder-renamed.litimcsellcop.com.

## Live checks

- Created a disabled custom job, QA cron audit.
- Successful manual execution returned the expected printf output.
- Invalid minute 99 was rejected without changing the saved schedule.
- Edited command and custom timing persisted as 7 3 * * *.
- Enable and Disable changed the saved job state correctly.
- A printf followed by false correctly reported a failed test with its output.
- Every-minute scheduling was exercised with a harmless touch marker. The first
  marker path used the root-owned home directory and correctly failed with
  Permission denied; no permissions were weakened. The scheduled test was moved
  into the website-owned tmp directory.
- Scheduler journal confirmed the job was launched by crond with the isolated
  website UID and GID. The tmp/qa-cron-scheduled-proof.txt marker appeared after
  the next minute tick, proving actual scheduled execution.
- After exact user confirmation, disabled and removed only QA cron audit. The
  list correctly returned to No custom cron jobs yet. Marker file retained.

## Confirmed UI bugs fixed

1. Old test output/success remained visible after editing or saving a command,
   and while a new test was starting. Clear output and hide the result when
   editing, after a successful mutation, and before each new test request. A
   failed request can no longer leave a previous successful result visible.
2. Editing a job retained the Add a cron job heading. It now says Edit cron job,
   and returns to Add a cron job on cancel/reset.

## Verification

All 11 frontend cron tests passed. Focused Go tests passed for schedule syntax,
privilege dropping, unsafe identities/paths, cross-site metadata, authentication,
origin enforcement, bounded test output and isolated test execution arguments.
JavaScript syntax and diff checks passed. Existing Makefile retained. No build,
deployment or distributable package generated.

Default application cron was covered by an existing automated test, not toggled
on the production-like main website. No claim is made about every possible user
command or external mail/network action. Test commands were limited to printf,
false and a marker in the disposable website.

Screenshot: ../cron-scheduler-proof-20261104.jpg
