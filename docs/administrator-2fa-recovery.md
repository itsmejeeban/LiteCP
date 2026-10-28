# Administrator 2FA recovery

This procedure applies after the pending security release is deployed and an administrator has enabled 2FA. The existing live installation does not yet have this feature.

1. At enrollment, save all ten one-use recovery codes offline. Do not store them in a website directory, mailbox on the same server, or support ticket. The panel displays them once and stores only hashes.
2. If the authenticator device is unavailable, sign in with the administrator password and one unused recovery code. Each code works only once; the same code cannot be reused after a restart.
3. After sign-in, open Security Dashboard and replace the recovery codes using the current password and another valid authenticator or recovery code. Save the new set offline. This signs out all panel sessions.
4. To replace the authenticator itself, sign in with a recovery code, disable 2FA using the current password and a remaining valid factor, then enroll the new authenticator. Disabling signs out all panel sessions. The password alone cannot disable 2FA.
5. If every factor and recovery code is lost, there is no password-only reset endpoint or browser bypass. Treat recovery as an offline server-owner incident. Verify ownership through an independent channel, preserve an encrypted copy of `/var/lib/litecp` and the current binary, review recent sign-in events, and arrange an approved maintenance procedure at the server console. Do not expose a temporary web bypass or send secrets through email/chat. The offline recovery implementation and acceptance tests are still pending; this step must not be treated as an automated operational procedure.

Any suspected compromise calls for changing the administrator password, rotating remaining recovery codes, ending other sessions, and reviewing server and mail logs from an independent source.
