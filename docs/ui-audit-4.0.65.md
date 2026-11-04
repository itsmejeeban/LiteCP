# UI audit - 4 November 2026

Live Chrome target: LiteCP v4.0.63. Changes prepared locally as v4.0.65.

## Coverage

Opened all 69 sidebar hrefs at desktop width (1920px), phone width (390px)
and tablet width (768px). These are navigation entries rather than 69 unique
screens: the legacy WAF link redirects and some entries share a destination.
Also opened Documentation from the header at desktop width.

Groups covered: Overview, Websites, all Website Tools, all Email sections,
Security, Server Environment, Services, all Backup sections, Logs, Terminal,
Server File Explorer, all Migration sections, Analytics, System Update,
Recent Activities and all Settings sections.

Checked route headings, page-level horizontal overflow, visible broken image
assets, replacement characters and unlabeled empty buttons. Inspected selected
desktop/mobile screenshots of tables, forms, Services, Resource Limits,
System Snapshot and Website Traffic Analytics. No page-level horizontal
overflow was detected in the navigation pass. Wide data tables intentionally
scroll within their containers.

Email pages are gated by unconfigured webmail on this server. Their setup
notice and navigation were inspected; the hidden configured mailbox UI and
mail delivery workflows were not fully verified. Merely opening a route does
not verify every asynchronous report, dataset, modal or server operation.

## Corrections

- Resource Limits now uses the common padded panel header and retains its
  own pager without a second pager from the generic list component.
- Mobile navigation footer hides while the off-canvas sidebar is closed,
  avoiding the orphan toggle floating over page content.
- Informational webmail setup confirmation closes when navigation changes.
  Destructive dialogs do not opt into this behavior.
- Server File Explorer now has the correct page heading instead of the
  generic website File manager title.
- Website Traffic Analytics gives space to the request search field instead
  of allowing its field selector to take nearly the whole row.
- Fail2ban and ClamAV update services now have meaningful identifiers and
  descriptions instead of the generic SV fallback.
- Preserved the pending v4.0.64 Appearance icon and encoding correction.

## Verification and limits

25 focused dialog, Email navigation, pagination, appearance and Overview tests
passed. JavaScript syntax checks and git diff whitespace validation passed.
Existing Makefile retained. No build, package, publication or deployment was
performed. Local corrections have not been visually verified in the deployed
panel. No website, database, backup, service or authentication setting was
changed to exercise destructive workflows.

Browser viewport override was reset after the responsive checks.
