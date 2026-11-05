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
navigation/action/form tests. Live Redis service acceptance remains outstanding:
the test server did not have Redis/Valkey server and CLI packages installed.
