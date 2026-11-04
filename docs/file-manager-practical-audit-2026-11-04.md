# File Manager practical audit — 4 November 2026

Live version tested: 4.0.67. Fixes prepared locally: 4.0.68.
Chrome testing used Website Tools > File manager and the disposable website
qa-placeholder-renamed.litimcsellcop.com. Test data stays under qa-file-manager.
The main website was not modified.

## Observed live results

| Feature | Result |
| --- | --- |
| Website selector and Open File Manager | Passed |
| New folder and new file | Passed |
| Text editing and save | Passed; UTF-8 punctuation and checkmark preserved |
| Find/replace, word wrap, expanded editor | Passed |
| Rename including spaces | Passed |
| Copy into an existing folder | Passed; original retained |
| Move into an existing folder | Passed; destination listing verified |
| Numeric permissions | Passed; mode changed from 0640 to 0644 |
| ZIP, TAR, gzip TAR, bzip2 TAR creation | Passed |
| All four archive extraction formats | Passed; ZIP extracted file downloaded and content checked |
| Extraction over an existing file | Correctly required confirmation; confirmed replacement passed |
| Single download | Passed; downloaded contents matched saved Unicode text |
| Upload | Passed using the downloaded file in another folder |
| Duplicate upload | Correctly rejected without overwriting |
| URL upload | Passed with public https://example.com/ saved as qa-url.html |
| HTML source/visual switching and visual save | Passed; saved source preview verified |
| Source preview and context-menu View | Passed |
| Recursive search | Passed; three matching nested paths returned |
| Sorting and pagination | Passed; next page and page size exercised |
| Select all, clear selection, Ctrl multi-select | Passed |
| Direct path, Up, Back, Forward | Passed |
| Preferences | Last Opened saved and verified; original Home Directory restored |
| Full-screen toggle | Passed; returned to normal layout |
| Delete to Trash and restore | Passed; original path returned in search results |
| Permanent deletion | Passed via context menu for only qa-permanent-delete-test.txt, after user confirmation; Trash verified empty |
| Trash toolbar Delete | Bug: disabled even with a selected Trash item |
| Combined file download | No download observed in Chrome; new-tab form submission removed locally |
| Copy/Move destination guidance | Bug: asks for a file path but requires an existing folder; missing path exposes raw lstat error |

## Local corrections

- Trash toolbar switches to Delete permanently and enables it only for a selected
  Trash item. It uses the existing DELETE confirmation and resets to normal Delete
  outside Trash.
- Combined downloads submit the existing POST form through the current tab,
  avoiding a new-tab/popup dependency. Server streaming and selection validation
  remain unchanged. A post-deployment Chrome retest is still required.
- Copy/Move dialogs explicitly request an existing folder and explain filename
  preservation. Missing destinations return an actionable message without the
  physical server path. No confinement or overwrite protections were relaxed.
- Updated the stale search-test fixture to supply listing state.
- Version incremented to 4.0.68 in application and installer entry points.

## Verification

- Nine File Manager frontend tests passed.
- JavaScript syntax and git diff whitespace checks passed.
- Focused Go tests passed: TestFileTransferMissingDestinationExplainsFolderRequirement,
  TestSearchManagedFilesScopesAndHiddenEntries, TestSelectedDownloadZIPAndTraversal.
- Existing Windows CRUD/archive round-trip tests failed at file renames with
  Access is denied. They are not reported as passing; the corresponding installed
  Linux workflows were tested live as recorded above. bzip2 local tooling was not
  available, while live bzip2 archive creation/extraction passed.
- Makefile remains the build entry point. No build, deployment or release package
  was generated. Browser-generated test archives/downloads were part of the
  explicitly authorized practical File Manager tests.

## Limits and retained data

These results cover the listed workflows, not every encoding, file size, drag/drop
gesture, optional visual formatting command, browser or filesystem combination.
Empty-all-Trash was not exercised; the individually approved new test file was
permanently removed. A synthetic sandbox-local upload fixture could not be read
by Chrome; a real downloaded fixture completed the upload test instead.

Remaining test files, archives and downloaded moved.txt are retained for review.
Screenshot: ../file-manager-audit-proof.jpg
