SELinux enablement from Security Dashboard
=========================================

In version 4.0.100, the SELinux card offers Enable SELinux when the server is
Permissive. Confirming starts a background systemd job. The card shows progress,
the result and a repair log; reopening the page resumes status checks.

The job repairs managed WAF labels, backs up /etc/selinux/config, arms an
independent five-minute rollback guard, enables enforcement, checks Nginx reload,
the local panel endpoint and previously active services, then persists enforcing
mode. Failed attempts retain logs and configuration backups. Wait eight minutes
before retrying a failed attempt so an outstanding guard cannot race a new job.

Disabled-at-boot or unknown SELinux states cannot be enabled with this button.
Disabled installations need OS maintenance and filesystem relabeling. The panel
does not reboot automatically. These checks do not replace testing every hosted
application, mail workflow and application-specific SELinux policy.
