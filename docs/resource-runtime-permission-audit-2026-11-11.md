# Resource runtime permission repair (6.0.29)

The host command executor uses umask 0077. Creating runtime directories with
Mkdir/MkdirAll(0755) therefore produced root-only 0700 directories. Nginx could
accept the root-run health client's connection but could not traverse the same
directory to reach PHP-FPM, producing HTTP 502 with Permission denied.

The repair explicitly sets traversal permissions on validated root-owned runtime
parents. It repairs existing 0700 directories as well as first activation,
rejects symlinks and unsafe writable directories, and preserves private plans,
recovery copies and PHP temporary storage. Nginx buffering directories receive
their explicit intended permissions too. Private-worker errors inherit stderr.

The resource monitor now uses systemd notify readiness. Activation completes
only after monitor startup validation and gate handling; the activating process
no longer races the monitor by reopening the access gate itself.

## Verification

- Focused local checks and the Linux TestResource suite passed.
- Linux tests exercised umask 0077, existing-directory repair, unsafe directory
  rejection, preserved private contents and an unprivileged Unix-socket client.
- Installed Nginx and PHP-FPM parsers accepted the generated configuration.
- A new .invalid test website reproduced the installed 6.0.28 permission failure.
- The patched candidate activated the same isolated boundary successfully.
- Immediate public PHP execution and a buffered 1 MiB request passed.
- Two further disable/enable cycles passed immediate PHP requests.
- Project-quota and aggregate cgroup limit verification passed.
- The 11 existing hosted websites retained their previous HTTP responses. Panel,
  broker, shared Nginx and shared PHP master process IDs were unchanged.

This live run used a separate server with SELinux Permissive. It did not repeat
CPU/RAM/disk exhaustion tests or change SELinux policy. No installed panel binary,
customer website data or saved customer resource policies were replaced.

## Existing enabled policies

Updating the panel does not restart already-running website monitor processes
or regenerate their existing worker configuration. Retry failed activations
after updating. Existing enabled policies created by older releases should be
disabled and re-enabled individually to refresh their workers, retaining their
saved allowance values. Bulk enable can skip already-enabled policies.
