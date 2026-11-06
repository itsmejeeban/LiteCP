# LiteCP external API

Open **API**, below Recent Activities. In **API Rule**, create a named rule and select the permitted actions. In **API Credential**, generate an identifier/secret pair and assign one or more rules. Save the secret immediately: only its SHA-256 hash is persisted. The administrator association is the panel's existing administrator; this feature does not create additional administrator accounts.

Credentials can be edited, temporarily disabled, restricted to source IPv4/IPv6 addresses or CIDRs, given an expiry, or permanently revoked. Revoked credentials cannot be re-enabled. Remove rule assignments before removing a rule. Changes take effect immediately. Blank source restrictions allow any IP. Limits are per credential (default 60/minute, maximum 600) with an additional 600/minute source-IP ceiling. Windows reset after one minute and are in memory; restarting the panel resets rate windows.

## Requests

Use HTTPS with certificate verification. POST JSON to `https://PANEL:PORT/api/v1` with these headers:

```
Content-Type: application/json
X-LiteCP-Identifier: YOUR_IDENTIFIER
X-LiteCP-Secret: YOUR_SECRET
```

```json
{"action":"sites.list","parameters":{}}
```

Credentials in query strings and browser-session-only authentication are rejected. API credentials cannot authorize administrator endpoints, terminal access, arbitrary commands or credential management. Permissions apply to all websites managed by this panel; these are administrator integration credentials, not tenant-scoped accounts.

Example PHP request (load secrets from your module's encrypted configuration):

```php
$request = curl_init($panelUrl . '/api/v1');
curl_setopt_array($request, [
    CURLOPT_POST => true,
    CURLOPT_RETURNTRANSFER => true,
    CURLOPT_CONNECTTIMEOUT => 10,
    CURLOPT_TIMEOUT => 300,
    CURLOPT_HTTPHEADER => [
        'Content-Type: application/json',
        'X-LiteCP-Identifier: ' . $identifier,
        'X-LiteCP-Secret: ' . $secret,
    ],
    CURLOPT_POSTFIELDS => json_encode(['action' => 'sites.list', 'parameters' => new stdClass()]),
]);
$response = curl_exec($request);
$status = curl_getinfo($request, CURLINFO_RESPONSE_CODE);
// Check curl errors and HTTP status before interpreting JSON.
```

## Actions and parameters

| Action | Parameters |
|---|---|
| `sites.list` | None; returns `items` with website IDs |
| `sites.create` | `domain`, `appType` (`php`, `wordpress`, or supported panel application), `phpVersion`; optional `appVersion`, `createDatabase`, `enableCron`, `wordpressMode`, `wordpressUsername`, `wordpressPassword`, `wordpressEmail` |
| `sites.suspend`, `sites.unsuspend` | `id`; suspension is selected by action, not caller input |
| `sites.php` | `id`, `version`, optional `databasePassword` for legacy compatibility |
| `sites.domain` | `id`, `domain` |
| `sites.ssl` | `id`, `email`, optional `renew` |
| `sites.delete` | `id`, exact `domain` confirmation; permanently removes website data using existing panel safeguards; backups are retained |
| `databases.list` | None |
| `databases.create` | `name`, `username`, optional `password`, `siteId`; generated password returned once when omitted |
| `databases.password` | `id`, optional `password`; generated password returned when omitted |
| `databases.delete` | `id`; permanently deletes the managed database and local user |
| `maildomains.list` | None |
| `maildomains.create` | `domain` |
| `mailboxes.list` | None; password hashes excluded |
| `mailboxes.create` | `domain`, `localPart`, `password`, `quotaMb` (100–102400) |
| `mailboxes.password` | `id`, `password` |
| `mailboxes.delete` | `id`; disables the mailbox and retains Maildir data |
| `dns.read` | `site` (website ID) |
| `dns.write` | `site`, `records` or `zoneText`; uses existing DNS validation |

Responses use the existing panel handler's JSON shape and HTTP status. Lists return `{"items":[]}`. Errors return `{"error":"message"}`. Expect 401 for invalid/expired/revoked credentials or disallowed IP, 403 for missing permission or non-HTTPS, 429 with `Retry-After` for rate limits. Requests are serialized with panel mutations. Validation and conflict errors are retained from existing handlers.

Provisioning calls are synchronous. Do not blindly repeat create/delete/password operations after a timeout: query the current objects and reconcile first. No automatic retries or idempotency-key protocol is provided in this first version. Snapshot backups contain API rule settings and hashed credentials; review or revoke credentials on a restored server before exposing it.

## Verification

Run `go test -run '^TestAPI' .` and `node --check web/api-access.js`. The existing Makefile remains the build entry point. No external integration credentials are created automatically on installation.
