# Firewall remarks

Version 4.0.105 adds optional multiline Remarks (up to 500 characters) to custom
firewall rules. The text is saved in LiteCP configuration, shown in the custom
rules table and restored when editing a rule. Existing labels and rules remain
compatible. Older API clients that omit remarks when editing retain the saved
text. Label/remarks-only edits do not reapply rules or reload the firewall.

The required-port table automatically explains each port's service and purpose,
including direction-specific DNS, mail, web, SSH, FTPS and time synchronization.
Configured panel, SSH and webmail ports are taken into account. Existing required
rules gain these descriptions when loaded; reapplying the profile is unnecessary.
Unrecognized retained ports are marked for review rather than assigned a guessed
service. Generated descriptions are read-only; custom rules have editable notes.

Inbound is displayed in blue and Outbound in purple in both tables, with visible
text labels. Remarks are displayed as plain text, including line breaks. They
are panel metadata, not executable commands or OS firewall comments.

## Address restrictions

Custom rules accept IPv4/IPv6 addresses or CIDR networks with no fixed address-count cap, separated
by commas or new lines. Inbound rules use source addresses; outbound rules use
destination addresses. Use `any` for all addresses. Cloudflare is a separate
inbound preset and cannot be combined with an address list. Existing host rules
are not taken over when a duplicate selector is detected.

The required-port editor also supports address restrictions, port/range,
protocol and remarks. Its direction, zone and Allow action stay fixed to the
selected service rule. Current SSH and panel access rules cannot be narrowed in
this editor. Each address is verified separately in the host firewall; a failed
replacement restores the previous rule. Other rules can still permit access.

The required-port editor also offers Cloudflare only for a single inbound port.
Its early guard blocks non-Cloudflare traffic independently of other allow rules.
Switching back removes only its linked guard; unrelated Cloudflare rules remain
managed separately. Large address lists still depend on host capacity and the
standard 1 MiB JSON request size. The streaming external API request endpoint can
be used for lists exceeding the external API JSON envelope size.
