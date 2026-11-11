# Restore manual website ZIP backups

Open the website backup popup. Use Upload and inspect to upload a downloaded LiteCP `website.zip`, or Restore beside a retained backup. The supported format is exactly two nested archives: `public_html.zip` with files under `public_html/`, and `databases.zip` with SQL dumps (or the generated no-database README).

The popup shows an inspected summary, an empty target website selector, and separate source-SQL/empty-managed-database selectors. Restore files and each required database separately after confirmation. Target websites must be empty: move existing files or placeholders to Trash yourself if appropriate. Target databases must be empty. Existing live files/tables are rejected rather than overwritten. A running Node.js/Python/Ruby target must be disabled before file restoration.

The database password requested is the current password of the selected target managed database. A failed SQL import resets only that previously empty target through the existing scoped importer. The source archive cannot choose an arbitrary target path or MySQL root privileges. Dumps needing unsupported administrator privileges may fail. Different database names/users require updating restored application configuration. The popup does not automatically rewrite WordPress/WHMCS credentials or import server/mail/DNS settings.

Uploads are limited to 2 GiB. Nested archives are each limited to 2 GiB; combined extraction is limited to 8 GiB and 100,000 entries. Available disk space is checked before staging. Absolute/traversal paths, duplicate paths, links/special files and unexpected archive layout are rejected. Only restore trusted backups: restored application code can execute when visited.

Uploaded and server-saved archives are retained independently of inspection stages. The stages use the existing Migration storage and a maximum of three valid stages; review/delete them explicitly under Migration when finished. Deleting an original backup does not delete an already inspected restore stage. Failed uploads/staging may leave root-only partial data for owner-led review; there is no automatic destructive cleanup.

Files and SQL are restored independently. A successful file restore does not prove that all databases or application dependencies were restored. Verify the application, permissions, configuration, database contents and backups before production use. The existing website and SQL import safeguards are reused; no production restoration is executed merely by uploading or inspecting.

Validation: nested ZIP fixtures, hidden files, SQL discovery, traversal/symlink/layout rejection, unauthenticated endpoints, an empty temporary website restore, and rejection of a subsequent restore into that now-nonempty fixture. Live production SQL restoration is not exercised during deployment.

## Restore via Folder database selection

In Migration > Restore via Folder, inspect an archive and choose the target website. For database restoration, select the source SQL dump, then select a **Target database** linked to that website and enter that database's current password. Every linked database is available, including secondary databases. Changing the website or SQL dump clears the previous database choice and password.

The target website and selected database must be empty. Files only does not import or modify a database. Create and link a managed database first if the target has none. After import, update the restored application's database name, username and password as needed.

API clients can send `databaseID` in `POST /api/migrations/import`. The server rejects unknown or foreign database IDs before staging files. For older clients that omit the ID, a single linked database remains supported; a website with multiple linked databases requires an explicit ID. Existing empty-target and restricted SQL-user safeguards still apply.
