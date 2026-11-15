#!/usr/bin/env bash
set -Eeuo pipefail
source "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/prepare-website-quotas.sh"
# Only create new files in a unique test directory. Retain them for inspection.
TEST_DIR="$(mktemp -d "${TMPDIR:-/tmp}/litecp-quota-tests.XXXXXXXX")"
TEST_FORMAT_CALLS=0 TEST_FORMAT_EXIT=0
mkfs.ext4() {
  TEST_FORMAT_CALLS=$((TEST_FORMAT_CALLS + 1))
  [[ "$*" == "-q -O project,quota -E quotatype=prjquota,nodiscard -m 0 $TEST_DIR/"* ]] || return 91
  return "$TEST_FORMAT_EXIT"
}
quota_create_sparse_image "$TEST_DIR/new.ext4" 67108864
[[ "$(stat -c %s "$TEST_DIR/new.ext4")" == 67108864 ]] || { echo 'FAIL: wrong sparse image capacity'; exit 1; }
[[ "$(stat -c %a "$TEST_DIR/new.ext4")" == 600 ]] || { echo 'FAIL: image permissions are not private'; exit 1; }
(( $(stat -c %b "$TEST_DIR/new.ext4") * 512 < 1048576 )) || { echo 'FAIL: image capacity was allocated upfront'; exit 1; }
[[ "$TEST_FORMAT_CALLS" == 1 ]] || { echo 'FAIL: new image was not formatted exactly once'; exit 1; }
printf 'preserve-existing-data' > "$TEST_DIR/existing.ext4"
if quota_create_sparse_image "$TEST_DIR/existing.ext4" 67108864; then echo 'FAIL: existing image accepted'; exit 1; fi
[[ "$(cat "$TEST_DIR/existing.ext4")" == preserve-existing-data && "$TEST_FORMAT_CALLS" == 1 ]] || { echo 'FAIL: existing image was changed'; exit 1; }
ln -s "$TEST_DIR/existing.ext4" "$TEST_DIR/link.ext4"
if quota_create_sparse_image "$TEST_DIR/link.ext4" 67108864; then echo 'FAIL: symlink accepted'; exit 1; fi
[[ "$(cat "$TEST_DIR/existing.ext4")" == preserve-existing-data && "$TEST_FORMAT_CALLS" == 1 ]] || { echo 'FAIL: symlink target was changed'; exit 1; }
truncate() { return 1; }
if quota_create_sparse_image "$TEST_DIR/failed-size.ext4" 67108864; then echo 'FAIL: sizing failure accepted'; exit 1; fi
[[ "$TEST_FORMAT_CALLS" == 1 && -f "$TEST_DIR/failed-size.ext4" ]] || { echo 'FAIL: sizing failure formatted or removed data'; exit 1; }
unset -f truncate
TEST_FORMAT_EXIT=1
if quota_create_sparse_image "$TEST_DIR/failed-format.ext4" 67108864; then echo 'FAIL: formatting failure accepted'; exit 1; fi
[[ -f "$TEST_DIR/failed-format.ext4" ]] || { echo 'FAIL: failed image was removed'; exit 1; }
TEST_TRIM_EXIT=0
fstrim() { [[ "$*" == '-v -- /home/litecp' ]] || return 91; printf 'test discard result\n'; return "$TEST_TRIM_EXIT"; }
TEST_TRIM_OUTPUT="$(quota_check_fresh_reclaim /home/litecp)"
[[ "$TEST_TRIM_OUTPUT" == *'discard accepted'* ]] || { echo 'FAIL: accepted trim not reported'; exit 1; }
TEST_TRIM_EXIT=1
TEST_TRIM_OUTPUT="$(quota_check_fresh_reclaim /home/litecp)"
[[ "$TEST_TRIM_OUTPUT" == *'WARNING:'* && "$TEST_TRIM_OUTPUT" != *'discard accepted'* ]] || { echo 'FAIL: failed trim reported ready'; exit 1; }
printf 'PASS: sparse creation, existing image/symlink preservation, failure handling and trim diagnostics (files retained: %s)\n' "$TEST_DIR"
TEST_KIND=ext4 TEST_OPTIONS=rw,prjquota TEST_ACCOUNTING=on TEST_QUOTA_EXIT=1
findmnt() {
  case "${*: -1}" in
    FSTYPE) printf '%s\n' "$TEST_KIND";;
    OPTIONS) printf '%s\n' "$TEST_OPTIONS";;
    TARGET) printf '/home/litecp\n';;
    *) return 1;;
  esac
}
quotaon() { printf 'project quota on /home/litecp (/dev/test) is %s\n' "$TEST_ACCOUNTING"; return "$TEST_QUOTA_EXIT"; }
xfs_quota() { printf 'Accounting: ON\nEnforcement: %s\n' "$TEST_ACCOUNTING"; }
quota_verify /home/litecp || { echo 'FAIL: ext4 active quota rejected'; exit 1; }
TEST_QUOTA_EXIT=0
quota_verify /home/litecp || { echo 'FAIL: ext4 active quota with zero exit rejected'; exit 1; }
TEST_QUOTA_EXIT=2
if quota_verify /home/litecp; then echo 'FAIL: quota command failure accepted'; exit 1; fi
TEST_QUOTA_EXIT=0
TEST_ACCOUNTING=off
if quota_verify /home/litecp; then echo 'FAIL: disabled ext4 quota accepted'; exit 1; fi
TEST_QUOTA_EXIT=1
if quota_verify /home/litecp; then echo 'FAIL: nonzero exit mistaken for active quota'; exit 1; fi
TEST_ACCOUNTING=unknown
if quota_verify /home/litecp; then echo 'FAIL: unknown ext4 quota state accepted'; exit 1; fi
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
