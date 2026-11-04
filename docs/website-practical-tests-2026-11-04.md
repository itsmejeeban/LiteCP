# Website practical tests — 4 November 2026

Live version tested: 4.0.65. Local fixes: 4.0.66, not deployed.
Disposable website: qa.litimcsellcop.com, renamed to qa-renamed.litimcsellcop.com.

| Action | Observed result |
| --- | --- |
| Create website with managed database | Passed |
| Change PHP 8.4 to 8.3 | Passed |
| Rename domain | Passed; website directory and routing changed |
| Individual and bulk suspend/unsuspend | Passed |
| File creation, Trash and backup restoration | Passed; marker file recovered |
| Cron creation and Run test | Passed with /usr/bin/true |
| PHP.ini memory setting | Saved successfully |
| DNS draft | Saved; authoritative DNS service is not installed |
| SSL issuance and renewal | Passed with supplied contact email |
| Bulk SSL on an existing certificate | Correctly skipped |
| Temporary FTP account | Created and listed, restricted to test website home |
| phpMyAdmin sign-on and SQL | Passed; created qa_verification and test marker |
| Local backup and database restore | Passed; original marker recovered after modification |
| Website removal | Passed; managed website removed and local backups retained |
| Restore after removal | Website and database recreated; job sB5LB4c1Qdntx_1J5oXaMxWe reports restored, HTTP 200 |
| Database after recreation | Original backup-proof-20261104 marker verified in phpMyAdmin |

## Fixes prepared locally

- Restore previously moved active manual/scheduled repositories into recovery storage, hiding them from normal backup listings. Preserve verified copies in the staged home before the transactional swap. Originals remain available for rollback. Reject symlink repositories and reserved archive conflicts.
- New default placeholder pages use public_html rather than an absolute home path, preventing stale upload guidance after domain renaming. Existing user index files are not rewritten.
- Version incremented to 4.0.66.

## Verification and limits

Focused backup repository and SQL preamble tests passed during implementation. A later rerun could not start because Go was absent from the current shell PATH. git diff --check passed. Makefile remains the build entry point. No build, deployment or distributable packaging was performed.

FTP account creation does not prove network FTPS login/upload. Authoritative DNS publishing was not tested because its service is absent. Existing credential reset actions were not performed. Remote-only backup transfers and every optional application runtime are outside the completed practical checks. The recreated test website remains available for inspection; its prior FTP account was removed with the website. SSL must be explicitly reinstalled after recreation.

Screenshot: ../website-restore-test-proof.jpg
