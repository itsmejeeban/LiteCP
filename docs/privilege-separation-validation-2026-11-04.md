# Privilege separation validation - 4 November 2026

Release source: 4.0.96. Changes are local; the installed panel was not switched.

Verified on an isolated copy at `/root/litecp-privilege-qa-tWsyL994` on AlmaLinux
with SELinux enforcing:

- Focused Go tests passed with the race detector.
- A real subprocess under UID 65534, without effective capabilities, served the
  frontend transport and reached the root broker. Reading a root-private fixture
  failed. Public version access worked; private state access without credentials
  returned 401; browser terminal access returned 403.
- Kernel peer-UID verification rejected a mismatched identity.
- Authenticated streaming preserved request/response data, query parameters,
  cookies and response flushing.
- An unauthenticated request withholding a large body was rejected without
  waiting for that body.
- Metadata/body bounds, malformed/truncated stream handling, operation policy,
  root startup rejection and dashboard authentication checks passed.
- Bash syntax and systemd unit validation passed without installing the units.
- Four local Node installation-policy checks passed. `git diff --check` passed.

No live service/account was changed for these tests. The test copy was retained;
no existing source, website, mailbox, database or backup was removed. Go test
fixtures were confined to temporary test directories. The existing Makefile was
retained; no production binary, release archive or installer package was built.

A fresh Ubuntu/AlmaLinux installation, full installed-service browser regression,
and actual update rollback still need staged acceptance. Component checks do not
prove every privileged workflow. The installer/update paths now perform explicit
runtime identity, capability, service and version checks before reporting success.
