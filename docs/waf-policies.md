# Server WAF and Website WAF

Local implementation; Linux/Nginx activation and browser acceptance still require testing. These controls do not reproduce Cloudflare's distributed network, bot scores or managed challenges.

## Navigation

- Security → Server WAF: rules for HTTP/HTTPS reaching existing Nginx virtual hosts, including panel, webmail and default hosts.
- Website Tools → Website WAF: select a managed website and configure its policy.
- The previous WAF screen is retained as a legacy route; its navigation entry is hidden. Once advanced policies exist, the legacy mutation API rejects changes to avoid bypassing server protection.

Both screens provide Off / Detect only / Protect, custom rules, managed CRS protection, rate limits, and paginated events. Changes remain drafts until Apply policy. Rules support enable/disable, edit, duplicate and ordering. Refresh prompts before abandoning a draft.

## Matching

Client IP/CIDR, Host header, raw request URI, normalized path, query, named arguments, method, protocol, user agent, Referer, headers, cookies, declared MIME, raw body, body/request size, HTTPS and uploaded filenames. Conditions support AND/OR groups; advanced JSON allows nested groups. Negative leaf operators and single-child NOT groups are supported. Limits: 100 rules, 20 limiters per scope, five group levels, 32 expanded branches, 24 conditions per branch, 1024 bytes per value.

Country/ASN, verified client certificates, TLS version and verified file MIME are explicitly unavailable pending their data/connector/trust integrations. URL matching uses the request URI; match hostname and HTTPS separately. Raw X-Forwarded-For can be matched as a header but is not trusted identity. The engine's existing body inspection limits and parsers still apply. No new arbitrary proxy trust is introduced.

## Precedence and limits

Server custom rules precede website custom rules. Block ends evaluation; Log continues. Detect compiles custom rules as pass/log actions. There are no blanket Allow/Skip controls that can silently bypass mandatory server rules.

Managed CRS runs once per request. When enabled in both scopes, the stronger protection mode applies to shared managed inspection. This differs from fully independent inspection engines and is stated in the interface.

Rate limits use shared Nginx zones keyed by effective client IP and normalized path prefix, with requests/minute and optional immediate burst allowance. Excess requests receive 429 until the leaky bucket refills. Detect uses Nginx dry-run. Mixed Detect/Protect limiters in inherited server/website scopes are rejected because Nginx has one dry-run control per context. Arbitrary counting windows, custom block duration and non-IP counting keys are not implemented.

## Configuration and recovery

- Policy: `/etc/litecp/waf/policies.json` (0600 in demo data directory; restrictive permissions on server).
- HTTP-context rate zones: `/etc/nginx/conf.d/00-litecp-waf-policy.conf`.
- Global rules: `/etc/litecp/waf/global-policy.conf`.
- Managed website rules: `/etc/litecp/waf/site-<id>.conf`.
- Existing default/panel/webmail virtual hosts receive a marked server include. Reapply after adding unmanaged virtual hosts. Custom WAF configurations are rejected for review rather than overwritten.

The connector merges parent rules, so ModSecurity policy is attached at virtual-host level rather than duplicated at HTTP and virtual-host levels. Existing files are captured for rollback. All proposed changes must pass `nginx -t` before reload. Failed validation restores the prior files. A failed reload restores files, validates and reloads the restored configuration, and reports failure. Recovery failures are reported explicitly; verify runtime state through SSH if reload times out. Keep SSH available while testing blocking rules.

Direct Go listeners, SSH, SMTP, FTP, provider firewalls, upstream bandwidth exhaustion and custom locations that override inherited directives are outside this WAF's guaranteed coverage. Server mode does not imply whole-network protection.

## Events

Detected/Blocked events use existing ModSecurity audit summaries; Rate limited events use the default Nginx error log. At most 2 MiB per log, 20 websites and 1000 summaries are sampled, with 20 events per page. These are bounded samples, not lifetime counts. Server custom-rule events use a distinct prefix. Managed CRS events remain in website logs. Bodies, cookies and authorization headers are not returned. Paths may be redirected by custom Nginx configuration, in which case default log sampling cannot see those events.

## Verification

Focused tests cover nested condition expansion, complexity bounds, unsupported fields, IP validation, regex validation, action escaping, Detect non-blocking behavior, server precedence, collision-resistant rate-zone names and dry-run semantics. JavaScript syntax is checked. No Linux service was reconfigured and no live site was blocked during local verification. Existing Makefile remains the build entry point; no release binary or archive is generated.

Before production: run Linux Nginx syntax/behavior tests; test panel recovery; verify IPv4/IPv6 and trusted proxies; exercise WordPress/WHMCS uploads, APIs and WebSockets; test rate limits concurrently; confirm actual events and UI at desktop/mobile sizes. GeoIP/ASN, mTLS, richer rate limiting, exception controls, complete event analytics, retention and complete-history export and deploy verification remain follow-up work.

References:
- https://github.com/owasp-modsecurity/ModSecurity-nginx/blob/master/src/ngx_http_modsecurity_module.c
- https://nginx.org/en/docs/http/ngx_http_limit_req_module.html
- https://modsecurity.org/docs/3-reference-manual/3-3-variables/
