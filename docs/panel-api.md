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

Credentials in query strings and browser-session-only authentication are rejected. The explicit `panel.*` permission catalog covers administrator settings, security operations and API credential management as well as hosting features. Those powerful actions must be selected deliberately. Permissions apply panel-wide; these are administrator integration credentials, not tenant-scoped accounts. Existing rules do not gain newly added permissions automatically.

## Full panel coverage

See [the full route and permission catalog](panel-api-catalog.md). API Rule includes searchable groups for Overview, Website Tools, File Manager and Server File Explorer, Databases, Email, Security, Server Environment, Services, Backups, System Snapshot, Migration, Analytics, Updates, Logs, Recent Activities, Settings and API Administration. Each permission binds one registered route and HTTP method. Where a panel endpoint selects sub-operations through its body (for example service start/stop/restart), its method permission grants those validated sub-operations; it is not a separate checkbox for every body value.

The original short actions below remain compatible. New actions use this JSON envelope:

```json
{
  "action": "panel.sites.resource_limits.put",
  "query": {"siteId": ["WEBSITE_ID"]},
  "parameters": {"enabled": false, "cpuPercent": 50, "memoryMiB": 512, "diskMiB": 1024, "tasks": 64, "concurrentRequests": 8}
}
```

`query` uses arrays of string values, matching URL query parameters. `parameters` is the target endpoint's JSON request body. `objectId` is required only for catalog routes ending in `/`, such as `panel.databases.delete`; it is one identifier, never a path. The dispatcher rejects path traversal and chooses the target path/method from its fixed catalog, not from caller-supplied URLs. Request fields, prerequisite checks, confirmations and response bodies match the existing panel endpoints. Consult the feature's frontend request or backend handler for its full body schema; the catalog documents route/method coverage rather than inventing alternate field names.

Binary upload/download mode: POST to `/api/v1/request`, supply the same identifier/secret headers plus `X-LiteCP-Action: panel.system_snapshots.upload.post`. Send the original multipart body and `Content-Type`, exactly as the panel upload form does. Put ordinary query parameters on this URL. For suffix routes add `X-LiteCP-Object-ID`. Downloads return the original bytes, content type and download headers. JSON mode remains limited to 64 KiB; streaming mode retains the underlying feature and broker transfer limits. The service never buffers a complete upload into JSON/base64.

Examples:

| Task | Permission | Additional request data |
|---|---|---|
| Resource limits | `panel.sites.resource_limits.get`, `.put`, `.post`, `.delete` | `query.siteId`; existing settings/recovery body |
| FTP | `panel.website_ftp.get`, `.post`, `.put`, `.delete` | `query.siteId`; existing account/password/confirmation body |
| Website backup | `panel.website_backups.post` | Existing website backup body and query |
| Website restore | `panel.website_backups.restore.post` | Existing restore confirmation/body |
| System snapshot restore | `panel.system_snapshots.restore.post` | `name`, `confirmation: "RESTORE ENTIRE LITECP"` |
| Remote storage scan | `panel.system_snapshots.remote.get` | Existing remote scan query |
| Service controls | `panel.services.action.post` | `service`, `action: "start"`, `"stop"` or `"restart"` |
| Server updates | `panel.system_updates.get`, `.post` | Existing update job body |
| Panel updates | `panel.updates.install.post`, `panel.updates.status.get` | Existing update options |
| API management | `panel.access.get`, `.post` | Existing rule/credential operation body |

### Existing product boundaries

Terminal command/session routes are present in the coverage catalog but are unavailable and cannot be selected: the privilege-separated panel deliberately disables browser/root shell execution. Use SSH for server shells. The API dispatcher enforces the same broker restriction rather than creating a bypass. Terminal capability/status remains available through `panel.shell_capabilities.get`.

Login/logout, internal one-time-ticket consumers and the phpMyAdmin HTML proxy are session protocols rather than management actions and are not granted as external API endpoints. Their supported launch/configuration management actions are catalogued. Browser-only display preferences such as local dark/light mode or sidebar state have no server management state to control. Documentation is static content, not a mutation endpoint.

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

Responses use the existing panel handler's JSON shape and HTTP status. Lists return `{"items":[]}`. Errors return `{"error":"message"}`. Expect 401 for invalid/expired/revoked credentials or disallowed IP, 403 for missing permission or non-HTTPS, 429 with `Retry-After` for rate limits. Mutating requests are serialized with panel mutations, except the same cancellation/upload exceptions used by the panel. Read requests do not hold that global lock. Validation and conflict errors are retained from existing handlers.

Provisioning calls are synchronous. Do not blindly repeat create/delete/password operations after a timeout: query the current objects and reconcile first. No automatic retries or idempotency-key protocol is provided in this first version. Snapshot backups contain API rule settings and hashed credentials; review or revoke credentials on a restored server before exposing it.

## Verification

Run `go test -run '^TestAPI' .` and `node --check web/api-access.js`. The existing Makefile remains the build entry point. No external integration credentials are created automatically on installation.
