# Cloudflare-only firewall sources

In Security > Firewall > Add rule, choose Inbound, Allow, a port and protocol,
then select **Cloudflare only (IPv4 + IPv6)** under Source type. This preset
is also available when editing a single inbound required-port rule. Linked
profile guards appear as Cloudflare only in that table and refresh with the
other managed guards. Switch the address type back to Any/custom to remove
only that linked restriction. Port ranges and outbound rules use custom addresses.

LiteCP fetches both official HTTPS lists, validates every CIDR, and adds an early
nftables input guard for that port. Other IPs are dropped before broad host-firewall
allow rules. Loopback is retained. SSH listeners and the panel port are protected.
This trusts Cloudflare networks, not a particular Cloudflare account or zone.

The nftables package must be installed. UFW must have IPv6 enabled. The host's
public firewalld zone is used for the associated allow rules. Provider firewalls
remain independent. The DNS record must use Cloudflare proxying, and the chosen
service/port must be supported by your Cloudflare product.

A per-port boot unit loads the last validated guard before network configuration.
While LiteCP runs, it refreshes lists on startup and every 24 hours. Failed or invalid
downloads retain the previous rules and create a Recent Activities warning. A
successful refresh replaces both address sets atomically, removing obsolete ranges.

Removing the rule disables its boot unit, removes its associated host allow rules
and removes the restriction. Other host rules remain intact and can permit direct
access again. Existing connections can be interrupted when a restriction is applied.

Local checks do not prove live enforcement: verify from Cloudflare and a direct
external IP before relying on this policy in production.
