# GitHub publishing recovery

The 4.0.136 update encountered `remote: fatal error in commit_refs` after GitHub
received the objects. The local commit remained intact. A later non-force push of
the exact same revision succeeded, and `ls-remote` verified GitHub main matched it.
The CRLF conversion warnings were unrelated to the push rejection.

The publisher now verifies the remote reference before and after pushing an exact
commit. It retries recognized temporary GitHub failures up to three times, never
force-pushes, and stops on branch-policy/authentication errors. A lost connection
after successful acceptance is recognized by checking the remote reference.

Rerunning update.bat after an unpublished local release retains its version and
reuses the commit when there are no new staged changes. Divergent remote history
stops the updater before it changes version files. Existing source, commits and
recovery files are preserved. Deployment starts only after remote verification.

Run `powershell -NoProfile -File scripts/publish-git.test.ps1` for mock-based retry
regression tests. Run update.bat only when publishing and deployment are intended.
