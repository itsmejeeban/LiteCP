# Optional application hosting

Node.js, Python and Ruby hosting have separate switches under Application Hosting. Missing settings mean disabled. Merely enabling a switch starts no services or image downloads. Existing WordPress/WHMCS sites remain PHP applications. Configure a dedicated Regular PHP/HTML site, upload code, choose a version, save startup/setup commands, and Start.

Supported official-image selectors:

- Node.js 22 and 24 (Debian bookworm slim).
- Python 3.11, 3.12, 3.13 and 3.14 (official slim images).
- Ruby 3.3, 3.4 and 4.0 (official slim images).

These are version-family tags: Start pulls the repository's current matching image. Installed running containers stay on their image until redeployed; there is no automatic application image upgrade. Custom image names are not accepted.

## Application contract

The working directory is `/app`, mounted from the website public directory. Startup commands execute inside the container as the website runtime UID/GID, not as host root. Use a production HTTP server bound to `0.0.0.0:8080`; PORT and HOST environment values are supplied. For example `node server.js`, `python -m gunicorn --bind 0.0.0.0:8080 app:app`, or `bundle exec puma -b tcp://0.0.0.0:8080`. The user must provide the matching application and dependencies.

Optional setup commands run before a new deployment in the same runtime image and writable application directory. Examples: `npm ci --omit=dev`, `pip install -r requirements.txt` (PIP_TARGET/PYTHONPATH point at `/app/.python-packages`), and `bundle config set --local path .bundle && bundle install`. GEM_HOME points at `/app/.gems`. Slim images do not contain every compiler or native library: prebuild compatible dependencies or choose dependencies supported by these images. Setup may change uploaded files; it is not an atomic source deployment.

The root filesystem is read-only, `/tmp` is a 128 MiB tmpfs, all capabilities are dropped, no-new-privileges is enabled, and each app is limited to 256 MiB memory, one CPU and 128 processes. Container network isolation is retained; no host network or public application port is used. Nginx reverse proxies HTTP and WebSocket upgrades from the website domain to a unique localhost port. The existing certificate is preserved. Enable SSL before Start; disable the website runtime before changing SSL or PHP settings.

Local MySQL's socket is mounted when present at `/run/mysqld/mysqld.sock`, read-only as a filesystem mount. Use socket-aware database drivers and the existing managed database credentials. Credentials are not injected by LiteCP. Remote services require their normal reachable endpoint and credentials. The container has outbound network access for ordinary application calls and dependency downloads.

## Lifecycle and recovery

Start pulls an allowlisted official image, runs optional setup, starts the container, checks its TCP listener and validates/reloads Nginx. Routing/state failures attempt to restore the previous website. A listening port is not proof that every endpoint is healthy. Startup failure containers and logs are retained for inspection. Logs show only the last 150 lines and may contain data printed by the application.

Stop interrupts application traffic but preserves application proxy routing. Start resumes the saved stopped container. Disable website runtime stops the container and restores the prior saved Nginx configuration, keeping files and configuration. Version/command editing requires a disabled website runtime. Disable all website runtimes for a language before disabling its feature switch. Website detachment attempts to stop its saved application container before removing normal hosting configuration.

Restart policy is `always`; Start enables `podman-restart.service` for reboot recovery. Actual reboot behavior was not exercised during implementation. Stopped/deployment/setup containers, downloaded images and routing recovery snapshots are retained; no destructive cleanup runs automatically.

The panel API uses the existing administrator authentication, origin checking and serialized mutations. Images and runtime versions are allowlisted; domain/root/UID come from the managed website, ports are selected by the server, and startup text is passed as an argument to the container shell rather than the host shell. Commands are visible in administrator state; never place credentials in them.

## Prerequisites and validation

Use `bash scripts/install-application-hosting.sh` if Podman is unavailable. The installer adds only the container engine and leaves all feature switches disabled. No separate public firewall ports are needed. Allow enough disk for images/dependencies and memory for each app. Native application hosting installs do not replace host Python/Ruby/Node versions.

Tests cover disabled defaults, allowlisted versions, rejection of WordPress conversion, container isolation flags, proxy-only routing and administrator authentication. Opt-in live tests use three temporary localhost HTTP fixture applications; they do not convert production websites or prove compatibility with all frameworks.

Official image references: [Node.js](https://hub.docker.com/_/node), [Python](https://hub.docker.com/_/python), [Ruby](https://hub.docker.com/_/ruby).
