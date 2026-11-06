# Full panel API permission catalog

Each row is an explicit permission. Existing credentials gain none automatically. JSON bodies use `/api/v1`; binary/multipart transfers use `/api/v1/request`. See [the integration guide](panel-api.md).

| Permission | Area | Method | Panel route | Availability |
|---|---|---|---|---|
| `panel.access.get` | API Administration | GET | `/api/access` | Supported |
| `panel.access.post` | API Administration | POST | `/api/access` | Supported |
| `panel.state.get` | Overview | GET | `/api/state` | Supported |
| `panel.activity_policy.get` | Recent Activities | GET | `/api/activity-policy` | Supported |
| `panel.activity_policy.put` | Recent Activities | PUT | `/api/activity-policy` | Supported |
| `panel.activity_policy.delete` | Recent Activities | DELETE | `/api/activity-policy` | Supported |
| `panel.activities.get` | Recent Activities | GET | `/api/activities` | Supported |
| `panel.php.extensions.get` | Server Environment | GET | `/api/php/extensions` | Supported |
| `panel.php.extensions.put` | Server Environment | PUT | `/api/php/extensions` | Supported |
| `panel.security.scan.post` | Security | POST | `/api/security/scan` | Supported |
| `panel.security.server_scan.get` | Security | GET | `/api/security/server-scan` | Supported |
| `panel.security.server_scan.post` | Security | POST | `/api/security/server-scan` | Supported |
| `panel.security.server_scan.put` | Security | PUT | `/api/security/server-scan` | Supported |
| `panel.security.malware_files.get` | Security | GET | `/api/security/malware-files` | Supported |
| `panel.security.malware_files.post` | Security | POST | `/api/security/malware-files` | Supported |
| `panel.live_migrations.auto_restore.post` | Migration | POST | `/api/live-migrations/auto-restore` | Supported |
| `panel.backup_schedules.get` | Backups | GET | `/api/backup-schedules` | Supported |
| `panel.backup_schedules.post` | Backups | POST | `/api/backup-schedules` | Supported |
| `panel.backup_schedules.put` | Backups | PUT | `/api/backup-schedules` | Supported |
| `panel.backup_schedules.delete` | Backups | DELETE | `/api/backup-schedules` | Supported |
| `panel.security.posture.get` | Security | GET | `/api/security/posture` | Supported |
| `panel.security.integrity.get` | Security | GET | `/api/security/integrity` | Supported |
| `panel.security.integrity.post` | Security | POST | `/api/security/integrity` | Supported |
| `panel.security.selinux.get` | Security | GET | `/api/security/selinux` | Supported |
| `panel.security.selinux.post` | Security | POST | `/api/security/selinux` | Supported |
| `panel.security.two_factor.get` | Security | GET | `/api/security/two-factor` | Supported |
| `panel.security.two_factor.post` | Security | POST | `/api/security/two-factor` | Supported |
| `panel.security.waf_events.get` | Security | GET | `/api/security/waf-events` | Supported |
| `panel.security.waf_policies.get` | Security | GET | `/api/security/waf-policies` | Supported |
| `panel.security.waf_policies.put` | Security | PUT | `/api/security/waf-policies` | Supported |
| `panel.sites.waf_logs.get` | Website Tools | GET | `/api/sites/waf-logs` | Supported |
| `panel.sites.waf.put` | Website Tools | PUT | `/api/sites/waf` | Supported |
| `panel.system.hostname.put` | Settings | PUT | `/api/system/hostname` | Supported |
| `panel.system.resolvers.put` | Settings | PUT | `/api/system/resolvers` | Supported |
| `panel.system.reboot.post` | Settings | POST | `/api/system/reboot` | Supported |
| `panel.services.action.post` | Services | POST | `/api/services/action` | Supported |
| `panel.sites.post` | Website Tools | POST | `/api/sites` | Supported |
| `panel.sites.sizes.get` | Website Tools | GET | `/api/sites/sizes` | Supported |
| `panel.sites.traffic.get` | Website Tools | GET | `/api/sites/traffic` | Supported |
| `panel.sites.suspension.put` | Website Tools | PUT | `/api/sites/suspension` | Supported |
| `panel.sites.wordpress.setup.post` | Website Tools | POST | `/api/sites/wordpress/setup` | Supported |
| `panel.sites.ssl.post` | Website Tools | POST | `/api/sites/ssl` | Supported |
| `panel.sites.cron.post` | Website Tools | POST | `/api/sites/cron` | Supported |
| `panel.sites.php.post` | Website Tools | POST | `/api/sites/php` | Supported |
| `panel.sites.php_settings.get` | Website Tools | GET | `/api/sites/php-settings` | Supported |
| `panel.sites.php_settings.put` | Website Tools | PUT | `/api/sites/php-settings` | Supported |
| `panel.sites.redis.get` | Website Tools | GET | `/api/sites/redis` | Supported |
| `panel.sites.redis.put` | Website Tools | PUT | `/api/sites/redis` | Supported |
| `panel.sites.redis.post` | Website Tools | POST | `/api/sites/redis` | Supported |
| `panel.sites.dns.get` | Website Tools | GET | `/api/sites/dns` | Supported |
| `panel.sites.dns.put` | Website Tools | PUT | `/api/sites/dns` | Supported |
| `panel.sites.resource_limits.get` | Website Tools | GET | `/api/sites/resource-limits` | Supported |
| `panel.sites.resource_limits.put` | Website Tools | PUT | `/api/sites/resource-limits` | Supported |
| `panel.sites.resource_limits.delete` | Website Tools | DELETE | `/api/sites/resource-limits` | Supported |
| `panel.sites.resource_limits.post` | Website Tools | POST | `/api/sites/resource-limits` | Supported |
| `panel.sites.domain.put` | Website Tools | PUT | `/api/sites/domain` | Supported |
| `panel.sites.delete` | Website Tools | DELETE | `/api/sites/{objectId}` | Supported |
| `panel.overview.jobs.get` | Overview | GET | `/api/overview/jobs` | Supported |
| `panel.backups.counts.get` | Backups | GET | `/api/backups/counts` | Supported |
| `panel.backups.restore_direct.get` | Backups | GET | `/api/backups/restore-direct` | Supported |
| `panel.backups.restore_direct.post` | Backups | POST | `/api/backups/restore-direct` | Supported |
| `panel.backups.restorable.get` | Backups | GET | `/api/backups/restorable` | Supported |
| `panel.backups.item.get` | Backups | GET | `/api/backups/item` | Supported |
| `panel.backups.item.delete` | Backups | DELETE | `/api/backups/item` | Supported |
| `panel.backups.item.post` | Backups | POST | `/api/backups/item` | Supported |
| `panel.backups.post` | Backups | POST | `/api/backups` | Supported |
| `panel.application_hosting.get` | Server Environment | GET | `/api/application-hosting` | Supported |
| `panel.application_hosting.post` | Server Environment | POST | `/api/application-hosting` | Supported |
| `panel.system_updates.get` | Updates | GET | `/api/system-updates` | Supported |
| `panel.system_updates.post` | Updates | POST | `/api/system-updates` | Supported |
| `panel.environment_inventory.get` | Server Environment | GET | `/api/environment-inventory` | Supported |
| `panel.analytics.processes.get` | Analytics | GET | `/api/analytics/processes` | Supported |
| `panel.analytics.usage.get` | Analytics | GET | `/api/analytics/usage` | Supported |
| `panel.analytics.disk_usage.get` | Analytics | GET | `/api/analytics/disk-usage` | Supported |
| `panel.website_ftp.get` | Website Tools | GET | `/api/website-ftp` | Supported |
| `panel.website_ftp.post` | Website Tools | POST | `/api/website-ftp` | Supported |
| `panel.website_ftp.put` | Website Tools | PUT | `/api/website-ftp` | Supported |
| `panel.website_ftp.delete` | Website Tools | DELETE | `/api/website-ftp` | Supported |
| `panel.website_cron.run.post` | Website Tools | POST | `/api/website-cron/run` | Supported |
| `panel.website_cron.get` | Website Tools | GET | `/api/website-cron` | Supported |
| `panel.website_cron.post` | Website Tools | POST | `/api/website-cron` | Supported |
| `panel.website_cron.put` | Website Tools | PUT | `/api/website-cron` | Supported |
| `panel.website_cron.delete` | Website Tools | DELETE | `/api/website-cron` | Supported |
| `panel.website_backups.get` | Backups | GET | `/api/website-backups` | Supported |
| `panel.website_backups.delete` | Backups | DELETE | `/api/website-backups` | Supported |
| `panel.website_backups.post` | Backups | POST | `/api/website-backups` | Supported |
| `panel.website_backups.upload.post` | Backups | POST | `/api/website-backups/upload` | Supported |
| `panel.website_backups.restore.post` | Backups | POST | `/api/website-backups/restore` | Supported |
| `panel.backup_destinations.post` | Backups | POST | `/api/backup-destinations` | Supported |
| `panel.backup_destinations.put` | Backups | PUT | `/api/backup-destinations` | Supported |
| `panel.backup_destinations.test.post` | Backups | POST | `/api/backup-destinations/test` | Supported |
| `panel.backup_destinations.storage.post` | Backups | POST | `/api/backup-destinations/storage` | Supported |
| `panel.backup_destinations.delete` | Backups | DELETE | `/api/backup-destinations/{objectId}` | Supported |
| `panel.files.get` | File Manager and Server File Explorer | GET | `/api/files` | Supported |
| `panel.files.delete` | File Manager and Server File Explorer | DELETE | `/api/files` | Supported |
| `panel.files.search.get` | File Manager and Server File Explorer | GET | `/api/files/search` | Supported |
| `panel.files.trash.get` | File Manager and Server File Explorer | GET | `/api/files/trash` | Supported |
| `panel.files.trash.post` | File Manager and Server File Explorer | POST | `/api/files/trash` | Supported |
| `panel.files.trash.delete` | File Manager and Server File Explorer | DELETE | `/api/files/trash` | Supported |
| `panel.migrations.post` | Migration | POST | `/api/migrations` | Supported |
| `panel.migrations.get` | Migration | GET | `/api/migrations` | Supported |
| `panel.databases.phpmyadmin.post` | Databases | POST | `/api/databases/phpmyadmin` | Supported |
| `panel.mail.webmail_configure.get` | Email | GET | `/api/mail/webmail-configure` | Supported |
| `panel.mail.webmail_configure.post` | Email | POST | `/api/mail/webmail-configure` | Supported |
| `panel.mail.webmail_login.post` | Email | POST | `/api/mail/webmail-login` | Supported |
| `panel.mail.delivery.get` | Email | GET | `/api/mail/delivery` | Supported |
| `panel.mail.smtp_port_check.post` | Email | POST | `/api/mail/smtp-port-check` | Supported |
| `panel.system_snapshots.settings.get` | System Snapshot | GET | `/api/system-snapshots/settings` | Supported |
| `panel.system_snapshots.settings.post` | System Snapshot | POST | `/api/system-snapshots/settings` | Supported |
| `panel.system_snapshots.remote.get` | System Snapshot | GET | `/api/system-snapshots/remote` | Supported |
| `panel.system_snapshots.remote.post` | System Snapshot | POST | `/api/system-snapshots/remote` | Supported |
| `panel.system_snapshots.get` | System Snapshot | GET | `/api/system-snapshots` | Supported |
| `panel.system_snapshots.post` | System Snapshot | POST | `/api/system-snapshots` | Supported |
| `panel.system_snapshots.upload.post` | System Snapshot | POST | `/api/system-snapshots/upload` | Supported |
| `panel.system_snapshots.inspect.post` | System Snapshot | POST | `/api/system-snapshots/inspect` | Supported |
| `panel.system_snapshots.restore.post` | System Snapshot | POST | `/api/system-snapshots/restore` | Supported |
| `panel.live_migrations.get` | Migration | GET | `/api/live-migrations` | Supported |
| `panel.live_migrations.post` | Migration | POST | `/api/live-migrations` | Supported |
| `panel.live_migrations.cancel.post` | Migration | POST | `/api/live-migrations/cancel` | Supported |
| `panel.live_migrations.download.get` | Migration | GET | `/api/live-migrations/download` | Supported |
| `panel.migrations.database.post` | Migration | POST | `/api/migrations/database` | Supported |
| `panel.migrations.import.post` | Migration | POST | `/api/migrations/import` | Supported |
| `panel.migrations.delete` | Migration | DELETE | `/api/migrations/{objectId}` | Supported |
| `panel.files.create.post` | File Manager and Server File Explorer | POST | `/api/files/create` | Supported |
| `panel.files.transfer.post` | File Manager and Server File Explorer | POST | `/api/files/transfer` | Supported |
| `panel.files.upload_url.post` | File Manager and Server File Explorer | POST | `/api/files/upload-url` | Supported |
| `panel.files.download_selected.get` | File Manager and Server File Explorer | GET | `/api/files/download-selected` | Supported |
| `panel.files.download_selected.post` | File Manager and Server File Explorer | POST | `/api/files/download-selected` | Supported |
| `panel.files.content.get` | File Manager and Server File Explorer | GET | `/api/files/content` | Supported |
| `panel.files.content.put` | File Manager and Server File Explorer | PUT | `/api/files/content` | Supported |
| `panel.files.encodings.get` | File Manager and Server File Explorer | GET | `/api/files/encodings` | Supported |
| `panel.files.upload.post` | File Manager and Server File Explorer | POST | `/api/files/upload` | Supported |
| `panel.files.directory.post` | File Manager and Server File Explorer | POST | `/api/files/directory` | Supported |
| `panel.files.rename.post` | File Manager and Server File Explorer | POST | `/api/files/rename` | Supported |
| `panel.files.permissions.put` | File Manager and Server File Explorer | PUT | `/api/files/permissions` | Supported |
| `panel.files.archive.post` | File Manager and Server File Explorer | POST | `/api/files/archive` | Supported |
| `panel.files.extract.post` | File Manager and Server File Explorer | POST | `/api/files/extract` | Supported |
| `panel.files.download.get` | File Manager and Server File Explorer | GET | `/api/files/download` | Supported |
| `panel.databases.post` | Databases | POST | `/api/databases` | Supported |
| `panel.databases.sizes.get` | Databases | GET | `/api/databases/sizes` | Supported |
| `panel.databases.password.put` | Settings | PUT | `/api/databases/password` | Supported |
| `panel.databases.delete` | Databases | DELETE | `/api/databases/{objectId}` | Supported |
| `panel.firewall.apply_litecp.post` | Security | POST | `/api/firewall/apply-litecp` | Supported |
| `panel.firewall.apply_litecp.get` | Security | GET | `/api/firewall/apply-litecp` | Supported |
| `panel.firewall.apply_litecp.delete` | Security | DELETE | `/api/firewall/apply-litecp` | Supported |
| `panel.backup_destinations.host_key.post` | Backups | POST | `/api/backup-destinations/host-key` | Supported |
| `panel.firewall.post` | Security | POST | `/api/firewall` | Supported |
| `panel.firewall.delete` | Security | DELETE | `/api/firewall/{objectId}` | Supported |
| `panel.firewall.put` | Security | PUT | `/api/firewall/{objectId}` | Supported |
| `panel.mail.hosting.put` | Email | PUT | `/api/mail/hosting` | Supported |
| `panel.mail.dns.get` | Email | GET | `/api/mail/dns` | Supported |
| `panel.mail.domains.post` | Email | POST | `/api/mail/domains` | Supported |
| `panel.mail.domains.delete` | Email | DELETE | `/api/mail/domains/{objectId}` | Supported |
| `panel.mail.mailboxes.post` | Email | POST | `/api/mail/mailboxes` | Supported |
| `panel.mail.mailboxes.password.put` | Email | PUT | `/api/mail/mailboxes/password` | Supported |
| `panel.mail.mailboxes.delete` | Email | DELETE | `/api/mail/mailboxes/{objectId}` | Supported |
| `panel.mail.aliases.post` | Email | POST | `/api/mail/aliases` | Supported |
| `panel.terminal.post` | Terminal | POST | `/api/terminal` | Unavailable: SSH required |
| `panel.terminal.session.post` | Terminal | POST | `/api/terminal/session` | Unavailable: SSH required |
| `panel.terminal.session.get` | Terminal | GET | `/api/terminal/session` | Unavailable: SSH required |
| `panel.terminal.session.delete` | Terminal | DELETE | `/api/terminal/session` | Unavailable: SSH required |
| `panel.shell_capabilities.get` | Terminal | GET | `/api/shell-capabilities` | Supported |
| `panel.security.ssh_port.get` | Security | GET | `/api/security/ssh-port` | Supported |
| `panel.security.ssh_port.put` | Security | PUT | `/api/security/ssh-port` | Supported |
| `panel.security.brute_force.get` | Security | GET | `/api/security/brute-force` | Supported |
| `panel.security.brute_force.put` | Security | PUT | `/api/security/brute-force` | Supported |
| `panel.security.panel_port.get` | Security | GET | `/api/security/panel-port` | Supported |
| `panel.security.panel_port.put` | Security | PUT | `/api/security/panel-port` | Supported |
| `panel.mail.aliases.put` | Email | PUT | `/api/mail/aliases/{objectId}` | Supported |
| `panel.mail.aliases.delete` | Email | DELETE | `/api/mail/aliases/{objectId}` | Supported |
| `panel.logs.get` | Logs | GET | `/api/logs` | Supported |
| `panel.settings.put` | Settings | PUT | `/api/settings` | Supported |
| `panel.password.put` | Settings | PUT | `/api/password` | Supported |
| `panel.updates.install.post` | Updates | POST | `/api/updates/install` | Supported |
| `panel.developer_mode.get` | Server | GET | `/api/developer-mode` | Supported |
| `panel.developer_mode.put` | Server | PUT | `/api/developer-mode` | Supported |
| `panel.updates.status.get` | Updates | GET | `/api/updates/status` | Supported |
| `panel.updates.get` | Updates | GET | `/api/updates` | Supported |
| `panel.version.get` | Server | GET | `/api/version` | Supported |

| `panel.security.captcha.get` | Security | GET | `/api/security/captcha` | Supported |
| `panel.security.captcha.put` | Security | PUT | `/api/security/captcha` | Supported |
