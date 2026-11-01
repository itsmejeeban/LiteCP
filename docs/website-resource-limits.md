# Website resource limits: development status

This is an incomplete implementation, not a production isolation feature.
Activation is deliberately rejected by the server. Disabled drafts are stored
with each site and automatically included wherever the complete Site is serialized.
No service, mount, quota, firewall or existing website is changed by saving a draft.

Implemented: validated settings, authenticated API, Linux/cgroup and mount capability
inspection, collision-free immutable slice naming, cgroup counter reader, deterministic
CPU/OOM/disk/process policy tests, and a Website Tools draft configuration page.

Create resource limit opens the configuration form for any managed website.
Saved configurations appear in a paginated list below it. While the page is visible,
observed website-UID CPU, summed RSS memory and process counts refresh every three
seconds; bounded disk measurements refresh every minute. CPU needs two samples.
Unavailable measurements are identified rather than displayed as zero. Shared
services and container processes with remapped UIDs are excluded. These observations
are not evidence of resource enforcement; saved configurations remain Disabled.

Still required before activation can be offered:

* A privileged execution boundary with durable per-site transition locks.
* Persistent systemd slices, dedicated FPM masters and PHP 7.3 compatibility containers.
* Dedicated local static workers and transactional HTTPS/WAF frontend proxy switching.
* App/setup/container and cron placement in each website slice; blocked-run guards.
* Real project-quota allocation, recursive assignment, enforcement verification and quota usage reads.
* Independent monitor service, persisted runtime states/history/cooldowns and single-flight recovery.
* Transactional rollback, PHP-version/domain lifecycle integration and restore revalidation.
* Fully isolated cgroup usage/history/retry APIs and enabled controls, once the executor is implemented.
* Supported-Linux stress and cross-site isolation tests from the approved acceptance plan.

The policy evaluator enforces a ten-second consecutive CPU exhaustion criterion,
five-minute recovery intervals, a strict below-95-percent disk recovery threshold,
and process-limit notification without suspending other workloads. It currently has
no service-stopping executor. No manual-suspension state is modified.

Run focused tests using `go test -run TestResource .`. The existing Makefile remains
the build entry point. This change is local; use the existing updater only after review.
