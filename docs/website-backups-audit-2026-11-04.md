# Website Backups audit — 4 November 2026

Chrome audit used qa-placeholder-renamed.litimcsellcop.com. Corrections are local
in v4.0.74; no deployment or build was performed. Existing Makefile retained.

## Live results

- Website selection and local background backup creation passed.
- Download passed; the ZIP contained public_html.zip, databases.zip and
  home_directories.zip.
- Automatic restore passed with HTTP 200. The generated backup remained listed.
- Upload and inspect correctly reported the existing three-stage limit and left
  live website data unchanged. Existing stages were preserved.
- After exact user confirmation, deletion of only the new qSxtNgdXDNhnFIORbLTQUFEe
  audit backup and its generated intermediates passed. The list became empty.
- No remote destination was configured. Remote/both buttons were correctly
  absent; remote upload, verification and remote-only cleanup were not tested.

## Bugs fixed locally

1. Uploads were saved before checking a known-full inspection stage queue. Failed
   retries could accumulate private, unlisted ZIP files. The upload handler now
   rejects full queues before multipart ingestion or persistent archive creation.
   The inspection safety limit remains in place; existing data is not cleaned.
2. The non-public home-directory restore selector defaulted to the first website
   instead of the website currently being backed up/restored. Both selectors now
   default to the current website.

## Verification

Frontend restore-target regression and JavaScript syntax checks passed.
Focused Go tests passed for upload capacity, non-public directory inclusion,
traversal and unexpected outer-archive rejection. An additional nested-inspection
test could not complete under the local Windows free-space inspection path; it
reported insufficient free space. It is not counted as passed. The corresponding
automatic backup restore succeeded on the installed Linux server.

The live failed upload was retained privately by the old handler, as its error
stated; it was not silently deleted. Successful manual upload/inspection into a
fresh queue remains unverified because the server already has three retained
stages. New fixes need the usual update and a post-deployment retest.

Screenshot: ../website-backups-audit-20261104.jpg
