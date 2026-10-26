# Advanced file manager

The file workspace uses LiteCP's existing website-confined APIs. It adds a lazy-loaded folder sidebar, address bar and breadcrumbs, Back/Forward/Up navigation, compact file table, grid view, current-folder search, hidden-file filtering, sorting, selection summary, and file details.

Actions include new files/folders, drag-and-drop uploads with transfer progress, editing through the existing Ace editor, downloads, batch copy/move, rename, ZIP compression/extraction, permissions, and recoverable Trash. Right-click an item or use its action menu. File operations use styled dialogs; permanent Trash deletion additionally requires typed confirmation.

- Click a folder to open it; double-click a file to edit it.
- Use checkboxes, Ctrl/Cmd-click, or Shift-click for selection. Ctrl/Cmd+A selects visible items when focus is in the workspace.
- F2 opens Rename. Delete opens a confirmation dialog; it does not immediately remove anything.
- Destination paths are relative to `/public`; traversal outside the website is rejected by both the interface and server.
- Upload up to four files, each at most 512 MiB. Multi-file uploads reject existing names; a single-file upload can replace its existing file after the user submits the upload dialog.
- Browser editing remains limited to UTF-8 text files up to 4 MiB. Extraction supports ZIP; the Archive type label does not imply TAR extraction support.
- Search filters the current folder; it does not recursively scan the server. Folder-tree children are fetched only when requested.
- Copy/move destinations must already exist. Existing destination names are not overwritten. Permissions apply to one selected item; recursive permission changes are intentionally unavailable.
- List/grid views and dialogs support the panel's light and dark themes. On narrower screens, the details panel/folder sidebar collapse and the toolbar scrolls horizontally.

Validation: run `node --test scripts/file-manager.test.cjs` and JavaScript syntax checks. Live Chrome checks covered real WordPress folder navigation, history, filtering, selection, details, list/grid switching, contextual actions, dialogs, and the empty Trash view. File-operation backend regressions passed using isolated test fixtures. No live files were renamed, moved, overwritten, archived, or deleted during browser verification; real upload progress and destructive submissions were not exercised against the WordPress site.
