# Server File Explorer

The Management sidebar entry opens the existing File Manager workspace at `/`.
It reuses folder trees, the path bar, back/forward navigation, search, sorting,
hidden-file preferences, details, previews, downloads and full-screen mode.

This explorer is read-only. Website File Manager keeps its existing editing
controls. The server scope is resolved only for authenticated GET requests and
is never registered as a hosted website. Existing write endpoints require an
actual website record, so the explorer ID cannot authorize server file writes.

Files are accessible according to the LiteCP service account's permissions.
Symlinks, devices, sockets and pipes are not followed/opened. Kernel and runtime
paths `/proc`, `/sys`, `/dev`, `/run` are excluded; persistent server paths such
as `/etc`, `/var`, `/root`, `/home` and `/usr` can be browsed when permitted.

After running update.bat, verify the sidebar entry, root listing, path entry,
directory navigation, text preview, downloads, and return to a website's File
Manager. Verify that write controls are hidden in the server explorer and that
website editing controls remain available.
