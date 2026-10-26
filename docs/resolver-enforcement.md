# Default DNS resolver enforcement

Installation and upgrades set the system resolvers to exactly `8.8.8.8` and `8.8.4.4`. The previous fresh-install-only opt-out is no longer applied.

`litecp-resolvers.timer` runs its root-only oneshot checker 45 seconds after boot and every five minutes thereafter. The checker compares active resolver addresses and their order. If they differ, it reapplies the Google pair through NetworkManager on AlmaLinux or systemd-resolved on Ubuntu, then verifies the result. It does not bounce the network interface. Failures are recorded by systemd and retried at the next timer interval.

Inspect with `systemctl status litecp-resolvers.timer litecp-resolvers.service` and `journalctl -u litecp-resolvers.service`. The checker is independent of the panel's restricted service. It does not edit the panel configuration file concurrently with the running panel. Settings displays detected active resolvers; manually selecting another resolver is temporary while enforcement is enabled.

Validation: exact-pair comparison tests, installer shell syntax, live AlmaLinux profile application, active resolver readback, and a manually triggered checker run. A real server reboot and Ubuntu boot behavior were not tested during deployment. The current VPS resolver configuration was backed up before applying the change.
