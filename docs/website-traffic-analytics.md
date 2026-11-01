# Website Traffic Analytics

Open **Website Tools → Website Traffic Analytics**. Select a website and UTC date range. Reports include requests, estimated pageviews, sent/received bytes, error rates, recognized bots, pages, referral domains, devices, browsers, operating systems, response codes, downloads, HTTP versions and slow requests (at least one second). Live Access polls every three seconds while visible, can be paused and filtered, and retains the most recent 200 completed requests, paginated 20 per page.

## Collection and definitions

Nginx's shared HTTP-context format is installed at `/etc/nginx/conf.d/00-litecp-traffic.conf`. Each managed website writes an additional `logs/traffic-analytics.log`. Existing operational logs retain their destinations and format. Static locations write analytics without inheriting the operational access log. Configuration upgrades retain TLS and custom routing, run `nginx -t` before reload, and recover the previous configuration on failure. Existing websites receive logging during startup reconciliation; new sites inherit it from their generated configuration. Legacy roots outside managed website homes report analytics as unavailable.

Pageviews are **estimates**: successful 2xx HTML/XHTML GET responses, excluding recognized bots, common static assets, downloads and `/health` or `/healthz`. All-request counters still include those categories. Devices and bots are inferred from User-Agent and may be incorrect. Missing referrals mean **Direct / Unknown**, not necessarily a direct visit. Session counts, unique people, scrolls, browser rendering speed and engagement are not inferred. Duration measures Nginx processing/transfer time. Cloudflare/CDN or browser-cache responses that never reach this Nginx instance are absent.

Dates are UTC; weekly buckets start Monday. The initial collection contains only the new analytics logs, never reconstructed historical traffic. Missing intervals do not prove the website was offline. Charts display recorded buckets. Each breakdown keeps up to 80 distinct values per UTC day; additional values are combined as **Other (bounded detail)**. This is bounded detail, not a guaranteed top-80 algorithm.

The incremental collector runs every three seconds, processes up to 256 KiB of new log data per site and up to 32 websites per cycle, with round-robin fairness. Large installations or traffic bursts may lag real time. Offsets and aggregates are atomically persisted together in a private per-site file under `LITECP_DATA_DIR/website-traffic/`, keyed by SHA-256 of the immutable site ID. A first-record fingerprint including Nginx's random request ID survives rotation and gzip compression. Partial trailing records wait for completion; malformed complete JSON records are skipped and counted. Errors and last successful collection appear in the UI.

Each site has a 16 MiB snapshot limit, 400 day buckets, and 512 file identities; at capacity, existing data is preserved and a warning is shown. Compressed recovery replay is limited to 64 MiB per file; large rotated logs are retained and reported for administrator review, while other readable logs continue. No logs, snapshots or existing retention policies are deleted or changed automatically. This feature is intended for bounded panel reporting, not an unlimited raw log warehouse.

## Privacy, proxies and optional countries

Dedicated analytics logs strip query strings and store referral hostnames only. Cookies, Authorization headers and request bodies are not collected. Raw logs still contain client IPs and User-Agent strings and must remain private. Persisted reports and live access contain only IPv4 `/24` or IPv6 `/48` masked addresses, with no raw User-Agent. Client IPs use `$remote_addr`; arbitrary forwarded headers are never trusted. Administrators must configure Nginx's real-IP module with explicitly trusted proxies if required.

Optional country reporting supports an administrator-provided local JSON map at `LITECP_DATA_DIR/website-traffic-geo.json` (at most 4 MiB and 50,000 CIDRs), for example:

```json
{"192.0.2.0/24":"Example country","2001:db8::/32":"Example IPv6 country"}
```

Use an appropriately licensed and maintained dataset; longest prefix wins. It reloads once a minute, applies only to newly collected events, and sends no IPs to external services. Without a valid local database, locations display **Unavailable**. Country attribution is approximate; exact physical location is unavailable.

## API and verification

Authenticated read-only endpoint: `GET /api/sites/traffic?siteId=...`. Parameters: `range=today|7|30|custom`, `from=YYYY-MM-DD`, `to=YYYY-MM-DD` (inclusive, UTC), `group=hourly|daily|weekly|monthly`, `dimension=pages|sources|devices|browsers|systems|bots|statuses|downloads|slow|protocols|countries`, `page`, `size=10|25|50|100`. Custom ranges are limited to 366 days; hourly charts are limited to 31 days. Add `view=live`, optional `filter` and exact response `status` for recent events. Responses are `Cache-Control: no-store` and contain summaries/points/paginated rows, or up to 200 masked live entries, with collection metadata and warnings. Download classifications are based on common file extensions and successful responses; they do not prove that a user saved a file.

The existing root `Makefile` remains the automation entry point. Focused checks:

```text
go test ./... -run '^TestWebsiteTraffic' -count=1
node --test scripts/website-traffic.test.cjs
```

No packages or deployment are required by these checks. Actual Nginx validation and traffic delivery must also be verified on the destination Linux server before claiming production collection.
