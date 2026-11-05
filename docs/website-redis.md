# Website Redis cache

Website Tools > Redis and the Redis action on each Websites row open the same
website-specific manager. Redis is disabled by default. Settings are serialized
with Site records; socket names use immutable website IDs.

This release is cache-only: no RDB/AOF persistence, allkeys-lru eviction, no public
TCP port, a private UNIX socket, dedicated website UID, and a separate systemd unit.
Native PHP/static sites are supported. Application containers and PHP 7.3 require
socket-mount integration and are explicitly rejected when enabling.

Install a distribution-supported Redis or Valkey server and matching CLI first.
The manager detects either executable pair. It does not change a shared mail Redis
service. Install the PHP Redis extension separately and configure the application
explicitly; no .env, WordPress files or database credentials are overwritten.

Memory setting limits the cache dataset. The service adds 64 MiB of overhead and
has a hard systemd memory cap. With Resource Limit active, Redis enters the website
slice and is restarted by its recovery monitor. Disable Redis before changing
resource enforcement. Cache-only restarts may discard cache data. Do not use this
instance for sessions, durable queues or primary data.

The UI provides save/disable, restart, private socket details and five-second
metrics polling while visible. API mutations are authenticated and broker-allowed.
Failed starts preserve previous service/configuration where available and report
rollback errors. No cache flush or data deletion endpoint is exposed.

Verification: focused Go configuration/validation/serialization tests and JavaScript
navigation/action/form tests. The initial test server lacked server/CLI packages. See the live audit below for
subsequent installation and acceptance results.

## Live audit, 2026-11-05 / 4.0.136 candidate

The installed 4.0.135 UI exposed startup failures on the audit website. Fixed:
numeric website UIDs absent from NSS, executor umask affecting directory modes,
systemd runtime-directory ownership resetting between execution steps, missing
PHP open_basedir socket allowlist, Redis action loss after website table redraw,
and background polling hiding operation errors. Cache users cannot modify CONFIG,
ACL, modules, replication or persistence settings. Valkey version is shown directly.

Installed AlmaLinux AppStream valkey 8.0.11 for the authorized live test; its shared
service remained inactive. Corrected candidate tests passed PING, SET/GET, 48 MiB
bounded insertion into a 32 MiB cache (24-25 evicted keys), another website UID denied
socket access, non-root process UID, restart and no TCP 6379 listener. The audit
instance was then disabled. Both audit and comparison websites returned HTTP 200;
panel, Nginx and MySQL remained active.

PHP-FPM access is NOT certified yet. After the scoped open_basedir fix, PHP still
reported permission denied. SELinux httpd_can_network_redis is off. Automatic
approval review rejected changing that global boolean without specific approval;
it was not changed. The manager now reports this prerequisite and refuses new
enablement while it is known to be absent. Approval was requested separately.
The corrected panel binary has not been deployed. Tests used a standalone candidate.

## Approved SELinux integration completed

The administrator explicitly approved enabling httpd_can_network_redis. It is now
persistently enabled; SELinux remains Enforcing. Policy inspection confirmed PHP
httpd_t may connect to redis_t, but not the previous unconfined_service_t worker.
The final service uses DynamicUser=yes with the existing numeric website UID and
executes the vendor binary directly, entering redis_t without creating login users,
root Redis processes, custom allow-all modules or disabling SELinux.

Final 4.0.136 candidate acceptance passed:

- Actual PHP-FPM HTTP request: PING -> PONG, SET -> OK, GET -> php-live.
- Redis process UID matched the website and its SELinux domain was redis_t.
- Cache-only PING/SET/GET, bounded 48 MiB insertion against 32 MiB maxmemory,
  25 evicted keys, forbidden CONFIG and cross-website socket access rejected.
- Restart passed, TCP 6379 remained closed, both websites returned HTTP 200.
- Audit cache disabled afterward; persistent panel settings were preserved.

The installed panel executable was not replaced. Deploy local 4.0.136 using the
existing update entry point to retain the startup, PHP allowlist and UI repairs.
The approved SELinux boolean and distribution Valkey package remain installed.
