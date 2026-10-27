# Webmail and mail signing

LiteCP's optional mail component installs Roundcube webmail and OpenDKIM. Users open the mailbox's **Log in** button and enter that mailbox's password. Administrator panel login does not reveal or reset mailbox passwords.

Use a public mail hostname with an A record pointing directly to the server and a matching, unexpired trusted TLS certificate. For an existing installation, a separate mail certificate can be supplied without changing the panel certificate:

```bash
LITECP_MAIL_HOSTNAME=mail.example.com \
LITECP_MAIL_CERT=/etc/letsencrypt/live/mail.example.com/fullchain.pem \
LITECP_MAIL_KEY=/etc/letsencrypt/live/mail.example.com/privkey.pem \
bash scripts/install-features.sh mail
```

The installer verifies the hostname, expiry, and matching private key before installing packages or changing mail services. It retains downloaded archives and configuration backups under `/var/backups/litecp/feature-install-*`. Roundcube 1.7.5 uses a pinned SHA256 archive. Webmail listens on HTTPS port 8444; IMAP uses TLS port 993, and authenticated SMTP uses TLS 465 or STARTTLS 587. Open these ports in the provider firewall as required. HTTP proxy services that do not support port 8444 must leave this hostname as DNS-only or use a separately configured supported HTTPS entry point.

Postfix and Dovecot use the selected certificate. A certificate-specific Certbot deploy hook validates configuration and reloads running mail listeners after renewal; disabled mail services stay disabled. The panel certificate and operating-system hostname are unchanged.

Publish each mail domain's MX, SPF, DKIM, and DMARC records from **DNS records**, and set the server IP's PTR at the hosting provider to the selected mail hostname. A locally created mailbox does not publish public DNS. Keep one SPF record and merge other authorized senders. Begin DMARC in monitoring mode. Mail delivery also depends on outbound TCP 25, forward/reverse DNS, reputation, and recipient policy; these settings cannot guarantee inbox placement.

On the current test server, `litecp.litimcsellcop.com` resolves to the server and has trusted TLS. Roundcube responds successfully at `https://litecp.litimcsellcop.com:8444/`, and SMTP/IMAP certificate checks pass. An isolated generated mailbox successfully logged into IMAP, authenticated to SMTP on 465 and 587, and reached Roundcube's authenticated mail page. Its credentials are retained root-only on the server. The existing `tmail.hostingtom.org` mail domain currently returns NXDOMAIN, and PTR still identifies the provider's default hostname. External delivery remains unverified; no test email was sent and no existing password was changed.

Official references: [Roundcube releases](https://github.com/roundcube/roundcubemail/releases), [Roundcube configuration](https://github.com/roundcube/roundcubemail/wiki/Configuration).
