# LiteCP UI and functional audit — 28 October 2026

## Summary

The panel uses Go embedded assets with plain JavaScript and CSS. No frontend framework or new dependencies were introduced. The existing dark theme and hosting workflows are retained.

| Measure | Result |
| --- | --- |
| Routes discovered and inspected | 44, including the File Manager workspace |
| Responsive observations | 352: 44 routes at 320, 375, 425, 768, 1024, 1280, 1440 and 1920 pixels |
| Confirmed issues in this follow-up | 11 |
| Fixed and verified | 11 |
| Known unresolved defects among those 11 | 0 |
| Frontend automated regression tests | 78 passed, 0 failed |
| Backend regression suite | Go package tests passed; go vet passed |
| JavaScript syntax checks | 33 source files passed |
| Final layout findings | No page-level horizontal overflow or unreachable clipped controls detected |

These are bounded audit results, not a guarantee that every hosting action is bug-free. Layout observations, unit tests and server-side tests are separate evidence. Destructive or credential-dependent workflows were not executed against existing hosting data.

## Confirmed corrections

All rows below are **FIXED AND VERIFIED** through browser observations, focused regression tests or both.

| # | Category / affected files | Root cause and correction | Verification |
| --- | --- | --- | --- |
| 1 | Overflow: `web/website-ssl.js`, `web/litecp-theme.css` | SSL table forced the phone viewport wider. Added a bounded, keyboard-focusable table scroll region. | All eight widths; table remains reachable without page overflow. |
| 2 | Overflow: `web/security-pages.js`, `web/litecp-theme.css` | WAF table inherited unrestricted table width. Contained scrolling in a labelled region. | All eight widths. |
| 3 | Overflow: `web/litecp-theme.css` | Loaded PHP extension catalogue exceeded mobile width. Bounded its own scrolling area. | Catalogue loaded and checked at all eight widths. |
| 4 | Alignment: `web/litecp-theme.css` | Shared panel headings retained intrinsic width and pushed search/actions outside narrow cards. Allowed shrinking and wrapping. | Full route matrix. |
| 5 | Clipped controls: `web/file-manager.css` | File Manager header breakpoint was too narrow; Settings/full-screen controls were clipped at tablet widths. Wrapped the header earlier and allowed search inputs to shrink. | 1024-pixel recheck plus full matrix. |
| 6 | Navigation: `web/panel-settings.js`, `web/sidebar-rail.css` | Mobile sidebar had no reachable opening control after desktop menu removal. Added a mobile-only labelled navigation button and corrected scrim visibility. | Opened sidebar at 375 pixels and navigated to Databases. |
| 7 | Accessibility: `web/file-manager.js` | Full-screen icon lacked an accessible name. Added an explicit label. | Browser control inspection. |
| 8 | Functional: `web/overview-environment.js` | Environment inventory requested protected data before authenticated state existed, producing an initial 401. Wait for panel state. | Final browser run has no failed API responses. |
| 9 | Encoding: `web/overview-environment.js`, `scripts/text-encoding.test.cjs` | An invalid UTF-8 byte corrupted a placeholder. Replaced it and made the encoding regression check reject malformed UTF-8. | Encoding test and syntax checks pass. |
| 10 | Functional: `web/website-ftp.js` | Successful creation closed the dialog before cleanup, leaving its busy state set. Cleanup now matches request/site identity independently of dialog visibility while preserving stale-request protection. | Existing popup regression tests pass, including subsequent operations. No real FTP account was created. |
| 11 | Dark-theme CSS: `web/file-manager.css` | Legacy light File Manager rules overrode panel theme, including alternating white rows and search-result buttons. Added scoped dark surfaces/text for workspace, context menu, search results and action dialogs. | Screenshot review and dialog geometry at phone, tablet and desktop widths; search-result text contrast visually rechecked. |

`web/index.html` has new asset versions so existing browsers fetch the corrected files. `scripts/panel-search.test.cjs` was aligned with the already implemented submenu destinations and current result-panel width; this was a stale test expectation, not a search behavior change.

The preceding deployed audit commit `848d50d` also fixed the server's File Manager path-opening failure when the kernel rejects openat2, with a confined fallback and backend tests. It corrected email/tool layout spacing. Those earlier changes are not counted again in the 11 follow-up issues above.

## Coverage

- Overview; Websites; File Manager picker and workspace; Databases; Website Backups; Cron Jobs; FTP; SSL.
- Email: Add Domain, Create Mailbox, Create Alias, Webmail, Domains, Mailboxes, Aliases.
- Security: Dashboard, Firewall, Malware Scan, WAF.
- Server Environment: Application Hosting and PHP Extensions; Services; Logs.
- Backups: Create, Add Destination, Settings, Destinations, History.
- Migration: cPanel, Plesk, LiteCP, Upload/Restore.
- Analytics, Recent Activities, Documentation.
- Settings: Password, Email, Time Zone, Hostname, DNS, Developer Mode, Update, Reboot.

Five primary dialogs were opened separately: website creation, database creation, cron, FTP and website backups. File Manager New File, New Folder, Upload and Preferences dialogs were checked at eight widths. Copy, Move, Rename, Permissions and Compress dialogs were opened without submitting changes. Right-click Edit was confirmed on a text file. This verifies availability and presentation, not successful server mutation.

## Evidence and reproducibility

`scripts/ui-browser-audit.cjs` launches isolated headless Chrome using Playwright, with credentials supplied only by environment variables. It blocks hosting API mutations, permits login/logout, waits for requests and finite animations, visits sidebar routes, records geometry/errors, and saves screenshots. Local-asset overrides allow checking candidate CSS/JS against real test-server data before deployment.

Local evidence is under `docs/ui-audit-2026-10-28/`: `final/results.json` contains the 352-route matrix; `dialogs-final/results.json` contains extended dialog/navigation checks; `dark-final/` contains the final dark File Manager screenshots. Evidence screenshots are local artifacts, not bundled in a ZIP. The runner requires an existing Playwright installation and Chrome; it does not install dependencies.

The `bc77ed5` build was deployed with the previous binary and configuration retained under `/var/backups/litecp/browser-audit-20261028-bc77ed5`. Only LiteCP restarted. The version endpoint reported the correct build, the service was active, and the hosted website returned HTTP 200. `deployed/results.json` repeats all 352 observations against delivered assets, with no JavaScript errors, failed API responses, page overflow, clipped controls or unnamed visible buttons. The subsequent search-dialog-only correction was candidate-tested separately in `search-verified/` before its deployment.

The final code build `fd13952` includes the search-dialog correction. Its deployment also retained a binary/configuration backup, the service reported active, and the website returned HTTP 200. Final delivered-asset browser evidence is in `deployed-final/`.

## Remaining verification limits

| Status | Area | Reason |
| --- | --- | --- |
| REQUIRES MANUAL REVIEW | Real create/edit/delete operations for websites, mail, databases and FTP | Browser audit intentionally prevented hosting mutations. Unit/backend tests are not live end-to-end proof. |
| REQUIRES MANUAL REVIEW | Backup generation, download content and restore | Restore can overwrite data; no existing files or databases were deleted or restored. |
| BLOCKED | Live cPanel/Plesk/LiteCP migration | No authorized live source credentials and migration fixture were supplied for this audit. |
| REQUIRES MANUAL REVIEW | SSL issuance, firewall changes, runtime installation, service restarts, reboot | Operational changes were outside the read-only browser audit. |
| REQUIRES MANUAL REVIEW | External webmail/phpMyAdmin and delivery | External authentication, DNS and mail delivery require separate end-to-end checks. |
| REQUIRES MANUAL REVIEW | Assistive technology and physical devices | Label/keyboard and viewport checks were performed; screen-reader and actual mobile-device testing were not. |

No claim is made that missing WAF/antivirus software is installed. Existing capability indicators remain truthful. Existing `whmcsdata` directories were not deleted. Authentication and server security configuration were not weakened.
