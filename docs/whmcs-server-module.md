# WHMCS server integration

The LiteCP WHMCS server module targets WHMCS 8.9.0 and uses the existing HTTPS API credential envelope.

| Action | Parameters | Response |
|---|---|---|
| `whmcs.server_info` | Empty object | `version` string, `websiteCount` integer, `loadAverages` array of three numbers or null when unavailable |
| `whmcs.admin_sso` | Empty object | `ticket` string, `expiresIn: 60` |

Grant these two permissions explicitly to a dedicated credential. Restrict that credential to the WHMCS server outbound IP. Login requires the panel owner to enable **Trust WHMCS administrator SSO** on the API page. It defaults to disabled; enabling it authorizes SSO without the owner's password or extra authenticator code. API credentials cannot enable it. Existing rules do not gain the new actions automatically.

The module puts the ticket in the fragment of `https://PANEL:PORT/api/whmcs/sso#TICKET`. The landing page loads `/whmcs-sso.js`, removes the fragment and redeems the ticket using a same-origin JSON POST containing `ticket`. GET never creates a session; query parameters and cross-origin redemption are rejected. The script then navigates to `/`. HTTPS, no-store and no-referrer protections apply. Never enable external request/response body logging for ticket issuance or redemption.

Tickets are single use, expire in 60 seconds, and are stored as hashes in bounded process memory. API credential status, expiry, owner, secret revision, original WHMCS source restrictions and permissions are checked again at redemption. Browser IP is independent of the WHMCS source IP. The normal administrator session is created without persistent browser trust. Disabling trust blocks pending and new ticket logins; existing sessions keep the normal lifecycle.

The new consumer is a session protocol, not an externally callable management route. Its explicit broker allowlist entry supports privilege-separated installations. Server information uses real Linux load averages and returns null when they are unavailable.

This integration does not add customer accounts, provisioning or account synchronization. Deploy updated LiteCP before installing `modules/servers/litecp` in WHMCS. No release or deployment is performed by this source change.

Focused tests: `go test . -run 'TestWHMCS|TestAPISecrets|TestAPICatalogCoversPanelRoutes' -count=1`.
