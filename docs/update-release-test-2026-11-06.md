# Release update acceptance — 2026-11-06

## Scope and result

The repository was temporarily made public as requested, then verified private again. Stable releases were missing initially. Release 5.0.5 was published from commit `7dc987b` and installed successfully on the destination test server, upgrading 5.0.2.

This was a server-side update-worker test over verified SSH, not a browser-button acceptance test. The installed 5.0.2 worker first reproduced the failure. A patched isolated worker was used to bootstrap the repaired release because the old embedded worker cannot fix itself before building.

## Bugs found and repaired

- Transient services lack an interactive HOME/GOPATH. Go could not locate its module cache. Each update now defines private GOPATH, GOMODCACHE and GOCACHE paths inside its retained work directory.
- Sandbox preflight used `systemd-run --pipe`, passing background-service journal descriptors into another unit. This failed with “Remote peer disconnected.” The probe now uses systemd-managed output while retaining its UID, capability, NoNewPrivileges and private-file access assertions. Direct and nested-service probes were verified.
- GitHub 404 responses were incorrectly treated as successful release checks. They now report inaccessible release status, covering private repositories and missing stable releases.
- Browser fetch failures now explain panel connection/certificate checks rather than showing only “Failed to fetch.”

## Verification

- 13 targeted JavaScript checks passed.
- Linux Go release-check regression tests and worker shell syntax validation passed.
- Actual release download, compilation, service preparation, panel restart and post-install health checks succeeded.
- Installed API version: 5.0.5; build: 7dc987b.
- Panel, broker, Nginx, MySQL, Postfix and Dovecot active; SELinux Enforcing.
- All ten enabled application runtimes returned HTTP 200 on their local ports.
- Eleven website records retained; 44 non-log website file hashes matched the source snapshot. Runtime logs were excluded because requests append to them.
- Test database login and marker row verified. Test mailbox login, message checksum and 100 MiB quota verified.
- Previous binaries, update work directories and recovery data retained. No website/database/backup deletion was performed.

## Remaining release-channel limitation

Private repository visibility blocks the current unauthenticated GitHub release/download endpoints. Returning the repository to private therefore also prevents future unauthenticated automatic updates. Production private updates need a dedicated authenticated release channel or a separate public release repository. No workstation GitHub credential was installed on the server.

The existing Makefile remains the build entry point. The real update built on Linux; no local distributable package or ZIP was generated.
