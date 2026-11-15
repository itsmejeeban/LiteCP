#!/usr/bin/env bash
set -Eeuo pipefail

# Mock all host operations: these tests never trim a disk or change a service.
source "${BASH_SOURCE[0]%/*}/prepare-storage-trim.sh"

TEST_CASES=0
TEST_OUTPUT=''
TEST_STATUS=0

test_fail() { printf 'FAIL: %s\n%s\n' "$1" "$TEST_OUTPUT" >&2; exit 1; }
test_reset() {
  TEST_EXISTING=0 TEST_MISSING_TOOL=''
  TEST_ROOT_OPTIONS=rw,relatime TEST_FSTAB_OPTIONS=defaults
  TEST_ROOT_FIND_EXIT=0 TEST_FSTAB_FIND_EXIT=0
  TEST_TIMER_STATE=loaded TEST_SERVICE_STATE=loaded
  TEST_TRIM_EXIT=0 TEST_ENABLE_EXIT=0 TEST_ENABLED_EXIT=0 TEST_ACTIVE_EXIT=0
}

# FD 3 stays outside each command substitution in the implementation, so the
# captured events verify operation order without creating trace files.
command() {
  if [[ "${1:-}" == -v && "${2:-}" == "$TEST_MISSING_TOOL" ]]; then return 1; fi
  builtin command "$@"
}
storage_trim_existing_installation() { [[ "$TEST_EXISTING" == 1 ]]; }
findmnt() {
  printf 'EVENT findmnt %s\n' "$*" >&3
  case "$*" in
    '-n -r -o OPTIONS --mountpoint /') printf '%s\n' "$TEST_ROOT_OPTIONS"; return "$TEST_ROOT_FIND_EXIT";;
    '--fstab -n -r -o OPTIONS --mountpoint /') printf '%s\n' "$TEST_FSTAB_OPTIONS"; return "$TEST_FSTAB_FIND_EXIT";;
    *) printf 'UNEXPECTED MOCK findmnt %s\n' "$*" >&3; return 99;;
  esac
}
fstrim() {
  printf 'EVENT fstrim %s\n' "$*" >&3
  [[ "$*" == '-v -- /' ]] || { printf 'UNEXPECTED MOCK fstrim\n' >&3; return 99; }
  if [[ "$TEST_TRIM_EXIT" == 0 ]]; then
    printf '/: 1 GiB (1073741824 bytes) trimmed\n'
  else
    printf 'fstrim: /: the discard operation is not supported\n' >&2
  fi
  return "$TEST_TRIM_EXIT"
}
systemctl() {
  printf 'EVENT systemctl %s\n' "$*" >&3
  case "$*" in
    'show --property=LoadState --value fstrim.timer') printf '%s\n' "$TEST_TIMER_STATE";;
    'show --property=LoadState --value fstrim.service') printf '%s\n' "$TEST_SERVICE_STATE";;
    'enable --now fstrim.timer') return "$TEST_ENABLE_EXIT";;
    'is-enabled --quiet fstrim.timer') return "$TEST_ENABLED_EXIT";;
    'is-active --quiet fstrim.timer') return "$TEST_ACTIVE_EXIT";;
    *) printf 'UNEXPECTED MOCK systemctl %s\n' "$*" >&3; return 99;;
  esac
}

test_run() {
  TEST_CASES=$((TEST_CASES + 1))
  if TEST_OUTPUT="$(storage_trim_prepare_fresh 3>&1 2>&1)"; then TEST_STATUS=0; else TEST_STATUS=$?; fi
  [[ "$TEST_OUTPUT" != *'UNEXPECTED MOCK'* ]] || test_fail 'unexpected host operation'
}
test_warning() {
  [[ "$TEST_STATUS" == 2 && "$TEST_OUTPUT" == *'WARNING'* ]] || test_fail "$1"
  [[ "$TEST_OUTPUT" != *'Periodic unused-block trimming is enabled and active'* ]] || test_fail 'failed setup claimed periodic trimming was ready'
}
test_no_trim_or_enable() {
  [[ "$TEST_OUTPUT" != *'EVENT fstrim '* && "$TEST_OUTPUT" != *'EVENT systemctl enable '* ]] || test_fail "$1"
}

test_reset
test_run
[[ "$TEST_STATUS" == 0 && "$TEST_OUTPUT" != *'WARNING'* ]] || test_fail 'supported storage setup did not succeed'
[[ "$TEST_OUTPUT" == *'EVENT fstrim -v -- /'*'EVENT systemctl enable --now fstrim.timer'*'EVENT systemctl is-enabled --quiet fstrim.timer'*'EVENT systemctl is-active --quiet fstrim.timer'* ]] || test_fail 'root trim and timer verification ran in the wrong order'
test_run
[[ "$TEST_STATUS" == 0 && "$TEST_OUTPUT" != *'WARNING'* ]] || test_fail 'repeat setup did not remain safe and successful'

test_reset
TEST_EXISTING=1
test_run
[[ "$TEST_STATUS" == 0 && "$TEST_OUTPUT" != *'EVENT '* ]] || test_fail 'existing installation was changed'

for TEST_TOOL in fstrim findmnt systemctl; do
  test_reset
  TEST_MISSING_TOOL="$TEST_TOOL"
  test_run
  test_warning "missing $TEST_TOOL was not reported"
  test_no_trim_or_enable "missing $TEST_TOOL still caused a storage mutation"
done

for TEST_BAD_STATE in not-found masked; do
  for TEST_UNIT in timer service; do
    test_reset
    if [[ "$TEST_UNIT" == timer ]]; then TEST_TIMER_STATE="$TEST_BAD_STATE"; else TEST_SERVICE_STATE="$TEST_BAD_STATE"; fi
    test_run
    test_warning "$TEST_BAD_STATE $TEST_UNIT was not reported"
    test_no_trim_or_enable "$TEST_BAD_STATE $TEST_UNIT was unmasked or storage was trimmed"
  done
done

test_reset
TEST_ROOT_FIND_EXIT=1
test_run
test_warning 'unreadable root mount state was accepted'
test_no_trim_or_enable 'unknown root mount state allowed trimming'

test_reset
TEST_ROOT_OPTIONS=ro,relatime
test_run
test_warning 'read-only root filesystem was accepted'
test_no_trim_or_enable 'read-only root filesystem was trimmed'

for TEST_OPT_SOURCE in mount fstab; do
  test_reset
  if [[ "$TEST_OPT_SOURCE" == mount ]]; then TEST_ROOT_OPTIONS=rw,X-fstrim.notrim; else TEST_FSTAB_OPTIONS=defaults,X-fstrim.notrim; fi
  test_run
  test_warning "$TEST_OPT_SOURCE trim opt-out was not reported"
  test_no_trim_or_enable "$TEST_OPT_SOURCE trim opt-out was ignored"
done

test_reset
TEST_FSTAB_FIND_EXIT=1 TEST_FSTAB_OPTIONS=''
test_run
[[ "$TEST_STATUS" == 0 ]] || test_fail 'root mount missing from fstab was rejected'

test_reset
TEST_TRIM_EXIT=1
test_run
test_warning 'unsupported discard was reported as success'
[[ "$TEST_OUTPUT" != *'EVENT systemctl enable '* ]] || test_fail 'unsupported root discard activated a timer'

test_reset
TEST_ENABLE_EXIT=1
test_run
test_warning 'timer activation failure was reported as success'

test_reset
TEST_ENABLED_EXIT=1
test_run
test_warning 'disabled timer was reported as ready'

test_reset
TEST_ACTIVE_EXIT=1
test_run
test_warning 'inactive timer was reported as ready'

printf 'PASS: %s storage trim scenarios; all filesystem and systemd operations were mocked.\n' "$TEST_CASES"
