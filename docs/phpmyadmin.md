# phpMyAdmin access

The Databases page has separate copy icons for database names and usernames. Clicking a database name opens phpMyAdmin in a normal new tab with that database selected after login.

Clicking a managed database signs in automatically using a dedicated localhost-only helper account restricted to that one database, with no global privileges or grant option. Application database passwords are not read or changed. Helper credentials are encrypted under the LiteCP secret key in the root-only phpmyadmin-credentials directory. They are retained for reuse; the panel does not automatically remove these accounts or credential files. Once authenticated, phpMyAdmin provides its normal table browser, SQL, import and export tools within those database privileges.

The installer runs `scripts/install-phpmyadmin.sh`. For existing installations, run that script as root from a complete LiteCP source checkout. It installs official phpMyAdmin 5.2.3 with SHA256 verification, a small isolated PHP 8.3 pool, and an Nginx listener bound to 127.0.0.1:9091. Public requests go through `/api/phpmyadmin/` and require an active LiteCP session. PHP never receives the panel session cookie. MySQL root login, empty passwords, arbitrary server selection and phpMyAdmin setup URLs are disabled. Database users remain restricted to their existing grants.

Prerequisites: PHP 8.3 FPM with mysqli, mbstring, session, XML, ctype and JSON; Nginx; curl; tar; OpenSSL. Ubuntu and AlmaLinux paths match the standard LiteCP installer. Uploads are limited to 128 MiB and PHP requests to 300 seconds. Larger imports should use the server's supported import tools.

Logging out of LiteCP or restarting the panel blocks all subsequent phpMyAdmin requests until LiteCP is signed in again. Launch tickets expire after 30 seconds, work once, and are bound to the active panel session. Tickets travel in POST bodies; passwords never go to browser code. No database passwords are reset by opening phpMyAdmin.

References: [official release](https://www.phpmyadmin.net/downloads/) and [authentication configuration](https://docs.phpmyadmin.net/en/latest/config.html).
