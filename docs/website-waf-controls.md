# Website WAF controls

Website Tools > Website WAF configures the selected website's protection mode,
managed attack protection, custom rules and rate limits. Security > Website WAF
shows the same websites with individual enable/disable buttons, selection across
pages, search, and bulk Enable selected / Disable selected actions.

Disabling retains the website's configured mode, rules and rate limits. It
suspends that website's ModSecurity inspection, including inherited server WAF
rules, and puts inherited request limiting into dry-run mode. Other websites and
the saved server policy are unchanged. Enabling restores the configured mode;
an unconfigured website starts with managed protection in Detect only mode.

Bulk actions use one authenticated policy update and the existing Nginx
validation/reload/rollback path. All supplied website IDs are checked before
applying any settings. Configuration saves preserve the current Security switch,
including when the editor was opened before the switch changed. Policies without
the new optional `enabled` property retain their previous behavior.

The old Security WAF route redirects to the switch list. The server policy API
and stored server restrictions remain compatible, but server rule configuration
is not exposed by the Security switch list.

Verification: `node --test scripts/waf-controls.test.cjs` and
`go test ./... -run 'TestWAF'`. The existing root Makefile remains the build/test
entry point. These changes do not require a package or live deployment.
