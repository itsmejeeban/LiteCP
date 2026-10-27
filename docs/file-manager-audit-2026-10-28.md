# File Manager comparison and verification — 28 October 2026

Compared the authenticated cPanel Jupiter File Manager in Chrome with the deployed LiteCP File Manager. cPanel data was not modified. LiteCP write tests used a newly created private `tmp/litecp-fm-audit-*` folder; no existing website files were deleted.

## Repairs deployed

- Single-click folder names now select; double-click opens. This makes copy, move, rename and context-menu actions accessible without inadvertently entering a folder.
- Name, Size, Last Modified, Type and Permissions headings sort in either direction and announce their sorting state.
- Directory sizes are displayed rather than replaced by a dash.
- Preferences offers Home Directory, Web Root (`public_html`) and Last Opened Folder.
- Permission digits are editable and stay synchronized with the User/Group/World checkboxes. Existing restrictions on world-writable and special modes remain enforced.
- Searches ignore older responses and results from a website/folder that is no longer selected.
- View displays source files as inline plain text instead of allowing PHP/source MIME types to trigger downloads. Active documents remain sandboxed; HTML/SVG are shown as text.
- Website creation/reconciliation and the storage-migration tool no longer create `whmcsdata`. Existing folders and application data are retained.

## Verification

| Workflow | Evidence |
| --- | --- |
| New file and new folder | Live authenticated API creation and listing passed; reference dialogs inspected; LiteCP New File layout verified in Chrome |
| Copy, move and rename | Live content-preserving operations passed; cPanel dialog layouts compared |
| Upload | Live multipart upload and content verification passed |
| Download and View | Live content/header checks passed; source-preview regression test passed |
| Edit and HTML source editing | Live read/write round trip with Unicode passed; existing editor workflow tests passed |
| Permissions | Live mode change verified; numeric controls compared with cPanel; Chrome numeric edit saved and server independently confirmed `0640` |
| Compress and Extract | Live ZIP contents verified and extraction passed |
| Delete, Restore and permanent deletion | Passed using only newly created audit files, with explicit user approval |
| Empty Trash | Source reviewed; not run against live Trash because it could contain existing items |
| Recursive search | Live isolated-folder search and stale-response regression tests passed |
| Home, Up, Back, Forward and tree | Existing controls reviewed; Chrome verified path navigation, Back, folder selection and opening |
| Sorting | Chrome verified ascending Size sorting; unit checks cover natural and octal sorting |
| Preferences | Chrome verified layout and Home/Web Root/Last Opened choices |
| Context menu | Existing actions and selection behavior reviewed; no changes to cPanel content |

All 15 focused frontend tests and the Linux Go test suite passed. Nginx/panel services stayed active and the WordPress homepage returned HTTP 200 after deployment.

The new audit folder was removed after testing with the user's approval. Existing website files, existing Trash entries, and existing `whmcsdata` directories were retained.

## Deliberate differences and remaining parity limits

- Compression/extraction supports ZIP; cPanel additionally offers TAR, gzip TAR and bzip2 TAR. Those formats have not been implemented by this change.
- HTML Editor uses the existing source editor, not a visual WYSIWYG editor.
- File editing is UTF-8 text up to 4 MiB. Arbitrary character-encoding conversion is not implemented.
- Symbolic links and special files are excluded. Structural website folders and unsafe permissions remain protected.
- Preferences omits cPanel account-specific Public FTP Root and multi-domain account selection; LiteCP confines each file workspace to its selected website home.
- This verifies the listed workflows, not a claim of complete cPanel parity or every browser/platform combination.
