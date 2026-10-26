# phpMyAdmin access

The Databases page has separate copy icons for database names and usernames. Clicking a database name opens phpMyAdmin in a normal new tab with that database selected after login.

phpMyAdmin uses its own cookie login with the managed MySQL username and password. LiteCP does not store database passwords and does not provide passwordless database login. Once authenticated, phpMyAdmin provides its normal table browser, SQL, import and export tools subject to that MySQL user's privileges.

The installer runs `scripts/install-phpmyadmin.sh`. For existing installations, run that script as root from a complete LiteCP source checkout. It installs official phpMyAdmin 5.2.3 with SHA256 verification, a small isolated PHP 8.3 pool, and an Nginx listener bound to 127.0.0.1:9091. Public requests go through `/api/phpmyadmin/` and require an active LiteCP session. PHP never receives the panel session cookie. MySQL root login, empty passwords, arbitrary server selection and phpMyAdmin setup URLs are disabled. Database users remain restricted to their existing grants.

Prerequisites: PHP 8.3 FPM with mysqli, mbstring, session, XML, ctype and JSON; Nginx; curl; tar; OpenSSL. Ubuntu and AlmaLinux paths match the standard LiteCP installer. Uploads are limited to 128 MiB and PHP requests to 300 seconds. Larger imports should use the server's supported import tools.

Logging out of LiteCP or restarting the panel blocks all subsequent phpMyAdmin requests until LiteCP is signed in again. Database login remains separate. No database passwords are reset by opening phpMyAdmin.

References: [official release](https://www.phpmyadmin.net/downloads/) and [authentication configuration](https://docs.phpmyadmin.net/en/latest/config.html).
