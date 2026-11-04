# Email practical audit

Live version 4.0.92. All ten Email routes opened without the previous webmail gate.

- Create Mailbox/Mailboxes: authorized qa_mail_audit@qa-placeholder-renamed.litimcsellcop.com created and listed. UI minimum is 100 MB (64 MB rejected before submission). Password generated through the UI, not recorded in this report. Password reset and mailbox login not tested.
- Add Mail Domain/Mail Domains: added disposable qa-placeholder-renamed.litimcsellcop.com; listing updated.
- Create Alias/Aliases: qa_alias forwarded to the local audit mailbox, listing updated.
- Email Forward: qa_forward forwarded to the same local audit mailbox, listing updated. No external recipients or messages used.
- Email Delivery: retained events loaded. Logs included rejected external relay attempts; no test email was transmitted.
- SMTP Port Check: four checks to Google/Yahoo ports 25/465 returned Working; no email sent.
- Configure Webmail: form/status loaded and Refresh status worked. Setup has not been run; requires a public mail hostname, DNS and TLS. Roundcube login is unverified.

Bug: mailbox Configuration and Details showed single-label hostname litecp as an incoming/outgoing server, with invalid public MX target litecp. Local v4.0.93 rejects DNS/client-settings generation until a valid public mail hostname is configured, with a clear prerequisite error. No fabricated fallback hostname or DNS changes.

Focused Go mail/SMTP/webmail tests passed, including hostname regression. Existing Makefile retained. No build/package/deployment. Audit domain, mailbox and two aliases remain; no deletion performed.
