package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// Called only by the root-only host-resolvers CLI in a transient systemd unit.
// Never replace resolv.conf or bounce the network connection (and SSH).
func (s *System) applyHostResolvers(ctx context.Context, values []string) error {
	resolvers, err := validateResolvers(values)
	if err != nil {
		return err
	}
	if s.os.ID == "ubuntu" {
		if !unitFileExists("systemd-resolved") {
			return errors.New("systemd-resolved is required; unmanaged resolver files are left unchanged")
		}
		const target = "/etc/systemd/resolved.conf.d/90-litecp.conf"
		original, readErr := os.ReadFile(target)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return readErr
		}
		if err := os.MkdirAll("/etc/systemd/resolved.conf.d", 0755); err != nil {
			return err
		}
		if err := writeAtomic(target, []byte("[Resolve]\nDNS="+strings.Join(resolvers, " ")+"\nFallbackDNS=\n"), 0644); err != nil {
			return err
		}
		if _, err := runCommand(ctx, 30*time.Second, "systemctl", "restart", "systemd-resolved"); err != nil {
			if readErr == nil {
				_ = writeAtomic(target, original, 0644)
			} else {
				_ = os.Remove(target)
			}
			_, rollbackErr := runCommand(context.Background(), 30*time.Second, "systemctl", "restart", "systemd-resolved")
			return fmt.Errorf("resolver restart failed: %v (rollback restart: %v)", err, rollbackErr)
		}
		return nil
	}
	if s.os.ID != "almalinux" {
		return errors.New("resolver management supports Ubuntu and AlmaLinux")
	}
	route, err := runCommand(ctx, 10*time.Second, "ip", "route", "show", "default")
	if err != nil {
		return err
	}
	fields := strings.Fields(route)
	device := ""
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == "dev" {
			device = fields[i+1]
			break
		}
	}
	if device == "" {
		return errors.New("default network interface not found; no changes made")
	}
	connection, err := runCommand(ctx, 10*time.Second, "nmcli", "-g", "GENERAL.CON-UUID", "device", "show", device)
	uuid := strings.TrimSpace(connection)
	if err != nil || uuid == "" || uuid == "--" {
		return errors.New("active NetworkManager connection not found; no changes made")
	}
	properties := []string{"ipv4.ignore-auto-dns", "ipv4.dns", "ipv6.ignore-auto-dns", "ipv6.dns"}
	rollbackArgs := []string{"connection", "modify", "uuid", uuid}
	for _, property := range properties {
		value, err := runCommand(ctx, 10*time.Second, "nmcli", "--escape", "no", "-g", property, "connection", "show", "uuid", uuid)
		if err != nil {
			return err
		}
		rollbackArgs = append(rollbackArgs, property, strings.TrimSpace(value))
	}
	v4, v6 := []string{}, []string{}
	for _, value := range resolvers {
		if net.ParseIP(value).To4() != nil {
			v4 = append(v4, value)
		} else {
			v6 = append(v6, value)
		}
	}
	args := []string{"connection", "modify", "uuid", uuid, "ipv4.ignore-auto-dns", "yes", "ipv4.dns", strings.Join(v4, ","), "ipv6.ignore-auto-dns", "yes", "ipv6.dns", strings.Join(v6, ",")}
	if _, err := runCommand(ctx, 20*time.Second, "nmcli", args...); err != nil {
		_, restoreErr := runCommand(context.Background(), 20*time.Second, "nmcli", rollbackArgs...)
		return fmt.Errorf("resolver profile update failed: %v (restore: %v)", err, restoreErr)
	}
	if _, err := runCommand(ctx, 20*time.Second, "nmcli", "device", "reapply", device); err != nil {
		_, restoreErr := runCommand(context.Background(), 20*time.Second, "nmcli", rollbackArgs...)
		_, reapplyErr := runCommand(context.Background(), 20*time.Second, "nmcli", "device", "reapply", device)
		return fmt.Errorf("resolver apply failed: %v (restore: %v; reapply: %v)", err, restoreErr, reapplyErr)
	}
	return nil
}
