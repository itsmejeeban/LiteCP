# Unprivileged web service and local management broker

From 4.0.96, fresh installs and both existing update entry points install:

| Unit | Identity | Responsibility |
| --- | --- | --- |
| `litecp.service` | `litecp-web`, no capabilities | Embedded UI, loopback HTTP listener, bounded control transport |
| `litecp-broker.socket` | root-owned, mode 0660, group `litecp-web` | Local Unix socket, no TCP listener |
| `litecp-broker.service` | root | Existing authenticated management operations and background jobs |

The dedicated system account has no login shell, home directory, supplementary
groups, sudo grant, or container socket access. The web service cannot read the
panel's private configuration, secret keys, website homes, mailbox storage or
backups through its filesystem namespace. Its capability bounding and ambient
sets are empty, and `NoNewPrivileges` is set. Startup refuses root and nonzero
effective capabilities. The loopback bind requirement is checked at runtime.

## Control boundary

The broker is launched through systemd socket activation and validates the
kernel-provided Unix peer UID against the dedicated service account. The web
service independently verifies that its broker peer is root. A fixed manifest,
`deploy/broker-operations.json`, permits registered control operations; new routes
require an explicit manifest entry. The protocol has bounded JSON metadata and
framed streaming bodies. It accepts neither executable names nor generic file
write primitives. Uploads/downloads do not buffer entire archives. Headers,
cookies, query parameters, response status and streaming progress are retained.

Existing operation handlers remain in the local broker, including authentication,
2FA, session checks, origin/CSRF validation, validation of resource ownership,
serialization of mutations and background scheduling. A connection from the web
UID does **not** bypass administrator authentication. The unprivileged process
does not receive the stored administrator hash or secret encryption key through
its filesystem; authorized management APIs remain governed by the administrator's
existing server-management permissions.

The browser terminal endpoints are refused in separated mode. Use SSH for server
shell access. Reintroducing a root browser terminal or an unrestricted command
broker would defeat this boundary. Root maintenance CLI commands remain available
to administrators through SSH.

This separates the network listener from host privileges. It is **not** a claim
that all privileged operations now have individual least-privilege sandboxes:
the broker still contains substantial management logic and runs as root. A bug
in an admitted privileged operation can still affect the host. Further broker
review and per-operation decomposition remain necessary. The dashboard discloses
this distinction rather than treating the system as completely secure.

## Installation and updates

`scripts/install-privilege-services.sh` creates or validates the dedicated account,
preserves existing service units, installs the three units, and checks the effective
web service user. It creates a separate frontend environment containing only the
bind value; private broker environment values are not copied to the web process.
It never recursively changes website/data ownership or modifies sudoers.

The normal installer, `update.bat` deployment and in-panel updater call this script.
Both update paths retain the previous binary and service units and restore them
together on failure. Restarting/stopping `litecp` also restarts/stops its broker
through systemd dependency relationships, preserving maintenance CLI expectations.
The DNS installer targets the broker's service override rather than giving the web
service write access to DNS configuration.

Installation/update success also requires `scripts/check-privilege-services.sh`
to observe the real web PID running as the dedicated UID with zero effective
capabilities and `NoNewPrivileges`, both services/socket active, and the expected
version responding through the loopback frontend. It reads the configured bind
without executing environment-file contents.

The service installer preserves recovery files. It does not delete websites,
databases, backups, or old deliverables. Existing custom systemd overrides should
be reviewed; an override that changes the effective web user makes installation
fail rather than silently continuing as root.

## Acceptance

Run focused tests with `go test ./... -run 'TestBroker|TestNetworkWeb|TestSecurityPosture|TestSecurityDashboard'`.
Installation configuration checks: `node --test scripts/privilege-install.test.cjs`.
The existing Makefile remains the build entry point; packaging is not automatic.

Before production acceptance on each supported distribution, verify the effective
service UIDs/capabilities, process tree and sockets; sign in and exercise website,
file, database, mail, backup/restore, migration and update workflows. Test socket
access from unrelated UIDs, unauthenticated operations, malformed/oversized frames,
streaming uploads/downloads, shutdown and update rollback. Root web startup must
fail closed. Keep SELinux enforcing and inspect denials without adding allow-all
policy modules. Source/component tests alone are not full fresh-install proof.
