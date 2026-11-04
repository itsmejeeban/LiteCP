# Traffic Analytics audit - 2026-11-04

Chrome access was initially disabled, then restored. Live panel was v4.0.85.
Totals, status-code reports, collection timestamps, method and HTTP-status filters
loaded successfully. Paused-filter stale rows were reproduced on that older build.
Custom date validation returned a clear error. No server data was modified.

Confirmed in code and regression tests: changing live filters while paused kept
old rows visible under the new filters. Clear the old entries on filter changes
and retain a clear paused/resume message when switching websites or report controls.
Pause still stops live API polling; Resume sends the current website/filter selection.

Also reproduced: choosing hourly charts for a range over 31 days made Live Access
fail with the chart interval error. Live requests now use a neutral daily grouping
while retaining the requested dates; chart validation remains intact.

Seven frontend tests passed, including escaped log data, polling lifecycle, stale
responses, custom dates, pagination, empty websites and paused-filter changes.
Focused Go traffic tests, JS syntax and diff checks passed.

Local version 4.0.87; not deployed. Existing Makefile retained. No build or package
generated. Earlier quota-installer work remains in the working tree.
