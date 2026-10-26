# Website FTP accounts

Use the FTP accounts button beside a website to create a login, edit its password, or remove it after confirmation. Each virtual account is confined to that website's public directory and uses its existing runtime UID/GID. No shell or OS login is created. Usernames are globally unique and cannot be renamed; create a separate login if another name is required.

Run `bash scripts/install-ftp.sh` as root on an existing LiteCP installation. It installs Pure-FTPd and a dedicated `litecp-ftps` service with virtual-user authentication only. It requires explicit FTPS on port 21, encrypted data channels, and passive ports 50000–50049. Allow those TCP ports in the provider firewall as well as the server firewall. Plain FTP and anonymous login are disabled.

The installer initially inherits the panel TLS certificate. Before production, provision a trusted certificate matching your FTP hostname and update `/var/lib/litecp/ftp/tls.pem` (private key plus certificate chain, root-only), then restart `litecp-ftps`. Do not disable certificate validation in FTP clients.

Account metadata and password hashes are stored under `/var/lib/litecp/ftp` with root-only permissions. Plaintext passwords are not stored. Root-only transaction snapshots are retained for recovery; their removal is administrator-managed. Removing an account removes authentication and metadata, preserving all website files. Already authenticated connections may remain active until they disconnect.

The API validates account membership against the selected website and serializes mutations through the panel's existing mutation lock. Per-account connections are limited to three. Websites must have an isolated runtime UID/GID of at least 1000. FTP access permits changing website files, including executable PHP, within that site's permissions.

Validation: isolated virtual-account create/password-change/remove tests, chroot path checks, plaintext password checks, cross-site account rejection, and panel authentication tests. These do not substitute for an external FTP-client connectivity test or cloud firewall verification.
