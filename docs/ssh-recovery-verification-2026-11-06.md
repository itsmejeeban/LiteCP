# SSH and independent recovery verification - 6 November 2026

## Verified

- Pinned-host SSH password login to the existing server works on TCP 369.
- SSH configuration validates. The active systemd drop-in 99-litecp-port.conf starts sshd with -p 369; plain sshd -T defaults to port 22 and must not be treated as the active listener port.
- Root/password authentication remains enabled; public-key authentication is allowed.
- /root/.ssh mode 0700, root-owned. authorized_keys mode 0600, root-owned, size zero: no root administrator key installed.
- This Windows profile's standard .ssh directory contains only known-host files. ssh-add cannot connect to an agent. No private-key contents were read, generated, stored or transmitted.

## Not ready / pending

- Key-only administrator login is not configured or verified. Install a user-controlled public key and test a separate key-only connection before restricting password access. Do not disable the current working route first.
- No provider/rescue/VNC console was available in the inspected Chrome tabs. Provider-console access has been requested from the user. Its availability is not proven by working SSH or by the LiteCP terminal.
- No SSH configuration, firewall rule, authorized key, password or recovery setting changed during this verification.

## Independent route verified afterward

The user opened the authenticated Hetzner console for litecp / 162.55.50.122 and completed OS login. Read-only commands were executed through the browser VNC console: hostname returned litecp, id returned uid=0(root), and tty returned /dev/tty1. This verifies an independent, interactive provider-console route to the running OS without relying on SSH or LiteCP. No reboot, rescue boot or destructive recovery was performed; offline rescue recovery is not claimed as tested.

Final result: independent console PASS; SSH key access NOT CONFIGURED. Current password SSH remains available. A user-controlled administrator public key and a successful separate key-only login are still needed before password SSH can safely be restricted.

## Dedicated SSH key installed and tested - 6 November 2026, 21:18 IST

The user authorized the key-access test. A dedicated passphrase-encrypted Ed25519 key was created locally at C:\Users\jeeba\.ssh\litecp_ed25519; the user entered its passphrase directly in a local terminal. The empty-passphrase check rejects decryption. Only the public key was sent to the server. Fingerprint: SHA256:sLsbmk1pkWjL7uKdLNvaYOEm4rfpd49mcA0kIn1aaWQ.

The public key was appended idempotently to root authorized_keys, preserving existing entries, with agent/port/X11 forwarding disabled. A separate Windows OpenSSH connection used this exact key, strict pinned-host verification, no agent and disabled password/keyboard-interactive fallback. It exited 0 and returned root UID 0 and hostname litecp. Server sshd independently recorded Accepted publickey for root with the exact fingerprint at 21:18:46 IST.

The temporary test initially reported failure because a Windows command-line quoting issue changed printf's newline marker into a literal n, joining the marker and UID. The SSH login itself succeeded. The marker was corrected to echo; the original diagnostic JSON was preserved rather than rewritten as a passing test. Server authentication evidence establishes successful key-only access without asking for another passphrase entry.

Updated result: key-only SSH PASS; independent Hetzner console PASS. The private key remains outside the repository and encrypted. Password SSH remains enabled to preserve the existing updater/access route; no password authentication or root login restriction was applied. Independent rescue boot / offline recovery remains untested.
