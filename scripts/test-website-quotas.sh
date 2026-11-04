#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/prepare-website-quotas.sh"
TEST_KIND=ext4 TEST_OPTIONS=rw,prjquota TEST_ACCOUNTING=on
findmnt() {
  case "${*: -1}" in
    FSTYPE) printf '%s\n' "$TEST_KIND";;
    OPTIONS) printf '%s\n' "$TEST_OPTIONS";;
    TARGET) printf '/home/litecp\n';;
    *) return 1;;
  esac
}
quotaon() { printf 'project quota on /home/litecp (/dev/test) is %s\n' "$TEST_ACCOUNTING"; }
xfs_quota() { printf 'Accounting: ON\nEnforcement: %s\n' "$TEST_ACCOUNTING"; }
quota_verify /home/litecp || { echo 'FAIL: ext4 active quota rejected'; exit 1; }
TEST_ACCOUNTING=off
if quota_verify /home/litecp; then echo 'FAIL: disabled ext4 quota accepted'; exit 1; fi
TEST_ACCOUNTING=on TEST_OPTIONS=rw,usrquota
if quota_verify /home/litecp; then echo 'FAIL: user quota mistaken for project quota'; exit 1; fi
TEST_KIND=xfs TEST_OPTIONS=rw,pquota TEST_ACCOUNTING=ON
quota_verify /home/litecp || { echo 'FAIL: XFS active quota rejected'; exit 1; }
TEST_ACCOUNTING=OFF
if quota_verify /home/litecp; then echo 'FAIL: XFS accounting alone accepted'; exit 1; fi
TEST_KIND=btrfs TEST_OPTIONS=rw,prjquota
if quota_verify /home/litecp; then echo 'FAIL: unsupported filesystem accepted'; exit 1; fi
quota_verify() { return 1; }
quota_check() { return 2; }
mountpoint() { return 0; }
mkfs.ext4() { echo 'FAIL: attempted to format existing storage'; exit 1; }
if quota_prepare_fresh /home/litecp; then echo 'FAIL: existing incompatible mount reported ready'; exit 1; else [[ "$?" == 2 ]]; fi
mountpoint() { return 1; }
find() { printf '/home/litecp/existing-website\n'; }
if quota_prepare_fresh /home/litecp; then echo 'FAIL: nonempty storage reported ready'; exit 1; else [[ "$?" == 2 ]]; fi
echo 'PASS: ext4/XFS quota readiness, missing project option, disabled enforcement and unsupported filesystem'
echo 'PASS: existing mounts and nonempty website storage are never formatted'
