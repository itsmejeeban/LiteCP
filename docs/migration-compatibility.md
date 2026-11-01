# Migration compatibility repairs (4.0.3)

- Laravel connections in `.env` (DB_DATABASE/DB_NAME, DB_USERNAME/DB_USER, DB_PASSWORD, DB_HOST) map to newly created scoped local databases. Optional DB_PORT and DB_SOCKET are reset to destination defaults. APP_KEY and unrelated settings remain unchanged. Private application `.env` files are included by the cPanel exporter.
- Static PHP variable, constant and array connection configurations are supported when one unambiguous local connection is present. No source PHP or Artisan code is executed by the mapper. External hosts, database URLs and dynamic/ambiguous configurations still require explicit mapping.
- All account database exports are restored into separate new managed schemas, including additional exports. Website SQL assets are not mistaken for database dumps.
- Laravel's generated bootstrap/cache/config.php is retained in a private recovery directory outside public_html. Laravel can then load the migrated `.env` values.
- Backslash archive separators are normalized before traversal validation. Absolute, drive, UNC, escaping paths and duplicate normalized destinations remain rejected.
- Archive size inspection streams in Go, supports cancellation and reports the actual failure. Uncompressed migration data is bounded at 64 GiB and 100,000 entries; native transfer remains bounded at 8 GiB. Available disk space plus a 1 GiB reserve is checked before extraction.
- Captured/restore-review jobs expose Retry restore. Partial old staging remains retained when a fresh stage replaces it. Existing destination websites still require overwrite confirmation.

Run focused Go migration tests and scripts/test_live_source.py exporter fixtures before deployment. These source checks do not prove the supplied real accounts have passed live migration or application health checks. After deployment, retry the retained captures and check the local health result before DNS cutover. Earlier exports that omitted a private .env require a fresh source capture.
