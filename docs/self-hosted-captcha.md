# Self-hosted CAPTCHA

Security → Self-hosted CAPTCHA configures each website separately. The default is Off.
Monitor counts checks and observes request/connection limits without enforcing them.
Challenge all visitors requires a six-digit local image verification before website access.
No Cloudflare account, external CAPTCHA provider, or new package dependency is used.

## Requirements and operation

Install/update using the existing LiteCP installer/updater, which installs the isolated
`litecp-captcha.service` and its dedicated non-login user. The gateway listens only on
127.0.0.1:9092. Never expose that port publicly. Website HTTPS and an Nginx build with
`--with-http_auth_request_module` are required; enabling refuses unsupported installations.
Existing LiteCP Nginx proxy connectivity must remain permitted by SELinux. The feature
does not disable SELinux or alter firewall policy.

Choose a website, select a mode, configure limits, and save. Static, PHP and application
requests use the same verification gate. Per-IP and aggregate website request limits
continue after verification. SSL challenge validation bypasses CAPTCHA. Explicitly
excluded paths bypass verification but retain rate limits; only exclude authenticated
application endpoints. Unverified POST requests are rejected, never replayed.

Clearance uses Secure, HttpOnly, SameSite=Lax, host-only cookies bound to the website,
policy revision, client address and browser user agent. Changing policy invalidates old
clearance. Challenges expire after three minutes and each answer can be submitted once.
The signing secret stays in private panel storage; the gateway receives only that secret
and protected website policies. Gateway memory, CPU, concurrent handlers, challenge
count and verification request rates are bounded. An unavailable gateway fails closed
for protected content. Off removes the gate and limits without deleting website data.

Configuration changes test Nginx before reload and restore previous configuration on
failure. Settings travel with website/snapshot metadata; restore revalidates service
availability. The runtime configuration and rate zones are reconstructed on panel start.
Updates restart the gateway so it uses the updated binary. In-memory challenges and
activity counters reset on gateway restart. The gateway counts CAPTCHA request-rate violations; website Nginx error logs record connection and WAF limits. CAPTCHA request rates use bounded one-second windows in the gateway so WAF Detect/Block settings remain independent.

## API

GET `/api/security/captcha` returns websites, policies and available gateway counters.
PUT accepts `{ "siteId": "...", "settings": { ... } }` using the fields shown in the UI.
External API credentials need `panel.security.captcha.get` / `.put` permissions through
the normal API dispatcher. The public challenge endpoints are separate from panel APIs.

## Protection boundary

This is basic browser friction and HTTP rate control, not a promise to stop all bots or
DDoS attacks. OCR and human-solving services can solve image challenges. Distributed
traffic can still consume server or network capacity; upstream filtering remains needed.
The image has no audio alternative: operators must provide an alternative access path
for visitors who cannot read it. APIs, webhooks, crawlers and accessibility flows need
explicit planning before enabling Challenge mode. Behind a proxy, configure trusted
real-client addressing in Nginx first; otherwise clients share the proxy's allowance.

Run focused verification with `go test -run TestCaptcha .`. The existing Makefile remains
the build entry point. Linux Nginx parsing and live browser/SELinux behavior must be
verified on the destination before claiming production readiness.

## Automatic mode

Automatic requires Resource Limit enabled for the selected website and HTTPS. Healthy
websites pass without a challenge. Verified runtime CPU exhaustion, memory/disk/process
allowance saturation, and resource blocking activate verification. The broker projects
resource state every two seconds; the gateway reloads within one second. Protection
remains active for five minutes after the last detected breach and survives panel restarts.
A new overload period invalidates clearance from the previous period.

Hard resource isolation is preserved: a stopped or recovering website stays unavailable
(HTTP 503) after verification. CAPTCHA does not restart workloads or override manual
suspension. Unverified visitors can see verification while the resource block is active.
Disabling CAPTCHA reinstates the normal frontend resource gate. Disabling resource
limits makes Automatic inactive. Stale resource enforcement fails closed. The settings
page shows the current automatic state after Refresh.
