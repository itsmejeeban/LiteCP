# Mail authentication repair for the audit domain

Domain: qa-backup-audit.litimcsellcop.com
DNS provider: Cloudflare (authoritative nameservers observed 6 November 2026).

The requested live test reached Gmail's SMTP server with 250 OK at 20:48 IST; the user reports Spam placement. The sender subdomain has no published SPF or DKIM record. Parent SPF does not authenticate an unconfigured subdomain. Inbox placement must be retested after DNS authentication passes; this is not guaranteed by DNS changes alone.

## Records to publish

| Type | Name (relative to litimcsellcop.com) | Value | Priority |
| --- | --- | --- | --- |
| MX | qa-backup-audit | litecp.litimcsellcop.com | 10 |
| TXT | qa-backup-audit | v=spf1 ip4:162.55.50.122 -all | |
| TXT | default._domainkey.qa-backup-audit | See public DKIM value below | |

Do not replace an existing record without first checking it. MX must remain DNS-only. Preserve the parent domain's existing DMARC policy; it is inherited by the subdomain unless overridden.

Public DKIM TXT value (one record; do not include code-fence markers):

```text
v=DKIM1; k=rsa; p=MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA4ezGEZ3A6J0WZtZZ16MYlDgwKtgplEP4kKmnjEeuqbaY0U1aUCCmRqqaAaImfERCS5Qg0J5PB8wAecsjA0PjDnE0mqcK/uMpyvYWGrJcrcNw1/6Nc2aMdvehbKXVIonUcUiX7e2uPLgFfwidPC7SMG/O15VEE/915q6wPCLl3yQWOLKyKJVdkcqOcGlZu/1vcHQFCZqn40GJZ2uJs3CdoagldQB6JxhnbaWPc6K0vciePpVVIR8Xj7bVQrFF6q6uaf3vxOLGFL7LIQZTN0UcAkRPgFP1R/+CHnDBNLOvUe2PQSy1v/ZLyVo5GmFKlPKUpGFG71kw/wNqog0LmeJ5swIDAQAB
```

The key above is public. The existing private key was not changed or disclosed. This record is for the audit subdomain only, not a universal DKIM record for every hosted domain.

## Acceptance

- Query authoritative DNS and a public recursive resolver for the records.
- Run opendkim-testkey for selector default and the exact sender domain.
- Send one new requested test after propagation.
- In Gmail Show original, verify SPF, DKIM and DMARC all PASS, then record Inbox/Spam placement separately.
- If authentication passes but Spam placement remains, investigate IP/domain reputation and message content; do not weaken DMARC or disable filtering to claim success.

## Applied and verified - 6 November 2026

- Published the DKIM TXT, sender SPF TXT and MX records in the existing Cloudflare zone. Preserved all existing records.
- Added explicit DNS-only A record qa-backup-audit → 162.55.50.122 to preserve website resolution when the new TXT/MX records suppress wildcard synthesis at that name.
- Authoritative Cloudflare and public resolvers 1.1.1.1/8.8.8.8 return the expected records. opendkim-testkey with the existing private signing key reports key OK. The informational 'key not secure' denotes unsigned DNS/DNSSEC validation, not a mismatched RSA key.
- Post-repair test 02 initially bounced from Gmail with 550 5.7.26. Its original message contained a DKIM signature; verification with opendkim-testmsg against the published key returned exit 0. Together with the later acceptance, the timing is consistent with earlier DNS results being cached; Gmail's internal resolver cache was not directly inspected.
- Final test 03: queue 34BD159F8, Gmail SMTP response 250 2.0.0 OK at 20:58:59 IST, queue empty afterward. Actual Inbox/Spam placement and recipient Authentication-Results remain pending user confirmation.
- No source-code or server mail configuration change was required. Existing signing key unchanged. This validates the audit sender domain only; each additional hosted mail domain needs its own matching DNS setup.
- Evidence screenshot: .staging/mail-dns-repair-final-20261106.jpg (local ignored staging artifact).
- Recipient subsequently confirmed final test 03 arrived in Spam. Therefore Inbox placement is NOT fixed or verified. Gmail SPF/DKIM/DMARC results have been requested to distinguish a remaining authentication failure from reputation/content classification. No further test emails sent after that report.
- Recipient supplied Gmail Show original results for final test 03: SPF PASS, DKIM PASS, DMARC PASS; transport ESMTPS TLS 1.3. Authentication is verified end to end. Raw recipient headers were not copied into the repository.
- Spam placement persists despite passing authentication. The supplied headers do not identify Gmail's classification reason. Reputation/content are possible factors, not proven root causes. No claim of universal Inbox delivery or delivery readiness for other mail domains is made.
