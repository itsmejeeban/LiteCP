# UI/UX audit - 2026-11-05

Local revision: 4.0.129. Installed panel checked: 4.0.128.

## Changes

- Shared button foreground/background colors follow the selected mode, fixing unreadable secondary buttons and profile captions in light mode.
- The light-mode logo has a contrasting backing so the original white wordmark remains visible.
- The sun/moon SVG rule has sufficient specificity to keep the theme control visible.
- Destructive controls using `button-danger` retain a red surface and white text.
- Malware scan metrics, paths and log disclosures follow theme colors.
- Overview status values, running-job notes, usage graph labels, and Traffic Analytics labels/inputs/table headers have theme-aware contrast.
- Wide Firewall tables scroll inside their containers instead of widening the mobile page.

## Verification

- Chrome read-only route audit: all 70 distinct sidebar routes in light and dark mode; no broken visible image assets or desktop page-width overflow observed.
- Mobile route audit at 390 x 844 found Firewall overflow; fixed locally.
- Chrome local static component preview uses the production CSS in its normal stylesheet order. Checked both modes at 390 x 844, 768 x 1024 and 1280 x 900. No page-width overflow or broken images. Sampled active button labels, profile caption, scan totals, chart labels and environment values have a minimum settled contrast ratio of 4.83:1.
- Theme-toggle and UTF-8 text checks: 4 tests passed.
- `git diff --check` passed.
- Existing Makefile retained. No build, package, push or deployment performed.
- Original live light-mode preference and browser viewport restored.

## Scope limits

The 70-route sweep checked rendered layout and image loading; it is not proof of every modal, hover state, data-dependent condition or functional action. The updated styles were verified in a local component preview, not deployed over the live panel. Recheck live pages after update.bat deploys 4.0.129.

Local screenshots and measurement records are retained under `.staging/ui-*-4.0.129.*`.
