#!/usr/bin/env bash
# Fresh-install maintenance only. Never formats, resizes or removes storage.
set -Eeuo pipefail

storage_trim_info() { printf '[STORAGE] %s\n' "$*"; }
storage_trim_warn() { printf '[STORAGE] WARNING: %s\n' "$*" >&2; }

storage_trim_existing_installation() {
  [[ -e /var/lib/litecp/config.json || -L /var/lib/litecp/config.json ]]
}

storage_trim_prepare_fresh() {
  local tool unit state options fstab_options result
  if storage_trim_existing_installation; then
    storage_trim_info 'Existing installation preserved; trim settings and filesystems were not changed.'
    return 0
  fi
  for tool in fstrim findmnt systemctl; do
    if ! command -v "$tool" >/dev/null 2>&1; then
      storage_trim_warn "$tool is unavailable; automatic storage reclamation could not be configured."
      return 2
    fi
  done
  if ! options="$(findmnt -n -r -o OPTIONS --mountpoint /)" || [[ -z "$options" ]]; then
    storage_trim_warn 'Cannot inspect the root filesystem; no trim settings were changed.'
    return 2
  fi
  # The vendor service honors this administrator opt-out. Direct fstrim would
  # bypass it, so inspect fstab too before performing the initial root trim.
  fstab_options="$(findmnt --fstab -n -r -o OPTIONS --mountpoint / 2>/dev/null)" || fstab_options=''
  case ",$options,$fstab_options," in
    *,ro,*|*,X-fstrim.notrim,*)
      storage_trim_warn 'Root storage is read-only or explicitly excludes trimming; its settings were preserved. Cloud snapshot reclamation is not verified.'
      return 2
      ;;
  esac
  # A live timer alone is insufficient if its service is missing or masked.
  # Do not unmask units, replace vendor files or overwrite administrator policy.
  for unit in fstrim.timer fstrim.service; do
    if ! state="$(systemctl show --property=LoadState --value "$unit" 2>&1)" || [[ "$state" != loaded ]]; then
      storage_trim_warn "$unit is unavailable or masked ($state); automatic root trimming is not configured. Existing unit settings were preserved."
      return 2
    fi
  done
  storage_trim_info 'Checking unused-block reclamation on the outer root filesystem.'
  if result="$(LC_ALL=C fstrim -v -- / 2>&1)"; then
    storage_trim_info "Root filesystem discard accepted. $result"
  else
    storage_trim_warn "Root filesystem trim failed or is unsupported: $result"
    storage_trim_warn 'Cloud snapshots may retain freed disk blocks. Check discard support with your storage provider; website files were not removed.'
    return 2
  fi
  if ! result="$(systemctl enable --now fstrim.timer 2>&1)"; then
    storage_trim_warn "Could not enable periodic trimming: $result"
    return 2
  fi
  if ! systemctl is-enabled --quiet fstrim.timer || ! systemctl is-active --quiet fstrim.timer; then
    storage_trim_warn 'fstrim.timer is not both enabled and active; periodic storage reclamation could not be verified. Inspect systemctl status fstrim.timer fstrim.service.'
    return 2
  fi
  storage_trim_info 'Periodic unused-block trimming is enabled and active through the OS fstrim.timer.'
  storage_trim_info 'Trim output reports blocks submitted for discard, not measured cloud snapshot savings. Snapshot size also depends on provider support and compression.'
}

storage_trim_main() {
  [[ "$(id -u)" -eq 0 ]] || { storage_trim_warn 'Run this installer helper as root.'; return 1; }
  [[ "${1:-}" == --prepare-fresh && "$#" == 1 ]] || { storage_trim_warn 'Use --prepare-fresh.'; return 1; }
  storage_trim_prepare_fresh
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then storage_trim_main "$@"; fi
