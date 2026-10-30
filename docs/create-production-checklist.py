"""Generate the printable QA workbook. Requires reportlab and pypdf; no server actions."""
from pathlib import Path
from xml.sax.saxutils import escape
from reportlab.pdfgen import canvas
from reportlab.lib.pagesizes import A4
from reportlab.lib import colors
from reportlab.platypus import Paragraph
from reportlab.lib.styles import ParagraphStyle
from pypdf import PdfReader

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / 'output' / 'pdf' / 'LiteCP-Production-Test-Checklist-2026-10-30.pdf'

# Each line is a test action followed by the observable acceptance criterion.
DATA = r'''
Installation & first boot
Install on a fresh supported OS using the tar launcher | Download, extraction and all stages finish with a clear success result.
Try an invalid/private download without valid access | Clear failure; no false success or partial installation claimed as complete.
Observe long package and WAF build stages | Current stage is visible; errors remain readable and identify the failed step.
Record actual LiteCP, OS, Nginx, MySQL and PHP versions | Installed versions match the panel inventory and command-line results.
Install twice on separate disposable servers | Fresh administrator passwords differ; neither is a fixed default.
Inspect service environment and installer-created files | No plaintext administrator password or GitHub token is persisted.
Upgrade an existing test installation | Administrator credentials, websites and settings survive without duplicate records.
Open panel HTTPS on configured port 4321 | Login loads; deployed certificate and browser trust are recorded.
Verify resolver defaults and selected display timezone | Intended DNS servers and Asia/Kolkata default are applied to fresh installation.
Reboot after installation | Panel, database, web, mail and selected runtimes recover; settings persist.
SSH port transition & remote access
Start installation through SSH port 22 | Session remains usable while port changes; installation reaches completion.
Check configured SSH port after fresh installation | Intended port 369 listens; displayed connection instructions are correct.
Connect a second SSH session on the new port | Authentication succeeds before the old access path is removed.
Change SSH port from Security | New port is allowed in host firewall and permitted by SELinux policy where needed.
Try an occupied, invalid or restricted port | Change is rejected safely; the original working SSH connection remains available.
Simulate new-port validation failure on a disposable server | Previous SSH configuration/access is restored; error explains the failure.
Reboot after a successful port change | SSH listens on the saved port and firewall rules remain effective.
Check provider firewall separately | Provider rules allow the chosen SSH and panel ports; recovery console is available.
Inspect externally reachable ports | Only intended services are reachable; database and internal FastCGI stay private.
Keep a working session open during all access tests | No unexpected lockout; recovery steps are recorded before approval.
Login, sessions & administrator security
Sign in using valid and invalid credentials | Valid login works; invalid login fails without exposing account secrets.
Submit blank, long and malformed login values | Validation is readable; no crash, injection or raw stack trace.
Open an authenticated page in a fresh browser | Login is required; copied page URL alone grants no access.
Sign out, then revisit protected pages and APIs | Session is invalidated and protected data is inaccessible.
Change administrator password | Old password fails, new password works and existing access is revoked as intended.
Use password generation, eye and copy controls | Values are random, visibility toggles correctly and clipboard matches exactly.
Inspect login with 2FA disabled | Authenticator field is hidden until it is needed.
Set up 2FA, test valid/invalid/expired codes | Enrollment requires verification; only valid codes permit protected actions.
Use a recovery code twice on a disposable account | First use succeeds; reuse is rejected and codes never enter ordinary logs.
Test developer trusted-browser mode across restart | Only the approved browser stays trusted; a new browser still needs a password.
Sign out or change password with trusted-browser mode on | Trusted access is revoked; expired or disabled trust cannot restore a session.
Navigation, design & accessibility
Open every main menu and submenu | Correct page and active state appear; no blank page or console error.
Expand/collapse Website Tools, Email, Security and Settings | Labels, icons and chevrons remain aligned; state is predictable.
Collapse the sidebar and reopen it | Icons remain visible with usable labels/tooltips and a reachable toggle.
Search for a feature, setting, domain and mailbox | Relevant readable results open the intended destination.
Use Ctrl+K, keyboard arrows, Enter and Escape in search | Focus and selection work; results do not overlap or escape the viewport.
View at 100% and 200% zoom and narrow widths | Text and actions remain reachable without clipped controls.
Open each modal, use Tab and Escape, then close | Focus stays usable and returns sensibly; unsaved work is not silently lost.
Trigger success, information and failed notifications | Types are distinguishable; failures remain until manually dismissed.
Inspect dark-mode text and hover/focus/disabled states | Text is legible; no invisible labels, garbled characters or underlines on controls.
Open Documentation and search several topics | Manual sections load and describe current labels and workflows.
Check copy/generate/remove/close controls across pages | Controls are recognizable; destructive buttons are red and icons centered.
Overview & server inventory
Compare CPU, memory and disk with OS tools | Values use stated units and reflect the actual server within sampling delay.
Check usage bar thresholds around boundaries | Green at 50% or below, yellow above 50% through 70%, red above 70%.
Check disk badge when disk usage is high | Status reflects low free space; it does not misleadingly say Healthy.
Compare total websites and mailbox counts | Counts match current managed records after create/delete/refresh.
Review OS, hostname and server IP | Informational values are correct and use the intended blue styling.
Review running, stopped and missing services | Green means observed working; red means stopped/missing; unknown is explicit.
Compare installed PHP and container runtime inventory | Only actually detected versions are listed; disabled hosting is identified.
Restart a test service and refresh overview | Status changes accurately and recovers without a stale success indicator.
Website creation & lifecycle
Create a Regular PHP/HTML site using the two-step wizard | Default application, validation and final review are correct.
Create WordPress and licensed WHMCS test sites | Correct application profile is saved; provisioning is distinct from app installation.
Try duplicate, malformed and hostile domain names | Validation rejects unsafe/duplicate input without changing another site.
Inspect the website home layout | Files use /home/litecp/<domain>/public_html; private folders sit outside web root.
Request backups, logs, trash and configuration through HTTP | Private content is not publicly served.
Click the listed domain | Correct website opens using the intended protocol in a usable tab.
Search, paginate and change page size | Results/counts are correct; all actions remain available on each page.
Compare website size with the documented storage scope | Value is current, unit is clear and measurement scope is understood.
Edit a disposable site's domain | Vhost, files, PHP, cron and linked records remain coherent; collisions are blocked.
Suspend a disposable site and request HTTP and HTTPS | A dedicated suspension response appears; another domain's data is never served.
Unsuspend the site | Original content and intended services return; state survives a reboot.
Remove a disposable site using the stated removal scope | Confirmation explains retained/deleted data; results and analytics match that scope.
PHP versions & extensions
Select every installed PHP runtime on a disposable site | Runtime switches successfully and the actual web PHP version matches selection.
Switch WordPress/WHMCS runtime deliberately | Installed choices are available; application compatibility is independently checked.
Switch a legacy-runtime database site forward and back | Database authentication and PHP socket permissions remain functional.
Request a nonexistent PHP version through the API | Validation fails; current website keeps its previous working runtime.
Search extensions by name, module and package | Catalogue returns real packages without a read-only DNF log/cache error.
Filter the catalogue by each PHP version and state | Counts and installed status correspond to the selected runtime.
Install one optional extension on a disposable runtime | Correct runtime gains the module; other runtimes are unaffected.
Remove that optional extension | Module is removed safely; required/core modules remain protected.
Verify application-required extensions and ionCube where needed | Actual application health check passes for its chosen PHP runtime.
Fail a package operation on a test system | Clear error; no false installed state or broken runtime left unnoticed.
Application hosting: Node.js, Python & Ruby
Inspect each hosting type on a fresh installation | Optional hosting starts disabled and consumes no unnecessary resident runtime.
Enable each type separately | Feature becomes available and reports actual installation/download progress.
Select an available version and launch a sample application | Selected runtime serves the sample through the correct website/proxy.
Restart the sample process | Application returns without losing its configuration or files.
Test dependencies and startup command errors | Failure output is readable; secrets are redacted and other sites keep working.
Check process ownership, socket/port binding and resource limits | Application is isolated and does not gain root or cross-site access.
Reboot with a sample enabled | Intended application startup and runtime selection persist.
Disable the feature after the test | State accurately reports disabled; unrelated sites and stored data remain intact.
File Manager: browsing & layout
Open File Manager from Website Tools | Website selection is requested rather than silently opening the wrong site.
Open Files from a website row | The requested website home opens with the correct path and contents.
Navigate Home, Up, Back, Forward and Reload | Paths and history are correct; no folder is skipped or mixed between sites.
Expand nested folders, collapse all and resize the tree pane | Tree controls and splitter work without overlap or unusable widths.
Open public_html and inspect folder/file icons | Public web root is distinguishable; file types have readable icons.
Sort name, size, modified time, type and permissions | Order is consistent and folder selection is preserved sensibly.
Show/hide dotfiles and save Preferences | Hidden files and startup folder follow preferences after reopening.
Search current folder and broader supported scopes | Scope is clear and results navigate to the correct file.
Select all/unselect, multi-select and use keyboard actions | Correct items are selected; disabled actions match supported selection.
Right-click near file names and screen edges | Menu opens beside the cursor, stays on-screen and exposes applicable actions.
Enter/exit full screen | Only File Manager expands; header/logo and all controls fit the available height.
Change website while a request is loading | A stale response cannot populate or modify the newly selected website.
File Manager: file operations
Create a new file and folder in a named QA directory | Correct items appear at the chosen destination with safe permissions.
Copy a file and a nested directory | Contents match originals; existing destination conflicts require an explicit choice.
Move and rename QA files/folders | Paths update correctly; unsupported/conflicting destinations fail safely.
Upload multiple QA files including a large test file | Progress and errors are accurate; completed bytes match originals.
Cancel/interupt a QA upload | No false completion; partial-file handling is clear and existing files remain intact.
Download one file and a supported selection | Names, bytes and expected archive contents match the requested selection.
Edit PHP, CSS, JS, TXT and HTML through toolbar/right-click | Edit is offered for text files; correct file opens and saves.
Use find/replace, wrap, language and editor full screen | Controls work; saving preserves the intended text and encoding.
Use HTML editor on a disposable HTML file | Preview/edit/save behavior is correct; script content cannot execute in panel context.
View images and text; attempt binary edit | Supported previews work; unsafe/unsupported edits are blocked clearly.
Change QA file permissions and verify on disk | Octal mode is correct and does not affect unrelated files.
Trash then restore QA files; test permanent delete separately | Restore returns correct bytes/path; permanent removal requires confirmation.
File Manager: archives & isolation
Compress QA content using each offered archive format | Archive opens independently and contains the intended relative paths.
Extract each offered format into a new QA folder | Files and nested folders match; progress and result are accurate.
Test archive conflicts with existing QA files | No silent overwrite beyond the explicitly confirmed operation.
Try traversal entries such as ../ and absolute paths | Extraction is rejected; no file is written outside the permitted root.
Try symlink escapes and links to another site's files | Preview, copy, edit and download cannot bypass website confinement.
Try malformed, oversized-expansion and encrypted archives | Operation stops safely with a useful error and resource controls.
Try another website ID or server-explorer ID on write APIs | Authorization/confinement checks reject inappropriate writes.
Save an edit after another session changes the file | Conflict is detected or clearly handled; stale text does not silently replace changes.
Server File Explorer & terminal
Open Server File Explorer and browse persistent server paths | Correct server directories load; special/virtual paths are handled safely.
Search, sort, preview and download allowed server files | Read operations return the requested content and show failures honestly.
Attempt create/edit/delete/upload through Server File Explorer | Read-only restriction is enforced by the server, not only hidden buttons.
Open Terminal from a signed-in administrator session | Visible icon opens a working interactive session with clear privilege context.
Run pwd, printf, cd and an interactive harmless command | Output, input and current directory behave like a real terminal.
Use arrows, backspace, Tab, Ctrl+C and terminal resize | Editing, interrupt and screen dimensions work without duplicated input.
Switch tabs and return during output | Terminal state and output remain usable; no unexpected session recreation.
Disconnect and test idle expiry, logout and a second browser | Sessions close/expire as designed; unauthorized browsers cannot attach.
Inspect shell scope and root access before production | Actual privilege boundary is documented and accepted; no hidden unrestricted access.
Databases & phpMyAdmin
Create a disposable database with generated names/password | Database/user exist, names are valid and copy controls match values.
Try duplicate, invalid and injection-like identifiers | Rejected safely; no existing schema/user is modified.
Connect locally using the new database user | User can access only its assigned database and required privileges.
Click the database name from the panel | phpMyAdmin opens in a new tab and signs in to the intended database.
Open tables, run a harmless query, export and import QA data | Structure and data are correct without cross-database access.
Test phpMyAdmin handoff after expiry and replay | Expired/reused credentials or tokens cannot grant unauthorized access.
Check configured import limit and request overhead | UI reflects the requested 5 GiB file limit and surrounding limits permit it.
Import a representative large QA dump with monitoring | Import completes accurately or reports a recoverable failure; resources remain usable.
Reset the QA user's password | New password works; old password fails and dependent application updates are handled.
Delete only a disposable database using confirmation | Correct database/user are removed according to the documented scope.
Website cron jobs
Open Cron jobs for WordPress, WHMCS and a regular site | One clear interface contains default application cron and custom jobs.
Add two harmless jobs to one site | Both schedules persist independently without duplicates.
Choose common presets and one custom cron expression | Saved schedule matches the intended frequency and server timezone.
Edit, disable and re-enable a QA job | Only that job changes; scheduler output and UI state agree.
Use Run to execute a harmless QA command immediately | Output, exit status and failure message reflect the actual command.
Test a slow command and an invalid command | Timeout/concurrency handling is clear; panel stays responsive.
Switch the site's PHP version | PHP-based application cron uses the correct intended runtime afterward.
Remove one QA job | Confirmation is explicit and other jobs continue unchanged.
Reboot and inspect the saved jobs | Jobs persist once and execute under the intended website identity.
Website FTP / FTPS
Open FTP for a selected website | Host/IP, port, encryption mode and directory are correct.
Generate and copy username/password | Buttons work, valid random credentials are produced and text is readable.
Create an account and record the one-time details | Username, password, host/IP and port are available once without plaintext storage.
Connect with explicit FTPS from an external client | TLS certificate is checked and login succeeds on the configured ports.
Upload/download a QA file through passive mode | Bytes match; port 21 and intended passive range work externally.
Try plaintext login and another website's directory | Required encryption and website confinement are enforced.
Edit password/account settings | Saved changes take effect; previous password fails as expected.
Remove a QA FTP account | New logins are blocked; handling of existing sessions is documented.
SSL certificates & domain routing
Install SSL for a DNS-ready disposable domain | Certificate is issued for the correct names and Nginx configuration validates.
Attempt issuance for a domain with incorrect DNS | Useful failure; no broken vhost or misleading Active status.
Request HTTP then HTTPS for each hosted domain | Correct content is served; redirects do not loop or switch websites.
Check certificate hostname, issuer, chain and expiry externally | Browser trust and chain validation succeed for production names.
Request unknown hosts and an unconfigured HTTPS name | No unrelated customer's website, secrets or default content leaks.
Suspend a site with and without SSL | Both protocols show suspension safely, never another site's content.
Test renewal using the certificate tool's safe dry-run mode | Renewal validation succeeds and reload behavior is recorded.
Verify renewal scheduling survives reboot | Automation is enabled once and errors are visible to the administrator.
Email domains, mailboxes & aliases
Use each Email submenu independently | Correct form/list opens; obsolete repeated DNS information cards are absent.
Create a mailbox for an existing hosted domain | Domain appears automatically and mail routing is created consistently.
Add an email-only domain and create a mailbox | Website hosting is not required; routing and mailbox records agree.
Try duplicate/invalid domains, addresses and weak inputs | Clear validation; no partial or duplicate mail-domain records.
Generate a mailbox password, set quota and create account | Password handling and quota match the requested values.
Edit/reset a disposable mailbox password | New login works; old password fails; mail content remains intact.
Create an alias to another approved test recipient | Mail forwards once to the intended address without an unintended loop.
Remove a QA alias/mailbox/domain in the proper order | Confirmation explains data scope; unrelated accounts remain operational.
View DNS and mail-client configuration | Hostnames, ports, MX, SPF, DKIM and DMARC values match the domain/server.
Webmail, delivery & SMTP connectivity
Click Log in beside a mailbox as panel administrator | Correct mailbox opens automatically without requesting its password.
Test expired/replayed webmail handoff and unauthorized access | Invalid handoffs fail; one mailbox cannot be substituted for another.
Send and receive between two QA mailboxes | Delivery succeeds and messages appear in the correct folders.
Send to and receive from an approved external test address | Both directions work; message headers and delays are recorded.
Inspect SPF, DKIM, DMARC, PTR and mail TLS | Results align with configured identity; spam placement is assessed separately.
Use IMAPS 993 and SMTP submission 465/587 externally | TLS, authentication and mailbox quota enforcement work.
Attempt unauthenticated relay to an external address | Relay is denied; legitimate authenticated submission still works.
Open Email Delivery and filter/search a QA message | Sender, recipient, time and delivered/deferred/bounced status are accurate.
Test SMTP Port Check for ports 25 and 465 | Reachability/TLS errors are shown; timeout is not falsely proven to be provider blocking.
Check deferred queue/retry and invalid-recipient handling | Failures are visible and do not disappear as successful delivery.
Website backups & manual restore
Create a complete QA website backup | Named downloadable file appears with size/time and a clear completion state.
Inspect nested ZIP structure independently | Outer ZIP contains public_html.zip and databases.zip with expected content.
Include hidden files and registered databases in QA data | Backup includes intended hidden content and database rows.
Download a backup and compare checksum | Download is complete, authorized and matches the stored file.
Upload a valid manual website backup | Picker is usable; validation and inspection precede destructive restoration.
Restore into a disposable empty target | Files, database rows and application connection details are restored correctly.
Restore into a populated QA target | Explicit overwrite scope/confirmation is required; recovery path is available.
Upload corrupt/traversal/oversized-expansion archives | Rejected safely without escaping target paths or exhausting the server.
Delete one QA backup | Only the selected archive is removed after confirmation.
Run a backup while the site changes content | Consistency limits are understood; production backup method meets recovery needs.
Server backups, schedules & remote storage
Create a server backup from Backups | Success occurs only after files and database capture complete.
Create two schedules with different frequencies | Both are saved independently, show next-run time and run as configured.
Edit, disable and delete a QA schedule | Only the selected schedule changes; no duplicate scheduler entries.
Set a schedule retention count and exceed it with QA backups | Only that schedule's oldest backups are removed after a successful new backup.
Force a scheduled backup failure on a test target | Failure is reported; previous recovery backups are retained.
Add FTP, FTPS and SFTP destinations separately | Correct protocol/port/path validation and credentials are used.
Test SFTP fingerprint entry and automatic trust workflow | Identity approval is clear; a changed host key is rejected.
Try wrong credentials or an unreachable destination | Useful failure; local backup remains available and no false remote success.
Verify remote backup bytes/checksum and restore from it | Remote copy is complete and independently recoverable.
Inspect backup settings, logs and exported metadata | Credentials are encrypted/redacted and absent from public responses.
System Snapshot & disaster recovery
Create a System Snapshot from a disposable environment | Archive completes with a manifest and clear coverage of included data.
Check settings, sites, databases, mail and linked metadata | Every promised component is present; exclusions are explicitly documented.
Upload a snapshot and select one from an allowed server path | Both sources validate correctly; unauthorized paths and unsafe links are rejected.
Restore into a separate fresh test server | Intended state/files/databases recover with explicit overwrite confirmation.
Check new server IP, DNS, certificates and service paths | Server-specific values are reconciled rather than blindly trusting old values.
Verify login, 2FA/trust handling and encrypted credentials | Access remains recoverable; old sessions/secrets are handled intentionally.
Open applications, cron, mail and FTP after restore | Each included service works with the restored identities and permissions.
Interrupt a QA restore and follow recovery instructions | Failure state is honest and a tested recovery path is available.
Record actual recovery time and latest recoverable point | Measured RTO/RPO meet the deployment's agreed requirements.
Migration: cPanel, Plesk & LiteCP
Connect to each supported source using password and key | Authentication, port and host-key trust work without exposing secrets.
Discover accounts and search/select multiple accounts | Full domains/source IP are shown; selection and drag/drop remain usable.
Start migration for a new disposable domain | Capture, transfer, verify, extract, preflight and restore proceed automatically.
Migrate onto an existing QA domain | Overwrite is requested before replacement; cancellation preserves the existing site.
Observe progress and live/completed job lists | Only queued/running jobs are live; percentage reaches 100 only on actual completion.
Restart the panel during a QA migration | Work resumes or reports a recoverable interruption without duplicate destructive action.
Cancel an individual job and then a separate batch | Cancel/Cancel all stop intended work and report remaining/recovery state accurately.
Compare migrated files, hidden content and database row counts | Destination matches source within documented migration scope.
Open the migrated app before DNS cutover using host mapping | Correct site works; no HTTP 500, wrong database or cross-domain content.
Check PHP/extensions, rewrites, paths, cron, mail and SSL scope | Supported items work; unsupported items are clearly listed, never implied migrated.
Verify source-created archive cleanup after validated transfer | Only LiteCP-created export artifacts are removed; original source data is untouched.
Clean finished migration temporary data from the panel | Only eligible temporary archives/staging are removed; restored websites/history survive.
Firewall & SSH protection
Open full-width Managed rules and Add rules dialog | Forms are aligned; editing targets the correct rule.
Apply LiteCP Firewall on a disposable host | Only required configured services are allowed and current administrator access survives.
Add/edit/remove a scoped inbound allow rule | External connectivity matches protocol, port and source restriction.
Add/edit/remove a scoped outbound rule | New host-originated traffic follows policy; established replies behave as documented.
Try blocking protected SSH/panel/web core access | Safety checks prevent accidental administrative lockout.
Check optional mail/FTPS ports when features are disabled | Exposure matches actually enabled services, not blanket open ports.
Reboot and reapply the firewall profile | Rules persist, no duplicates accumulate and service access remains correct.
Test Fail2ban using a disposable source and recovery access | Repeated failure is handled without banning the only administrator connection.
Security Dashboard, WAF & malware
Compare Security Dashboard observations with OS evidence | Reported protection is accurate; unknown checks remain explicitly unknown.
Inspect panel and helper process privileges | Web-facing privilege boundary is documented and verified; root-risk findings are resolved.
Check SELinux mode and relevant denials | Intended enforcing policy works with sites, PHP, mail, backups and containers.
Verify WAF engine/rules after installation | Actual Nginx module and rules are loaded; UI availability alone is insufficient.
Use WAF Off, Detect and Block on a QA site | Harmless request works; authorized benign test rule demonstrates detection/blocking.
Check WAF logs and common app requests | Evidence matches mode and false positives do not break legitimate application flows.
Install/update scanner definitions and scan a QA site | Scan starts, finishes and reports errors/skipped files honestly.
Scan an approved EICAR test fixture | Test fixture is detected; no ordinary file is automatically deleted.
Run Scan All Server and schedule a QA scan | Scope, exclusions, timing, results and resource impact are clear and correct.
Cancel/fail a large scan on a test host | No false clean result; panel remains responsive and previous findings remain available.
Analytics, storage & activity history
Open live/hourly/daily/weekly/monthly usage graphs | Samples, axes, units and timezone are correct and labels are sharp.
Create a short controlled CPU/memory workload | Graph reflects the change after sampling; missing data is not fabricated.
Compare root filesystem total/used/free with df | Chart and overview agree on the filesystem and units being measured.
Browse directory-wise usage across the server | Paths/sizes are shown; virtual mounts, links and inaccessible paths are handled clearly.
Inspect LiteCP website/software/backup/migration storage categories | Categories explain their scope and do not misleadingly double-count overlaps.
Hover the disk chart including free space | Tooltip identifies path/category, size and proportion; free space is distinct.
Remove/detach a QA site and refresh usage | Active-site view excludes it; retained physical files remain honestly accounted for elsewhere.
Create then clean eligible migration QA artifacts | Usage decreases after refresh within filesystem/cache timing limits.
Inspect Recent Activities after important actions | Correct event, target and timezone appear without passwords/tokens.
Test Forever and time-based activity retention on QA records | Retention matches policy; Delete everything requires explicit confirmation.
Services, settings & updates
Open each settings submenu | Password, email, timezone, hostname, DNS, reboot, developer mode and Update are separate.
Save administrator email/timezone and refresh | Values persist and timestamps show correct UTC offset without corrupt characters.
Change a QA hostname | Valid hostname applies; scope explains DNS/mail identity separately.
Save DNS resolvers and reboot | Effective resolution works; requested resolver persistence policy is honored.
Start/restart each supported service on a disposable system | Actual state changes and failures are reported; unrelated services remain operational.
Use reboot confirmation on a disposable server | Accidental click does not reboot; confirmed reboot recovers all intended services.
Check updates with no update and with a newer test release | Installed/available versions are accurate and checks fail clearly when unavailable.
Run an explicitly authorized LiteCP-only update | Binary/settings recovery copy exists; sites stay available and panel version advances.
Exercise update.bat prerequisite and failure paths | Missing Git/SSH/pager errors are clear; no false push/deploy success or destructive reset.
Verify rollback after a failed disposable update | Previous working panel returns with data and authentication intact.
Production reliability & isolation
Run representative traffic against disposable sites | Response times, errors and resource use meet a written target below.
Run simultaneous file/backup/migration requests | Concurrency limits work; no corruption, duplicate jobs or wrong-site actions.
Test low disk and low memory only on an isolated test host | Operations fail safely with visible errors; existing data remains recoverable.
Test panel APIs without session and with invalid CSRF | Sensitive reads/writes are rejected consistently, including file/terminal endpoints.
Test stored/reflected hostile text in names, logs and filenames | Content is escaped; browser scripts cannot execute in the administrator session.
Inspect TLS/cookies/cache and downloaded sensitive responses | Secure session behavior and private-data handling match the threat model.
Attempt cross-site file, process and database access | Website identities cannot read/write another tenant's protected resources.
Reboot and repeat critical smoke tests | Login, HTTP/HTTPS, database, cron, mail and backup access remain operational.
Inspect operational logs and external monitoring alerts | Failures can be detected and investigated without leaking credentials.
Run a sustained representative workload and recovery drill | No unexplained growth/crash; backup restore and rollback are proven independently.
'''

groups = []
for line in DATA.strip().splitlines():
    if ' | ' not in line:
        groups.append((line.strip(), []))
    else:
        groups[-1][1].append(tuple(line.split(' | ', 1)))

W,H=A4
M=37
INK=colors.HexColor('#172839')
GRAY=colors.HexColor('#52606b')
BLUE=colors.HexColor('#205b85')
style=ParagraphStyle('body',fontName='Helvetica',fontSize=9,leading=12,textColor=INK)
small=ParagraphStyle('small',parent=style,fontSize=8,leading=10)
def para(c,text,x,y,width,sty=style):
    p=Paragraph(text,sty); _,height=p.wrap(width,1000); p.drawOn(c,x,y-height); return height
def page(c,title,kicker='PRODUCTION ACCEPTANCE WORKBOOK'):
    c.setFillColor(BLUE); c.rect(M,H-41,W-2*M,4,fill=1,stroke=0)
    c.setFont('Helvetica-Bold',8);c.drawString(M,H-57,'LiteCP  |  '+kicker)
    para(c,escape(title),M,H-72,W-2*M,ParagraphStyle('title',parent=style,fontSize=20,leading=24,fontName='Helvetica-Bold'))
    c.setStrokeColor(colors.HexColor('#c8cfd4'));c.line(M,37,W-M,37)
    c.setFillColor(GRAY);c.setFont('Helvetica',8)
    c.drawString(M,24,'Manual verification - untested until completed by the tester')
    c.drawRightString(W-M,24,str(c.getPageNumber()))
def line(c,label,y):
    c.setFont('Helvetica',10); c.setFillColor(INK);c.drawString(M,y,label)
    c.setStrokeColor(colors.HexColor('#aab3bb')); c.line(M,y-12,W-M,y-12)
def boxes(c,x,y):
    c.setStrokeColor(GRAY);c.setFillColor(INK)
    for i,label in enumerate(('P','F','B','NA')):
        xx=x+i*25;c.rect(xx,y-1,7,7,fill=0,stroke=1);c.setFont('Helvetica',7);c.drawString(xx+9,y,label)

def generate():
    OUT.parent.mkdir(parents=True,exist_ok=True)
    if OUT.exists(): raise SystemExit('Output already exists; choose a new filename to preserve it.')
    c=canvas.Canvas(str(OUT),pagesize=A4)
    c.setTitle('LiteCP Production Test Checklist');c.setAuthor('HostingTom / LiteCP QA')
    total=sum(len(items) for _,items in groups)
    page(c,'Complete production test checklist')
    y=H-125
    for text in [f'<b>{len(groups)} categories / {total} individual checks</b>. Print on A4 at actual size. Each row includes an action, expected result and a place to record its outcome.',
      '<b>Result key:</b> P = Pass; F = Fail; B = Blocked; NA = Not applicable. Leave untested rows blank. Record evidence and defect IDs beside each result.',
      '<b>Purpose:</b> verify the installed build before accepting real customers. This is a test plan, not proof that LiteCP is production-ready or that all listed capabilities are currently implemented.',
      '<b>Safety:</b> use disposable sites, databases, mailboxes and a separate restore server. Obtain explicit approval before deletion, overwriting, firewall/SSH changes, reboot or fault injection. Never stress-test a customer server.',
      '<b>Optional features:</b> mark NA only when intentionally out of scope, with a written reason. A missing required feature is Fail or Blocked, not NA.',
      '<b>Evidence:</b> use screenshots, timestamps, sanitized logs, HTTP status, checksums and database counts. Never record passwords, tokens, private keys or recovery codes in this workbook.',
      '<b>Scope:</b> prepared from the local LiteCP v2.0.1 installer/source and requested workflows on 30 October 2026. Enter the actual deployed version below; rerun affected tests after each fix.']:
        y-=para(c,text,M,y,W-2*M)+13
    for label in ['Tester / date:','Server / environment / OS:','LiteCP version / commit / installation method:','Panel URL / SSH port / recovery-console reference:','QA domains / approved external mail recipient:']:
        line(c,label,y);y-=39
    c.showPage()
    page(c,'Test order & coverage index')
    y=H-118
    y-=para(c,'<b>Before testing:</b> save a recovery backup; confirm console access; agree a maintenance window; create QA data and a second restore server. Test installation/access first, then features, then security/recovery and load.',M,y,W-2*M)+16
    c.setFont('Helvetica-Bold',9); c.drawString(M,y,'Category');c.drawRightString(W-M,y,'Page');y-=19
    for n,(name,_) in enumerate(groups,1):
        c.setFont('Helvetica',9);c.drawString(M,y,f'{n:02d}  {name}');c.drawRightString(W-M,y,str(n+2));y-=18
    y-=8;para(c,'<b>Repeat matrix:</b> repeat the applicable checklist on each advertised OS/architecture and upgrade path. Use at least Chrome plus another browser, desktop plus narrow viewport, and fresh plus upgraded installations. Repeat migration tests separately for cPanel, Plesk and LiteCP; repeat each enabled runtime/protocol.',M,y,W-2*M)
    c.showPage()
    for num,(name,items) in enumerate(groups,1):
        page(c,f'{num:02d}  {name}')
        y=H-116
        y-=para(c,'Tester/date: _______________________  Build: _______________________<br/>Tick one: P / F / B / NA. Use the line for evidence, defect reference or an NA reason.',M,y,W-2*M,small)+14
        available=y-92
        rowh=min(65,available/len(items))
        for idx,(action,expected) in enumerate(items,1):
            c.setStrokeColor(colors.HexColor('#cbd2d8'));c.line(M,y,W-M,y)
            yy=y-8
            para(c,f'<b>{num:02d}.{idx:02d} {escape(action)}</b>',M+5,yy,W-2*M-116,small)
            boxes(c,W-M-102,yy-8)
            a=Paragraph('<b>'+escape(action)+'</b>',small);_,ah=a.wrap(W-2*M-116,1000)
            eh=para(c,'Expected: '+escape(expected),M+5,yy-max(ah,17)-3,W-2*M-10,small)
            note_y=y-rowh+7
            c.setFont('Helvetica',7);c.setFillColor(GRAY);c.drawString(M+5,note_y,'Evidence / issue: __________________________________________________________________________________')
            y-=rowh
        para(c,'Category result:  Pass / Fail / Blocked     Open defects: __________     Retest date: __________',M,70,W-2*M,small)
        c.showPage()
    page(c,'Defect register - photocopy as needed')
    y=H-120
    for n in range(1,5):
        for label in [f'Defect {n} - ID / checklist reference / severity:', 'Steps and actual result:', 'Expected result / evidence location:', 'Owner / fix build / retest result:']:
            line(c,label,y);y-=35
        y-=12
    c.showPage()
    page(c,'Production approval & recovery sign-off')
    y=H-120
    for text in ['<b>GO / NO-GO:</b> all required checks must Pass. Any failed/blocked authentication, isolation, data-integrity, backup/restore, remote-access or critical security check means NO-GO until resolved and retested.',
      '<b>Severity:</b> Critical = unauthorized control/data loss/cross-tenant access; High = major feature or recovery failure; Medium = usable workaround; Low = cosmetic. Do not accept unresolved Critical or High issues for production.',
      '<b>Recovery evidence:</b> a downloaded backup is not sufficient. Restore on another server and verify applications, databases, credentials, mail and operational services. Record what the backup excludes.',
      '<b>Monitoring:</b> confirm disk/inode, memory, service, certificate, queue and backup-failure alerts reach an owner. Confirm available capacity and who handles incidents.']:
        y-=para(c,text,M,y,W-2*M)+16
    for label in ['Required checks: ____  Pass: ____  Fail: ____  Blocked: ____  NA with reason: ____',
      'Workload target / tested concurrency / duration / error rate:',
      'Restore evidence / measured recovery time (RTO) / data loss window (RPO):',
      'Remaining accepted Low/Medium issues / owner / due date:',
      'Rollback procedure / last verified date / evidence:',
      'Monitoring and incident-response owner:',
      'Final decision: GO / NO-GO     Build approved:',
      'Tester signature / date:', 'Administrator approval / date:']:
        line(c,label,y);y-=38
    c.save()
    reader=PdfReader(OUT)
    assert len(reader.pages)==len(groups)+4
    text='\n'.join(p.extract_text() or '' for p in reader.pages)
    for n,(_,items) in enumerate(groups,1):
        for i in range(1,len(items)+1): assert f'{n:02d}.{i:02d}' in text
    print(f'PASS: {len(reader.pages)} pages; {len(groups)} categories; {total} checks; all check IDs verified.\n{OUT}')

if __name__=='__main__': generate()
