# Fresh installation quota check repair

The reinstalled server had a working 16 GiB ext4 loop filesystem mounted at
`/home/litecp` with `prjquota`. `quotaon --project --print-state /home/litecp`
printed `project quota ... is on` and returned 1. quota-tools print-state returns
the number of enabled quota types; treating every nonzero status as failure was
incorrect. The old mock returned zero for ON and did not reproduce that behavior.

The verifier now accepts status 0 or 1 only with an explicit project-quota ON
line and the project-quota mount option. Unknown/off states and other command
failures remain rejected. Tests cover both ON exit conventions, OFF, unknown,
command errors, user-only quota options, XFS enforcement and preservation guards.

Live recovery used the provider-console-verified new SSH fingerprint after the
OS reinstall. No host-key validation was disabled. Backups and metadata records
are retained in `/root/litecp-quota-repair-YKx6fENW`.

The quota helper was patched in the retained installer source and
`/usr/local/lib/litecp/prepare-website-quotas.sh`. Actual `--check` and
`--prepare-fresh` checks passed on the existing mount. The image inode/size and
fstab checksum were preserved; no formatting, quota disable, unmount or rescue
boot occurred. Mocked shell regressions passed on Linux.

The next user-console command is:

```sh
bash /opt/litecp-install.8Tc3wv8N/source/scripts/install.sh
```

The full installation has not been rerun by the agent. Run it in the console so
the newly generated administrator password remains visible to the user. The
permanent source repair is included in version 4.0.99. The existing
Makefile is retained; no release build or package was generated for this repair.
