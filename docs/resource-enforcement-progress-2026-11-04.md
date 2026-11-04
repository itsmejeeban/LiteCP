# Resource enforcement implementation checkpoint

Status: incomplete; not deployed or enabled. Keep the activation guard in place.

## Verified server facts

- 162.55.50.122, website homes on /dev/sda1, ext4, mounted at /.
- Filesystem UUID 05566eb3-58d8-48f7-8176-083180510495.
- tune2fs feature list has neither project nor quota.
- No quota or setquota executable found; xfs_quota is installed.
- User authorized automatic quota setup and offline maintenance, and will open the provider console. No provider/rescue console appeared in Chrome during this turn. No reboot, fstab or filesystem-feature changes performed.

## Code added

resource_slice.go contains aggregate cgroup property/unit generation, kernel verification, and ordered block/recovery actions. resource_slice_test.go tests these primitives, including missing controllers, unlimited/mismatched kernel values, manual suspension, cooldown preservation and failed health checks.

These helpers are not connected to website activation. Dedicated native/PHP 7.3 workers, static-worker routing, container/setup and cron placement, quota assignment, persistence/monitor service, lifecycle reconciliation and the enable/retry UI still require integration. Do not label this feature ready or remove the activation guard based on these component tests.

## Live component tests

- 12-second disposable systemd service: CPUQuota 25%, MemoryMax 64 MiB, MemorySwapMax 0, TasksMax 16. Kernel cpu.max read 25000 100000; nr_throttled 19 of 20 periods. LiteCP, Nginx and MySQL stayed active.
- Bounded memory test requested at most 96 MiB under a 64 MiB cap. Result oom-kill. Other three services stayed active. Used OOMPolicy=kill; MemoryOOMGroup is not a supported systemd unit property on this host.
- Test workloads self-terminated. No website files or databases were altered. The failed QA unit/journal metadata was retained.

## Required continuation

Access the provider rescue console before filesystem maintenance. Identify the root volume by UUID rather than assuming rescue device names. Preserve configuration backups, confirm the volume is unmounted, inspect filesystem health without automatic repair, then enable ext4 project/quota features and persistent mount options following the installed e2fsprogs documentation. Never format or run forced repair. Validate project accounting and actual quota rejection after boot, then complete runtime integration and website-level acceptance tests.

Existing Makefile remains the build entry point; no build, packaging or deployment was run.
