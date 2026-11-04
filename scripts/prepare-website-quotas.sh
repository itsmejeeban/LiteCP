#!/usr/bin/env bash
# Called by install.sh. Never formats a block device or moves website data.
set -Eeuo pipefail

quota_info() { printf '[QUOTA] %s\n' "$*"; }
quota_die() { printf '[QUOTA ERROR] %s\n' "$*" >&2; return 1; }

quota_mount_has_projects() {
  case ",$1," in *,prjquota,*|*,pquota,*) return 0;; *) return 1;; esac
}

quota_verify() {
  local website_home="$1" kind options mount state
  kind="$(findmnt -n -T "$website_home" -o FSTYPE)" || return 1
  options="$(findmnt -n -T "$website_home" -o OPTIONS)" || return 1
  mount="$(findmnt -n -T "$website_home" -o TARGET)" || return 1
  quota_mount_has_projects "$options" || return 1
  case "$kind" in
    ext4)
      state="$(LC_ALL=C quotaon --project --print-state "$mount" 2>&1)" || return 1
      grep -Eiq 'project quota .* is on[[:space:]]*$' <<<"$state" || return 1
      ;;
    xfs)
      state="$(LC_ALL=C xfs_quota -x -c 'state -p' "$mount" 2>&1)" || return 1
      grep -Eq 'Accounting: ON' <<<"$state" && grep -Eq 'Enforcement: ON' <<<"$state" || return 1
      ;;
    *) return 1;;
  esac
}

quota_check() {
  local website_home="$1"
  if quota_verify "$website_home"; then
    quota_info "Project quota accounting and enforcement are active for $website_home."
    return 0
  fi
  quota_info "Project quotas are NOT active for $website_home. Disk enforcement must remain unavailable."
  quota_info 'An existing root filesystem may require offline maintenance. No existing filesystem was changed.'
  return 2
}

quota_prepare_fresh() {
  local website_home="$1" image=/var/lib/litecp/website-storage.ext4
  local available bytes gib backup entry
  if quota_verify "$website_home"; then quota_check "$website_home"; return; fi
  # Never cover an existing mount or any existing data, including dotfiles.
  if mountpoint -q "$website_home" || [[ -n "$(find "$website_home" -mindepth 1 -maxdepth 1 -print -quit)" ]]; then
    quota_info 'Existing website storage preserved; automatic provisioning skipped.'
    quota_check "$website_home"; return
  fi
  if [[ -e "$image" || -L "$image" ]] || grep -Fq "$website_home" /etc/fstab; then
    quota_die 'Existing storage image or fstab entry found. Review it before resuming; nothing was overwritten.'; return 1
  fi
  available="$(df -B1 --output=avail "$website_home" | tail -1 | tr -d ' ')"
  [[ "$available" =~ ^[0-9]+$ ]] || { quota_die 'Cannot determine available storage.'; return 1; }
  # Reserve at least half the available space for OS, databases and snapshots.
  # Override with an explicit whole-GiB size, still retaining at least 4 GiB.
  gib="${LITECP_WEBSITE_STORAGE_GIB:-$((available / 2 / 1073741824))}"
  [[ "$gib" =~ ^[1-9][0-9]{0,5}$ ]] || { quota_die 'Storage size must be a positive whole number of GiB.'; return 1; }
  bytes=$((gib * 1073741824))
  ((gib >= 2 && bytes <= available - 4294967296)) || { quota_die 'Need at least 2 GiB website storage and 4 GiB remaining free space.'; return 1; }
  quota_info "Allocating ${gib} GiB dedicated website storage; this is separate from OS/database space."
  install -d -m 0700 /var/lib/litecp
  # noclobber protects an existing image even when another process races us.
  (set -o noclobber; : > "$image") || return 1
  chmod 0600 "$image"
  fallocate -l "$bytes" "$image" || { quota_die 'Allocation failed; image retained for review.'; return 1; }
  # This is ONLY the newly created regular file, never a server block device.
  mkfs.ext4 -q -O project,quota -E quotatype=prjquota,nodiscard -m 0 "$image"
  backup="$(mktemp /etc/fstab.litecp-quota.XXXXXXXX)"
  cp --preserve=mode,ownership,timestamps /etc/fstab "$backup"
  entry="$image $website_home ext4 loop,prjquota,nodev,nosuid 0 0"
  printf '\n# LiteCP dedicated website storage; preserve this mount before starting websites.\n%s\n' "$entry" >> /etc/fstab
  systemctl daemon-reload
  mount "$website_home" || { quota_die "Mount failed. Image and fstab backup retained: $backup"; return 1; }
  chmod 0755 "$website_home"
  quota_verify "$website_home" || { quota_die 'Mounted storage failed project-quota verification; installation stopped.'; return 1; }
  quota_info "Dedicated storage ready. fstab backup: $backup"
  quota_check "$website_home"
}

quota_main() {
  local mode="${1:---check}" website_home=/home/litecp
  [[ "$(id -u)" -eq 0 ]] || { quota_die 'Run as root.'; return 1; }
  [[ -d "$website_home" && ! -L "$website_home" ]] || { quota_die 'Website home must be a real directory.'; return 1; }
  case "$mode" in
    --check) quota_check "$website_home";;
    --prepare-fresh) quota_prepare_fresh "$website_home";;
    *) quota_die 'Use --check or --prepare-fresh.';;
  esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then quota_main "$@"; fi
