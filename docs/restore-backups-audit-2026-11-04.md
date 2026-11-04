# Restore Backups audit — 4 November 2026

Live Chrome version: 4.0.76. Corrections prepared locally: 4.0.77.

## Live checks

- Inventory listed seven available local archives, storage, creation dates and
  sizes. Existing and missing website labels were shown correctly.
- Cancelled restore confirmation without starting the restore.
- Restored the disposable qa-placeholder-renamed.litimcsellcop.com from its
  6kvjb1GPpInnisbjFEzw8XPU manual ZIP. Progress completed with HTTP 200.
- Files/databases were restored with recovery data retained. No existing backup
  was deleted. Remote restore was not exercised; no remote destination configured.

## Corrections

1. A restored-review result (including failed HTTP health checks) was presented
   as Restore finished. It now explicitly reports restored data needing review,
   with an attention toast rather than a success toast.
2. Restore clicks while overwrite confirmation was pending could open multiple
   confirmations and proceed toward duplicate requests. A confirmation guard
   now allows one confirmation/restore flow at a time.
3. Completing a restore refreshed global state but left the catalog stale.
   The catalog now reloads on the Restore page, including website-existence labels.

## Verification

Six frontend restore tests passed, covering cancellation, job polling, missing
website submission, health-review reporting, concurrent confirmation and catalog
refresh. JavaScript syntax and diff checks passed. Focused Go tests for job
directory initialization, invalid IDs and unsafe archive members passed.

The unhealthy-website case was simulated in tests; the live website was not
deliberately broken. Missing-website recreation was tested earlier, not repeated
by deleting another website during this audit. Existing Makefile retained; no
build, deployment or distributable package generated. Update/retest required for
the local fixes.

Screenshot: ../restore-backups-audit-20261104.jpg
