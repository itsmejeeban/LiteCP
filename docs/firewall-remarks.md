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
