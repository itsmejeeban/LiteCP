# LiteCP - By Hostingtom

LiteCP is a small, purpose-built control panel for self-hosted WHMCS, WordPress, and regular PHP/HTML websites. It supports Ubuntu and AlmaLinux and manages Nginx, parallel PHP-FPM runtimes, MySQL 8.4 LTS, files, cron, certificates, local and remote backups, firewall rules, databases, and email hosting.

The base panel is a single Go binary with an embedded vanilla HTML/CSS/JavaScript interface. It does not require a Node.js process, a cache/message-queue service, or a second database. Optional Rspamd spam filtering adds Redis-compatible cache storage and a local DNS resolver.

Release **0.5.1** adds failure-safe file extraction and migration, transactional mail configuration, MySQL-only installation, automatic security updates, mail-authentication bans, optional Rspamd filtering, and correctness fixes across demo, firewall, resolver, cron, WAF and PHP rollback paths. See [implementation boundaries, installation and validation status](docs/v0.5-development.md) before deploying optional services.

![LiteCP dashboard](docs/dashboard-preview.png)

## Included

- Responsive, session-protected administrator dashboard
- CPU, memory, disk, load, uptime, and essential service health
- Safe start/restart actions for Nginx, every installed PHP-FPM runtime, MySQL, Postfix, Dovecot, and cron
- WHMCS, WordPress, or regular PHP/HTML virtual hosts with application-specific sensitive-file rules
- Per-website PHP selection, isolated PHP-FPM worker identities, and safe runtime switching
- Optional MySQL database and one-time generated credentials
- Website-root-confined file manager with multiple upload, browser editing, download, folders, rename, permissions, delete, ZIP, and safe ZIP extraction
- Let's Encrypt certificates through Certbot
- Five-minute WHMCS or WordPress cron management using each website's PHP version
- On-demand website and database backups with FTP, encrypted FTPS, or host-key-pinned SFTP destinations
- Database creation, password reset, and destructive deletion with local-only application users
- Postfix/Dovecot virtual mail domains, authenticated mailboxes, quotas, forwarding aliases, SMTP submission, and IMAPS
- UFW or firewalld management for scoped custom inbound rules, with essential LiteCP ports protected
- Allowlisted Nginx, PHP, database, mail, and cron log views
- Parallel PHP 7.3, 7.4, 8.1, 8.2, 8.3, and 8.4 where native packages are available
- ionCube Loader, OPcache, and low-idle-memory FPM defaults for every runtime
- HTTPS panel proxy on port `8443`
- Loopback-only database access and a minimal host firewall enabled during installation
- Fail2ban protection for repeated SSH authentication failures

LiteCP does not download WHMCS or WordPress itself. Upload the licensed WHMCS release or the official WordPress release to the provisioned document root.

## Supported systems

- Ubuntu 22.04 or 24.04 (x86_64 or aarch64)
- AlmaLinux 8, 9, or 10 (x86_64 or aarch64)
- A fresh VPS is strongly recommended
- At least 1 GB RAM; 2 GB or more is more comfortable for database, mail, and PHP traffic

Ubuntu 22.04 and 24.04 receive the requested PHP versions through the co-installable Ondřej Surý PHP PPA. AlmaLinux uses native parallel Remi Software Collection packages. Because Remi does not publish PHP 7.3 for AlmaLinux 10, LiteCP runs PHP 7.3 there in a resource-limited Podman container based on the final official PHP 7.3 FPM image. The FastCGI socket and database stay local to the VPS. PHP 7.3, 7.4, and 8.1 are end-of-life and should only be used temporarily for legacy application upgrades.

The AlmaLinux 10 PHP 7.3 compatibility image is intentionally isolated because forcing Enterprise Linux 8 or 9 RPMs onto AlmaLinux 10 would mix incompatible system libraries. It still contains end-of-life software and receives no upstream PHP security fixes; migrate legacy applications to a supported runtime as quickly as possible.

Application compatibility is enforced by the panel:

- WHMCS 9.0: PHP 8.2 or 8.3.
- WHMCS 8.13 LTS: PHP 7.3, 7.4, 8.1, 8.2, or 8.3.
- WordPress: PHP 7.4, 8.1, 8.2, 8.3, or 8.4. PHP 8.3 or newer is recommended.
- Regular websites: any installed LiteCP PHP runtime; PHP 8.3 or newer is recommended for new applications.

Always check the current [WHMCS system requirements](https://docs.whmcs.com/8-13/installation-guide/system-requirements/) and [WordPress requirements](https://wordpress.org/about/requirements/) before upgrading an application or changing its PHP version.

> WHMCS officially validates Apache environments and notes that support for Nginx environments is limited. LiteCP uses Nginx deliberately for a smaller, focused runtime. See the same WHMCS requirements page before choosing this stack for a support-sensitive deployment.

## Install

Run this command from a complete LiteCP source checkout on the target server. LiteCP installs MySQL 8.4 LTS and refuses to mix or automatically replace an existing MariaDB installation:

```bash
sudo bash scripts/install.sh
```

On AlmaLinux, the installer uses Oracle's MySQL 8.4 LTS repository and binds MySQL to localhost. LiteCP only selects MySQL's legacy `mysql_native_password` authentication for sites running PHP 7.3 or 7.4; newer runtimes use the current MySQL default. Root administration remains protected by local Unix-socket authentication. Use strong generated passwords and do not expose port 3306 publicly.

The installer asks for an administrator username and password, installs the required packages, builds LiteCP, and prints the panel URL:

```text
https://SERVER_IP:8443
```

For automated provisioning, provide credentials through the process environment. The password is used only for first-time initialization and is not written to the service environment file.

```bash
sudo LITECP_ADMIN_USERNAME=admin \
  LITECP_ADMIN_PASSWORD='use-a-long-unique-passphrase' \
  LITECP_MAIL_HOSTNAME=mail.example.com \
  bash scripts/install.sh
```

The included panel certificate is self-signed so the first browser visit will show a warning. Verify its fingerprint over SSH or replace it with a trusted certificate before regular administration. Allow TCP ports `80`, `443`, and `8443` in any provider-level firewall. Email hosting additionally uses `25`, `465`, `587`, and `993`.

## First website

1. Point the billing domain's A/AAAA records to the server.
2. In LiteCP, choose **Add website**, select WHMCS, WordPress, or Regular PHP/HTML Website, select a compatible installed PHP version, and enter a name such as `billing.example.com`.
3. Save the generated database credentials immediately; the password is shown once.
4. Upload the licensed WHMCS files or official WordPress files to `/var/www/litecp/billing.example.com/public` and remove the placeholder `index.html`.
5. Complete the application web installer using the generated database values.
6. Enable SSL after DNS resolves to the server.
7. Confirm the application sees its five-minute system cron. For WordPress, define `DISABLE_WP_CRON` as `true` after the system cron is enabled.
8. Apply the application's recommended post-install file permissions. For WHMCS, move the cron directory outside the public document root if your security policy requires it.

## File manager

Choose a managed website under **File manager**. LiteCP confines every operation to that website's document root and refuses symbolic links. The manager supports up to four files per upload request and 512 MiB per file, UTF-8 text editing up to 4 MiB, folder creation, rename/move paths, safe permissions, download, delete, ZIP creation, and ZIP extraction. Multi-file uploads preflight name conflicts and roll back newly created files if any item fails.

ZIP extraction rejects absolute paths, traversal, symbolic links, duplicate/conflicting members, more than 100,000 members, and archives expanding beyond 2 GiB. Existing files are moved to private recovery storage before replacement and automatically restored if extraction fails. ZIP creation enforces its 8 GiB compressed-size limit while writing, before it can consume unbounded temporary disk space. The editor blocks binary files, special permission bits, and world-writable modes.

## Remote backups

Create an FTP, FTPS, or SFTP destination under **Backups**, then select it when running a website backup. The archive is always retained locally first. Destination passwords are encrypted with AES-GCM using `/var/lib/litecp/secret.key`, and neither the password nor encryption key is returned by the API.

SFTP destinations require the server's SHA256 host-key fingerprint. Obtain and verify that fingerprint through a trusted channel before saving the destination. Plain FTP does not encrypt credentials or backup contents; prefer SFTP or FTPS.

## Email hosting

LiteCP provides lightweight virtual mail hosting through Postfix and Dovecot. Add a mail domain, then create mailboxes and aliases. Clients use SMTP submission on `465` or `587` and IMAPS on `993` with the full mailbox address as the username.

Production delivery also requires work outside LiteCP:

- Set the mail hostname to a public FQDN and install a trusted certificate matching it.
- Point the mail hostname A/AAAA record and each hosted domain's MX record to the server.
- Publish the SPF, DKIM, and DMARC records shown by LiteCP. OpenDKIM signs outgoing messages after the optional mail feature is installed.
- Confirm that the VPS provider permits outbound TCP port 25 and configure reverse DNS with the provider.
- Test reputation and deliverability before sending billing messages.

The installer configures authenticated submission and rejects unauthenticated relaying. Mail-map changes are staged and rolled back if activation or service reload fails. Removing a mailbox or mail domain disables delivery/login but intentionally keeps its Maildir under `/var/vmail` for manual recovery.

For inbound/outbound spam filtering, run `sudo bash scripts/install-features.sh antispam`. This installs Rspamd, Redis (or AlmaLinux 10's compatible Valkey service) and a localhost-only Unbound resolver, connects Rspamd's milter to Postfix, and keeps the Rspamd controller private on loopback. Filtering and authentication improve delivery safety but cannot guarantee inbox placement.

## Firewall management

LiteCP manages custom allow rules through UFW on Ubuntu and firewalld on AlmaLinux. Rules can be limited to an IP address or CIDR range. Core SSH, web, panel, and mail ports cannot be duplicated or removed through the panel, reducing the chance of locking out the administrator.

## Developer run

Go 1.18 or newer is required.

```bash
export LITECP_DATA_DIR="$PWD/.data"
export LITECP_ADMIN_PASSWORD='a-long-development-password'
go run . init --username admin
unset LITECP_ADMIN_PASSWORD
go run .
```

Open `http://127.0.0.1:9090`. System mutations automatically use preview behavior on non-Linux systems. The frontend also has a localhost-only visual preview at `http://127.0.0.1:9090/?demo=1`.

Run the test suite with:

```bash
go test ./...
```

## Data and recovery

- Panel configuration: `/var/lib/litecp/config.json` (`0600`)
- Website roots: `/var/www/litecp/<domain>/public`
- Nginx configs: `/etc/nginx/sites-available/litecp-*.conf` on Ubuntu or `/etc/nginx/conf.d/litecp-*.conf` on AlmaLinux
- Application cron files: `/etc/cron.d/litecp-*`
- Backups: `/var/backups/litecp/*.tar.gz`
- Encrypted-secret key: `/var/lib/litecp/secret.key` (`0600`)
- Mail maps and password hashes: `/etc/litecp/mail`
- Maildir data: `/var/vmail/<domain>/<mailbox>/Maildir`

Detaching a website removes its LiteCP-managed Nginx, isolated PHP pool, and cron configuration, but it intentionally keeps website files and the database. Local backups older than the configured retention period are pruned after a successful backup or settings update; off-server retention remains the remote provider's responsibility.

## Security model

- PBKDF2-HMAC-SHA256 password hashing with a unique salt and 600,000 iterations
- HTTP-only, SameSite=Strict, time-limited in-memory sessions; changing the password revokes all sessions
- Reverse-proxy-aware login throttling and constant-time credential comparison
- Same-origin checks for state-changing requests
- A fixed command and service allowlist; no browser-provided shell commands
- Descriptor-relative file operations that refuse symlinks, plus ZIP-member, archive-size, upload-size, and permission validation
- AES-256-GCM encryption for stored remote-backup credentials
- SFTP SHA256 host-key pinning and TLS-required FTPS support
- Local-socket-only database administration with validated SQL identifiers
- Authenticated SMTP submission with unauthenticated relay rejection
- Fail2ban protection for SSH, Postfix/SASL and Dovecot, plus automatic operating-system security updates
- Atomic durable state/configuration writes, rollback on failed mutations, and strict domain validation
- Per-website numeric PHP identities, sockets, and document-root restrictions
- Panel process isolated with systemd filesystem and kernel protections
- No application database password persistence after provisioning; mailbox passwords are one-way hashed

LiteCP runs as root because it must update Nginx, cron, certificates, ownership, and system services. Keep port `8443` private to administrator IPs when possible, maintain normal operating-system updates, and copy backups to separate storage.

## Project layout

```text
.
├── main.go               HTTP server, authentication, and core API
├── main_extra.go         file, database, backup, firewall, and mail APIs
├── store*.go             atomic JSON configuration store
├── system.go             core allowlisted Linux operations
├── files.go              confined file manager and ZIP handling
├── infrastructure.go     secrets, remote backup, database, and firewall operations
├── mail.go               Postfix/Dovecot maps and mailbox operations
├── web/                  embedded control-panel UI
├── deploy/               systemd and PHP/FPM profiles
└── scripts/install.sh    Ubuntu/AlmaLinux installer
```

## License

Add the license appropriate for your distribution before publishing the project.
