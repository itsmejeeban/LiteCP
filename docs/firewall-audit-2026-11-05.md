# Firewall audit, 5 November 2026

Live Chrome checks on 4.0.103 passed custom-rule creation, editing from inbound
to outbound, invalid-port validation and approved deletion. A TCP 65001 deny rule
scoped only to documentation address 203.0.113.254 was verified in runtime and
permanent firewalld configuration. The test rule was removed afterwards. Panel,
broker and firewalld remained active, and the panel health endpoint returned 200.

Required-profile inspection failed under enforcing SELinux because systemd-run
could not pass captured broker output pipes to its transient unit. A captured
query under unconfined_service_t reproduced the error; a direct firewall-cmd
query succeeded. Version 4.0.104 keeps firewalld operations inside the broker's
existing sandbox: configuration writes are performed by the firewalld D-Bus
service, which is already how custom rules operate. Only the exact read-only
/usr/sbin/sshd -T probe also bypasses the transient writer; UFW retains its host
execution path. SELinux policy and service sandbox restrictions are unchanged.

Failed inspection now offers a read-only Retry firewall check button. Profile
removal and disablement preserve the installer's recorded SSH port as well as
sshd -T ports, because sshd's live -p command-line option is absent from -T output.
Unscoped custom changes and Cloudflare restrictions also protect that saved port.

Required-profile apply/disable/removal and Cloudflare-only transitions were not
performed live during this audit. The repaired profile needs a post-update live
retest. No claim of UFW validation on this AlmaLinux/firewalld server is made.
