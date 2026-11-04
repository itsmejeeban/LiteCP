# Brute-force live acceptance test

Test started 4 November and completed 5 November 2026 (Asia/Kolkata).
The installed panel reported 4.0.94; source fixes are queued in 4.0.97.

## Actual authentication requests

SSH was tested using a temporary network namespace, a dedicated veth pair, and
source IP `198.18.77.2`. The nonexistent username `qa_bf_nonexistent_1104` and a
deliberately invalid password were used. Host-key verification remained enabled;
the known-host key was obtained through the existing pinned administrative SSH
connection. No administrator credentials were guessed.

The configured SSH policy remained 5 matched failures in 600 seconds, with a
3600-second ban. Three SSH connections produced failed-password/invalid-user
events; Fail2ban can match more than one event from one SSH connection. The test
stopped when the IP was banned. A subsequent TCP connection from that namespace
was refused. Fail2ban logged the Ban at 18:20:47 UTC. Only this test IP was then
unbanned, and the created namespace/interfaces were removed. Historical events
were retained and verified in Chrome's Brute Force Protection page.

Panel login protection was tested with seven real HTTP requests from a separate
temporary bound source IP `198.18.78.1`, without spoofing forwarding headers.
Requests 1-5 returned 401; requests 6-7 returned 429 with the 15-minute rate-limit
message. The temporary source interface was removed. The real administrator's
source address and login allowance were not used.

Mail jails were active and their events visible, but no mailbox/SMTP password
attack was performed. Enable/disable and policy changes were not exercised.

## Issues found and repaired

The panel initially returned 502 and was restarting because confined Nginx could
not load WAF vendor rules labeled `var_t`. Vendor archive UID/GID 502/games was
also retained. The earlier SELinux acceptance checks had not exercised a reload.

Live repair preserved the previous metadata/policy export, made vendor rules
root-owned, persisted the scoped `httpd_config_t` mapping and restored labels.
Nginx reloaded successfully, LiteCP recovered, SELinux stayed Enforcing, and the
test website returned 200 while the existing WAF-blocked test path returned 403.
No WAF rule, firewall protection, password or policy threshold was disabled.

Permanent source changes normalize vendor ownership/labels during installation
and updates and perform a confined Nginx reload during SELinux enablement.
The UI also now clears old read errors after a successful refresh and disables
actions when status cannot be verified. These source changes are not yet deployed.

Evidence directories retained on the server:

- `/root/litecp-bruteforce-qa-zGIjd8F0` (initial attempt)
- `/root/litecp-bruteforce-qa-43gvyzJ2` (ban, blocked connection and metadata backup)

Screenshot: `brute-force-live-test-2026-11-04.jpg` in the project root.
Existing Makefile retained; no production build or release package generated.
