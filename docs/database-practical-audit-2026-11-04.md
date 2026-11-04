# Database audit — 4 November 2026

Live Chrome testing: v4.0.71. Local corrections: v4.0.72, not deployed.

## Confirmed bugs corrected

- New databases displayed Unavailable for size until navigating away and back.
  Creation now refreshes database sizes after displaying one-time credentials.
  A refresh requested during an older query queues one fresh query so newly
  created databases are not missed.
- Deleting any website-linked database cleared the website's primary database
  and username, including when the deleted database was only an additional one.
  Detachment now occurs only when the deleted name matches the primary database.
  Failed deletions restore the exact previous primary reference.

## Practical results

| Feature | Observed result |
| --- | --- |
| Database/username/password generators | Passed |
| Standalone creation | Passed: qa_db_audit_20261104 / qa_db_audit_1104 |
| Copy name and username | Passed; clipboard values matched |
| phpMyAdmin sign-on | Passed for standalone and website-linked databases |
| SQL read/write | Passed; qa_audit table and Unicode marker verified |
| Size collection on page entry | Passed; empty database 0 B, populated database 16 KB |
| Website action/filter/empty state | Passed for qa-placeholder-renamed.litimcsellcop.com |
| Website-linked creation | Passed: qa_site_db_audit_1104 / qa_site_db_1104 |
| Duplicate creation | Rejected; existing database not replaced |
| Standalone deletion | Passed after exact user confirmation; only standalone audit database/user and its test table removed |
| Password reset dialog | Open/cancel passed; password mutation not performed because computer-use policy requires user handoff |

The website-linked audit database remains for review. Its original one-time
credential dialog was left open after automatic approval review rejected
dismissal without saved-password evidence. No credentials are recorded here.
The main website's databases were untouched.

## Verification

- Two frontend workflow tests passed, covering immediate refresh and an in-flight
  size-query race.
- Focused Go tests passed: primary/additional deletion linkage, size endpoint
  authentication, ownership and size deduplication.
- Windows tests initially hit temporary-directory save restrictions; rerunning
  with a workspace-local temporary directory passed.
- JavaScript syntax and diff whitespace checks passed.
- Existing Makefile remains the build entry point. No build, deployment or
  distributable package was generated.
- Browser/runtime validation of v4.0.72 remains pending the user's usual update.

Screenshot: ../database-audit-proof-20261104.jpg
