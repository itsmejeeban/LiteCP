# Automatic website quota storage

Starting with 4.0.86, the Ubuntu/AlmaLinux installer installs quota, e2fsprogs,
xfsprogs and util-linux. It checks project accounting and enforcement before
reporting quota readiness.

On a fresh installation (no /var/lib/litecp/config.json), if /home/litecp is empty
and is not already a mount, it creates a newly allocated regular storage file at
/var/lib/litecp/website-storage.ext4. Only that new file is formatted, using ext4
project/quota features. It is mounted at /home/litecp with loop,prjquota,nodev,nosuid.
The root filesystem and existing block devices are never formatted or converted.

Default size: half the available space, rounded down to whole GiB, minimum 2 GiB.
At least 4 GiB must remain free. Set LITECP_WEBSITE_STORAGE_GIB before running the
installer to select another whole-GiB size. The file is fully allocated; filesystem
metadata reduces usable website space. Local website backups and Trash consume
this website volume; databases and system snapshots normally consume host storage.

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

This prepares filesystem support only. It does not complete or activate the
unfinished per-website enforcement runtime. No live server storage migration is
performed by updating panel source.
