# Interactive terminal

Installed privilege-separated panels intentionally do not expose a browser
shell. The Terminal page checks availability before enabling Connect and directs
administrators to their SSH client. This restriction prevents the web service
from obtaining a root shell through the management broker.

The behavior below applies only to runtimes where browser shells are supported.

Management → Terminal → Connect opens a persistent bash session in a Linux
pseudo-terminal. The shell runs with the LiteCP service account's privileges.
Directory changes, shell variables, interactive prompts and command history
remain in the session. xterm.js renders ANSI colors, cursor movement and
full-screen applications. Tab, arrow keys, Ctrl+C and Ctrl+D go to the shell.
Use the browser's normal copy/paste shortcuts for terminal selections and input.

Switching panel pages retains the shell. Disconnect ends the shell. Reloading
the page closes the old session when possible; disconnected browsers expire
after five minutes without requests. Eight sessions are allowed per panel
process. Panel restart ends sessions. Output history is bounded at 2 MiB on the
server and 10,000 lines in the browser. Credentials and terminal output are not
saved to disk by the terminal implementation; shell/application history policies
still apply normally.

Requires Linux, Python 3 and bash. Browser assets are vendored locally under
web/vendor/xterm with their licenses; no runtime CDN connection is needed.
Session access uses administrator authentication, same-origin mutation checks,
an unpredictable session identifier and browser-cookie ownership.

## Verification after running update.bat

- Connect; run pwd, cd /tmp, pwd and confirm the working directory persists.
- Type a partial command and use Tab completion and arrow-key history.
- Run sleep 60, press Ctrl+C, and confirm the prompt returns promptly.
- Run top or nano and verify live updates, keyboard handling and resize.
- Paste multiline text into cat; use Ctrl+D to finish input.
- Switch panel tabs and return; confirm the output/session remains.
- Disconnect and confirm the shell ends. A different signed-in browser must
  not be able to use the first browser's terminal ID.

Existing Makefile and update.bat remain the build/update entry points.
