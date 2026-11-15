# Automatic website quota storage

Starting with 4.0.86, the Ubuntu/AlmaLinux installer installs quota, e2fsprogs,
xfsprogs and util-linux. It checks project accounting and enforcement before
reporting quota readiness.

On a fresh installation (no /var/lib/litecp/config.json), if /home/litecp is empty
and is not already a mount, it creates a sparse regular storage file at
/var/lib/litecp/website-storage.ext4. Only that new file is formatted, using ext4
project/quota features. It is mounted at /home/litecp with
loop,prjquota,nodev,nosuid,discard.
The root filesystem and existing block devices are never formatted or converted.

Default size: half the available space, rounded down to whole GiB, minimum 2 GiB.
At least 4 GiB must remain free. Set LITECP_WEBSITE_STORAGE_GIB before running the
installer to select another whole-GiB size. This is a maximum logical capacity,
not an upfront reservation: the image consumes physical space as data is written.
Filesystem metadata still consumes some space and reduces usable website capacity.
The OS, databases, snapshots and website image share host free space; other host
writes can exhaust that space before the website volume reaches its own capacity.
Keep host disk alerts enabled and monitor both filesystems.
Local website backups and Trash consume
this website volume; databases and system snapshots normally consume host storage.

On supported loop devices and backing filesystems, the discard mount option
releases blocks freed inside the website volume back to the host. The installer
tests FITRIM only on the newly created, empty mount and reports a warning if
the discard request is not accepted. It does not claim measured host-space savings.
Sparse creation does not depend on successful
discard, but automatic release of subsequently freed blocks does.

Starting with 6.0.53, fresh installations also configure the OS-provided
fstrim.timer after website storage preparation. Trimming the loop filesystem
releases holes in the image; the outer filesystem must also discard its freed
blocks before a cloud provider can reclaim them. The installer checks the root
filesystem, performs an initial root trim, enables the existing timer and verifies
that it is enabled and active. It checks both the timer and its service before
activation and never unmasks or replaces them. Vendor scheduling and administrator
overrides are retained; the usual vendor schedule is weekly.

Read-only root storage, an explicit root X-fstrim.notrim option, missing tools or
units, unsupported discard, and timer activation failures produce visible storage
warnings. Installation can continue, but automatic reclamation is not reported as
verified. These checks do not format storage, resize images, delete files or
change root mount options. Other filesystems covered by the vendor service are
handled according to its configuration. Existing installations are not trimmed
or opted into a new maintenance policy by rerunning the installer or updating the
panel.

FITRIM success means the discard request was accepted; its byte count is not a
measurement of recovered provider storage. Snapshot size also depends on provider
behavior and compression. Freed blocks can accumulate between scheduled runs;
an administrator can run `fstrim -v /` before a snapshot on supported storage.
Existing cloud snapshots are unchanged by trimming the running server.

Existing images are never truncated, reformatted, remounted or trimmed by this
change, including when the installer is rerun. Removing website resource rules
does not remove the website filesystem. Reclaiming unused space in an older
fully allocated image requires a separate reviewed maintenance operation after
a backup; never delete the image while it contains website data.

The installer preserves a uniquely named /etc/fstab.litecp-quota.* backup before
adding the mount entry. The panel service requires /home/litecp to be mounted.
No nofail option is used: failure to mount must not silently put website writes
on an unprotected underlying directory. Failed setup retains the image and backup
for review; it does not delete or overwrite them on retry.

Existing installations and nonempty website directories are preserved. Already
compatible XFS/ext4 storage is reused. Unsupported existing storage reports quotas
unavailable rather than attempting root-filesystem changes, rebooting or moving data.

Read-only readiness check:

    sudo bash /usr/local/lib/litecp/prepare-website-quotas.sh --check

Mocked Linux regression checks:

    bash scripts/test-website-quotas.sh
    bash scripts/test-storage-trim.sh

This prepares filesystem support only; resource enforcement is enabled separately
per website. No live server storage migration is performed by updating panel source.
