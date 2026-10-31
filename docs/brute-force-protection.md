# Brute Force Protection

Security > Brute Force Protection manages the installed Fail2ban service. Enable starts it and enables startup after reboot; Disable stops it and disables startup after confirmation. This does not disable LiteCP sign-in rate limiting.

Select an active SSH or mail jail to configure retries, detection window and ban duration in seconds. Supported jails are sshd, postfix, postfix-sasl and dovecot. Rules persist in a dedicated jail override. Invalid configurations are rejected and previous configuration is restored; recovery copies are retained under /var/lib/litecp/brute-force-change.*. This page does not install Fail2ban or create unsupported jails.

Recent events show the latest 200 Found, Ban and Unban entries from available Fail2ban logs, with pagination. File logging is preferred; journal entries from the last 24 hours are used otherwise. Logging/rotation determine available history. Jail statistics and banned IPs are reported by Fail2ban. Missing service/logs are shown explicitly.

The authenticated API accepts only fixed managed jail names and bounded numeric settings. Updates use a serialized privileged systemd task. Existing firewall and SSH configuration are preserved.
