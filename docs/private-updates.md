# Private repository updates

Private LiteCP repositories need a dedicated GitHub fine-grained personal access token. Restrict it to `itsmejeeban/LiteCP`, give **Contents: read-only** (Metadata read access is automatic), and choose an expiry. No write or administrator permissions are required.

With LiteCP 5.0.6 or newer installed, run as root on the LiteCP server:

```sh
bash /usr/local/lib/litecp/configure-private-updates.sh
```

Enter the dedicated token at its hidden prompt. Do not paste it into chat, URLs or shell arguments. The script creates `/etc/litecp/github-update-token` with root ownership and mode 0600, and refuses to replace an existing credential.

The privileged broker reads this file for release checks. The update worker reads it separately for source downloads. Tokens are not returned in panel API responses, placed in process arguments/environment, or included in system snapshots. HTTPS verification remains enabled. Cross-host download redirects do not forward the Authorization header.

Click **Settings → Update → Check for updates** after configuration. Credential changes invalidate cached release checks. Keep the repository private; no temporary public exposure is needed once a valid token is configured.

Token replacement should be performed by the server administrator with the same ownership and permissions. Never reuse a broad workstation GitHub token on a hosting server. An expired/revoked token produces an error rather than a false successful update check.
