# Security update retest - 6 November 2026

Deployed version endpoint: 4.0.159, revision 80d516a401e50cbbc325948660d38831de860bb5.

## Live checks

- Oversized invalid login JSON plus trailing whitespace returns 400 with the size-limit error. No real login credentials were submitted.
- Both external API transports return 401 to invalid credentials when the declared 64 KiB request body is withheld.
- A remaining intermittent failure was reproduced: those early 401 responses sometimes lost their JSON body or final HTTP framing. These were a handful of bounded requests, not a load attack.
- The test endpoint has its known self-issued certificate; the scripted read-only checks skipped certificate-chain verification only for that known endpoint. No client or server TLS configuration was changed.

## Confirmed repair in local 4.0.160

The frontend deliberately interrupts unread uploads with a read deadline. This can cancel the incoming HTTP context. Its cancellation watcher could then close the broker connection before the rejection body had been fully forwarded. Stop that watcher before intentionally interrupting the upload; genuine cancellation handling remains active before that point, and broker connection deadlines still bound forwarding.

The existing slow-upload Linux test only asserted the HTTP status. It now reads the entire response, verifies the expected error, and fails on incomplete HTTP framing.

## Evidence

An isolated Linux source copy was used, without changing the installed panel:

- Before repair: 5 of 20 repeated slow-upload response-body checks failed with unexpected EOF.
- After repair: all 20 repetitions passed.
- Affected Linux broker, new audit and API regression tests passed.
- Local go vet and git diff --check passed.

The Makefile remains the build entry point. No release binary/package was generated or deployed. No service restart, real credential use, data deletion or website/database mutation was performed. The isolated test copy is retained for diagnosis. The live system still needs the user's next update to receive 4.0.160.
