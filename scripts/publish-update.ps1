# Run through update.bat. Launching it authorizes publishing and deployment.
# PuTTY batch mode retains cached host-key checks; unknown/changed keys fail safely.
# -no-antispoof removes only PuTTY's interactive Return-key pause. Requires Git and Windows OpenSSH; Go is required on the server.
param([string]$Server = '162.55.50.122', [int]$Port = 369, [string]$User = 'root', [switch]$CheckOnly, [string]$HostKey = $env:LITECP_UPDATE_HOSTKEY)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'publish-git.ps1')
Set-Location (Split-Path $PSScriptRoot -Parent)
if ($env:LITECP_UPDATE_PORT) { $Port = [int]$env:LITECP_UPDATE_PORT }
function Run-Step([string]$Tool, [string[]]$Arguments) {
    & $Tool @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Tool failed with exit code $LASTEXITCODE" }
}
# Only use for idempotent connection/transfer operations, never deployment.
function Run-TransferStep([string]$Tool, [string[]]$Arguments) {
    for ($attempt = 1; $attempt -le 3; $attempt++) {
        $previousPreference = $ErrorActionPreference
        try {
            $ErrorActionPreference = 'Continue'
            $messages = @(& $Tool @Arguments 2>&1)
            $code = $LASTEXITCODE
        } finally { $ErrorActionPreference = $previousPreference }
        $messages | ForEach-Object { Write-Host "$_" }
        if ($code -eq 0) { return }
        $transient = ($messages -join "`n") -match '(?i)connection timed out|connection reset|connection unexpectedly closed|connection refused|network error|connection closed by remote host'
        if (!$transient -or $attempt -eq 3) {
            if ($transient) {
                throw "Cannot connect reliably to ${Server}:$Port after $attempt attempt(s). Check the SSH service, configured port, server firewall and provider firewall through the provider console. Changing the password or host-key fingerprint will not fix a connection timeout."
            }
            throw "SSH command failed (exit $code). Review the error above. If the host key changed, verify it through the provider console before updating LITECP_UPDATE_HOSTKEY. Authentication and host-key failures are not retried."
        }
        Write-Host "Temporary SSH connection failure. Retrying safe transfer step ($($attempt + 1)/3)..." -ForegroundColor Yellow
        Start-Sleep -Seconds 2
    }
}
function Find-UpdateTool([string]$Name, [string[]]$Candidates) {
    $command = Get-Command $Name -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($command) { return $command.Source }
    foreach ($candidate in $Candidates) {
        if ($candidate -and (Test-Path -LiteralPath $candidate -PathType Leaf)) { return $candidate }
    }
    throw "$Name was not found. Install Git for Windows and Windows OpenSSH, then reopen update.bat."
}
$published = $false
try {
    Write-Host '[1/6] Checking Git and SSH tools...'
    $gitExe = Find-UpdateTool 'git.exe' @(
        "$env:ProgramFiles\Git\cmd\git.exe",
        "${env:ProgramFiles(x86)}\Git\cmd\git.exe",
        "$env:LOCALAPPDATA\Programs\Git\cmd\git.exe",
        "$env:USERPROFILE\.cache\codex-runtimes\codex-primary-runtime\dependencies\native\git\cmd\git.exe"
    )
    $sshExe = Find-UpdateTool 'ssh.exe' @("$env:WINDIR\System32\OpenSSH\ssh.exe", "$env:WINDIR\Sysnative\OpenSSH\ssh.exe")
    $scpExe = Find-UpdateTool 'scp.exe' @("$env:WINDIR\System32\OpenSSH\scp.exe", "$env:WINDIR\Sysnative\OpenSSH\scp.exe")
    $plinkExe = $null
    $pscpExe = $null
    if ($env:LITECP_UPDATE_PASSWORD) {
        $plinkExe = Find-UpdateTool 'plink.exe' @("$env:ProgramFiles\PuTTY\plink.exe", "${env:ProgramFiles(x86)}\PuTTY\plink.exe")
        $pscpExe = Find-UpdateTool 'pscp.exe' @("$env:ProgramFiles\PuTTY\pscp.exe", "${env:ProgramFiles(x86)}\PuTTY\pscp.exe")
    }
    $env:PATH = (Split-Path $gitExe) + ';' + (Split-Path $sshExe) + ';' + (Split-Path $scpExe) + ';' + $env:PATH
    # Bundled Git may not include less.exe. Never launch a pager in this workflow.
    # Use previously configured credentials; fail clearly instead of waiting for input.
    $env:GIT_TERMINAL_PROMPT = '0'
    $env:GCM_INTERACTIVE = 'Never'
    $env:GIT_PAGER = 'cat'
    $env:PAGER = 'cat'
    $env:GIT_CONFIG_COUNT = '1'
    $env:GIT_CONFIG_KEY_0 = 'core.pager'
    $env:GIT_CONFIG_VALUE_0 = 'cat'
    Write-Host "Git: $gitExe"
    Write-Host "SSH: $sshExe"
    $origin = (& git remote get-url origin).Trim()
    if ($LASTEXITCODE -ne 0 -or $origin -ne 'https://github.com/itsmejeeban/LiteCP.git') { throw 'Expected LiteCP GitHub origin was not found.' }
    $branch = (& git branch --show-current).Trim()
    if ($LASTEXITCODE -ne 0 -or $branch -ne 'main') { throw 'Switch to main before publishing this project.' }
    if (@(& git diff --name-only --diff-filter=U).Count) { throw 'Resolve Git merge conflicts first.' }
    if ($CheckOnly) {
        Run-Step 'git' @('--version')
        Run-Step 'git' @('--no-pager','diff','--cached','--check','--','.',':(exclude)output/**')
        Write-Host 'CHECK PASSED. Tools and repository are ready. No files, commits or servers were changed.'
        exit 0
    }
    $destination = "$User@$Server"
    $hostKeyArgs = @()
    if ($HostKey) {
        if ($HostKey -notmatch '^SHA256:[A-Za-z0-9+/]{43}=?$') { throw 'LITECP_UPDATE_HOSTKEY must be a verified SHA256 SSH fingerprint.' }
        if (!$plinkExe) { throw 'Fingerprint pinning requires PuTTY password mode. OpenSSH continues to use its known_hosts checks.' }
        $hostKeyArgs = @('-hostkey', $HostKey)
    }
    Write-Host 'Checking SSH identity and login before publishing...'
    try {
        if ($plinkExe) {
            Run-TransferStep $plinkExe (@('-batch','-no-antispoof') + $hostKeyArgs + @('-P',"$Port",'-pw',$env:LITECP_UPDATE_PASSWORD,$destination,'true'))
        } else {
            Run-Step 'ssh' @('-o','BatchMode=yes','-o','ConnectTimeout=15','-p',"$Port",$destination,'true')
        }
    } catch {
        throw "SSH preflight failed. No changes were published. $($_.Exception.Message)"
    }
    Write-Host '[2/6] Reviewing local changes...'
    Run-Step 'git' @('status','--short')
    Write-Host 'This publishes all non-ignored changes to GitHub main and deploys that commit to LiteCP.'
    Write-Host 'Starting automatic update. No confirmation input is required.'
    $releaseRevision = (& git rev-parse HEAD).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Cannot inspect local release commit.' }
    $remoteRevision = Get-PublishRemoteMain
    $retryingRelease = $releaseRevision -ne $remoteRevision
    if ($retryingRelease) {
        & git merge-base --is-ancestor $remoteRevision $releaseRevision
        if ($LASTEXITCODE -ne 0) { throw 'GitHub main has changes outside the local release history. Reconcile the branches before publishing; no reset or force-push was performed.' }
    }
    $mainPath = Join-Path (Get-Location) 'main.go'
    $installPath = Join-Path (Get-Location) 'scripts/install.sh'
    $main = [IO.File]::ReadAllText($mainPath)
    $match = [regex]::Match($main, 'var version = "(\d+)\.(\d+)\.(\d+)"')
    if (!$match.Success) { throw 'Cannot determine current LiteCP version.' }
    # Preserve a version already changed locally, including an explicit release version.
    $committedMain = (& git show 'HEAD:main.go') -join "`n"
    if ($LASTEXITCODE -ne 0) { throw 'Cannot inspect the published version.' }
    $committedVersion = [regex]::Match($committedMain, 'var version = "(\d+\.\d+\.\d+)"')
    if (!$committedVersion.Success) { throw 'Cannot determine the committed LiteCP version.' }
    $version = '{0}.{1}.{2}' -f $match.Groups[1].Value,$match.Groups[2].Value,$match.Groups[3].Value
    if (!$retryingRelease -and $version -eq $committedVersion.Groups[1].Value) {
        $version = '{0}.{1}.{2}' -f $match.Groups[1].Value,$match.Groups[2].Value,([int]$match.Groups[3].Value + 1)
    }
    $utf8 = New-Object System.Text.UTF8Encoding($false)
    [IO.File]::WriteAllText($mainPath, $main.Replace($match.Value, ('var version = "' + $version + '"')), $utf8)
    $installer = [IO.File]::ReadAllText($installPath)
    [IO.File]::WriteAllText($installPath, ([regex]::Replace($installer, 'LITECP_VERSION="[^"]+"', ('LITECP_VERSION="' + $version + '"'))), $utf8)
    $bootstrapPath = Join-Path (Get-Location) 'scripts/bootstrap-install.sh'
    $bootstrap = [IO.File]::ReadAllText($bootstrapPath)
    [IO.File]::WriteAllText($bootstrapPath, ([regex]::Replace($bootstrap, '(?m)^version=[^\r\n]+', ('version=' + $version))), $utf8)
    Run-Step 'git' @('add','--all')
    $staged = @(& git diff --cached --name-only)
    if ($staged -contains 'update-local.bat') { throw 'Private SSH settings are staged. Remove update-local.bat from Git staging before publishing.' }
    if ($staged | Where-Object { $_ -match '(^|/)(\.env|id_rsa|id_ed25519|password\.enc)(\.|$)|\.(pem|key|pfx)$' }) { throw 'Potential credential file staged. Review Git staging before publishing.' }
    # Generated QA output is not source text. It may already be staged by an
    # earlier updater run; do not discard that staging or alter those files.
    Run-Step 'git' @('--no-pager','diff','--cached','--check','--','.',':(exclude)output/**')
    Write-Host "[3/6] Committing and publishing LiteCP $version..."
    if ($staged.Count -gt 0) { Run-Step 'git' @('commit','-m',"Update LiteCP to $version") }
    else { Write-Host 'Reusing the retained release commit; no new commit is needed.' }
    $revision = (& git rev-parse HEAD).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Cannot inspect the release revision.' }
    Publish-GitRevision $revision
    $published = $true
    Write-Host '[4/6] Preparing the exact published source for transfer...'
    $output = Join-Path ([IO.Path]::GetTempPath()) ('litecp-update-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $output | Out-Null
    $archive = Join-Path $output 'source.tar'
    Run-Step 'git' @('archive','--format=tar',"--output=$archive",$revision)
    $remote = '/root/litecp-manual-update-' + [guid]::NewGuid().ToString('N')
    $destination = "$User@$Server"
    if ($plinkExe) {
        Run-TransferStep $plinkExe (@('-batch','-no-antispoof') + $hostKeyArgs + @('-P',"$Port",'-pw',$env:LITECP_UPDATE_PASSWORD,$destination,"umask 077; mkdir -p '$remote'"))
        Run-TransferStep $pscpExe (@('-batch') + $hostKeyArgs + @('-P',"$Port",'-pw',$env:LITECP_UPDATE_PASSWORD,$archive,"${destination}:${remote}/source.tar"))
    } else {
        Run-TransferStep 'ssh' @('-o','BatchMode=yes','-o','ConnectTimeout=15','-p',"$Port",$destination,"umask 077; mkdir -p '$remote'")
        Run-TransferStep 'scp' @('-o','BatchMode=yes','-o','ConnectTimeout=15','-P',"$Port",$archive,"${destination}:${remote}/source.tar")
    }
    Write-Host '[5/6] Testing, building and deploying on the server...'
    $deployCommand = "cd '$remote' && tar -xf source.tar && bash scripts/deploy-published-update.sh '$version' '$revision'"
    if ($plinkExe) { Run-Step $plinkExe (@('-batch','-no-antispoof') + $hostKeyArgs + @('-P',"$Port",'-pw',$env:LITECP_UPDATE_PASSWORD,$destination,$deployCommand)) }
    else { Run-Step 'ssh' @('-o','BatchMode=yes','-p',"$Port",$destination,$deployCommand) }
    Write-Host '[6/6] UPDATE SUCCESS. GitHub and LiteCP are updated.'
    Write-Host "Local transfer files: $output"
    Write-Host "Server source and previous binary: $remote"
    # Keep completion in the terminal; do not open the transfer folder.
} catch {
    Write-Host "UPDATE FAILED: $($_.Exception.Message)" -ForegroundColor Red
    if ($published) { Write-Host 'GitHub was updated, but deployment did not finish. Review the failed stage above.' }
    else { Write-Host 'Publishing was not confirmed. Review the failed stage above.' }
    Write-Host 'No automatic Git reset or file cleanup was performed.'
    exit 1
}
