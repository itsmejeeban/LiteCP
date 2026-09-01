# LiteCP - By Hostingtom

LiteCP is a small, purpose-built control panel for self-hosted WHMCS and WordPress websites. It supports Ubuntu and AlmaLinux and intentionally manages only the stack these applications need: Nginx, parallel PHP-FPM runtimes, MariaDB, cron, certificates, local backups, and logs.

The panel is a single Go binary with an embedded vanilla HTML/CSS/JavaScript interface. It does not run a Node.js process, Redis, a message queue, or a second database.

![LiteCP dashboard](docs/dashboard-preview.png)

## Included

- Responsive, session-protected administrator dashboard
- CPU, memory, disk, load, uptime, and essential service health
- Safe start/restart actions for Nginx, every installed PHP-FPM runtime, MariaDB, and cron
- WHMCS or WordPress virtual hosts with application-specific sensitive-file rules
- Per-website PHP selection and safe runtime switching
- Optional MariaDB database and one-time generated credentials
- Let's Encrypt certificates through Certbot
- Five-minute WHMCS or WordPress cron management using each website's PHP version
- On-demand website and database backups
- Allowlisted Nginx, PHP, MariaDB, and cron log views
- Parallel PHP 7.3, 7.4, 8.1, 8.2, 8.3, and 8.4 where native packages are available
- ionCube Loader, OPcache, and low-idle-memory FPM defaults for every runtime
- HTTPS panel proxy on port `8443`
- Loopback-only MariaDB and a minimal host firewall enabled during installation

LiteCP does not download WHMCS or WordPress itself. Upload the licensed WHMCS release or the official WordPress release to the provisioned document root.

## Supported systems

- Ubuntu 22.04 or 24.04 (x86_64 or aarch64)
- AlmaLinux 8, 9, or 10 (x86_64 or aarch64)
- A fresh VPS is strongly recommended
- At least 1 GB RAM; 2 GB or more is more comfortable for MariaDB and PHP traffic

Ubuntu 22.04 and 24.04 receive all requested PHP versions through the co-installable Ondřej Surý PHP PPA. AlmaLinux uses native parallel Remi Software Collection packages. Remi does not publish PHP 7.3 for AlmaLinux 10, so LiteCP reports that runtime as unavailable there instead of mixing incompatible packages from an older Enterprise Linux release. PHP 7.3, 7.4, and 8.1 are end-of-life and should only be used temporarily for legacy application upgrades.

Application compatibility is enforced by the panel:

- WHMCS: PHP 7.3, 7.4, 8.1, 8.2, or 8.3. PHP 8.3 requires WHMCS 8.13 or newer.
- WordPress: PHP 7.4, 8.1, 8.2, 8.3, or 8.4. PHP 8.3 or newer is recommended.

Always check the current [WHMCS system requirements](https://docs.whmcs.com/8-13/installation-guide/system-requirements/) and [WordPress requirements](https://wordpress.org/about/requirements/) before upgrading an application or changing its PHP version.

> WHMCS officially validates Apache environments and notes that support for Nginx environments is limited. LiteCP uses Nginx deliberately for a smaller, focused runtime. See the same WHMCS requirements page before choosing this stack for a support-sensitive deployment.

## Install

Run these commands from a complete LiteCP source checkout on the target server:

```bash
sudo bash scripts/install.sh
```

The installer asks for an administrator username and password, installs the required packages, builds LiteCP, and prints the panel URL:

```text
https://SERVER_IP:8443
```

For automated provisioning, provide credentials through the process environment. The password is used only for first-time initialization and is not written to the service environment file.

```bash
sudo LITECP_ADMIN_USERNAME=admin \
  LITECP_ADMIN_PASSWORD='use-a-long-unique-passphrase' \
  bash scripts/install.sh
```

The included panel certificate is self-signed so the first browser visit will show a warning. Verify its fingerprint over SSH or replace it with a trusted certificate before regular administration. Allow TCP ports `80`, `443`, and `8443` in any provider-level firewall.

## First website

1. Point the billing domain's A/AAAA records to the server.
2. In LiteCP, choose **Add website**, select WHMCS or WordPress, select a compatible installed PHP version, and enter a name such as `billing.example.com`.
3. Save the generated database credentials immediately; the password is shown once.
4. Upload the licensed WHMCS files or official WordPress files to `/var/www/litecp/billing.example.com/public` and remove the placeholder `index.html`.
5. Complete the application web installer using the generated database values.
6. Enable SSL after DNS resolves to the server.
7. Confirm the application sees its five-minute system cron. For WordPress, define `DISABLE_WP_CRON` as `true` after the system cron is enabled.
8. Apply the application's recommended post-install file permissions. For WHMCS, move the cron directory outside the public document root if your security policy requires it.

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

Detaching a website removes its LiteCP-managed Nginx and cron configuration, but it intentionally keeps website files and the database. Backups are never silently deleted; the retention setting is a policy reminder so an operator can coordinate off-server retention.

## Security model

- PBKDF2-HMAC-SHA256 password hashing with a unique salt and 600,000 iterations
- HTTP-only, SameSite=Strict, time-limited in-memory sessions
- Login throttling and constant-time credential comparison
- Same-origin checks for state-changing requests
- A fixed command and service allowlist; no browser-provided shell commands
- Atomic configuration writes and strict domain validation
- Panel process isolated with systemd filesystem and kernel protections
- No database password persistence after provisioning

LiteCP runs as root because it must update Nginx, cron, certificates, ownership, and system services. Keep port `8443` private to administrator IPs when possible, maintain normal operating-system updates, and copy backups to separate storage.

## Project layout

```text
.
├── main.go               HTTP server, authentication, and API
├── store.go              atomic JSON configuration store
├── system.go             allowlisted Linux operations
├── web/                  embedded control-panel UI
├── deploy/               systemd and PHP/FPM profiles
└── scripts/install.sh    Ubuntu/AlmaLinux installer
```

## License

Add the license appropriate for your distribution before publishing the project.
