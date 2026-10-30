# Run through update.bat. Requires Git and Windows OpenSSH; Go is required on the server.
param([string]$Server = '162.55.50.122', [int]$Port = 369, [string]$User = 'root', [switch]$CheckOnly)
$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot -Parent)
if ($env:LITECP_UPDATE_PORT) { $Port = [int]$env:LITECP_UPDATE_PORT }
function Run-Step([string]$Tool, [string[]]$Arguments) {
    & $Tool @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Tool failed with exit code $LASTEXITCODE" }
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
    Write-Host '[2/6] Reviewing local changes...'
    Run-Step 'git' @('status','--short')
    Write-Host 'This publishes all non-ignored changes to GitHub main and deploys that commit to LiteCP.'
    if ((Read-Host 'Type UPDATE to continue') -cne 'UPDATE') { Write-Host 'Cancelled.'; exit 0 }
    $mainPath = Join-Path (Get-Location) 'main.go'
    $installPath = Join-Path (Get-Location) 'scripts/install.sh'
    $main = [IO.File]::ReadAllText($mainPath)
    $match = [regex]::Match($main, 'var version = "(\d+)\.(\d+)\.(\d+)"')
    if (!$match.Success) { throw 'Cannot determine current LiteCP version.' }
    $version = '{0}.{1}.{2}' -f $match.Groups[1].Value,$match.Groups[2].Value,([int]$match.Groups[3].Value + 1)
    $utf8 = New-Object System.Text.UTF8Encoding($false)
    [IO.File]::WriteAllText($mainPath, $main.Replace($match.Value, ('var version = "' + $version + '"')), $utf8)
    $installer = [IO.File]::ReadAllText($installPath)
    [IO.File]::WriteAllText($installPath, ([regex]::Replace($installer, 'LITECP_VERSION="[^"]+"', ('LITECP_VERSION="' + $version + '"'))), $utf8)
    Run-Step 'git' @('add','--all')
    $staged = @(& git diff --cached --name-only)
    if ($staged -contains 'update-local.bat') { throw 'Private SSH settings are staged. Remove update-local.bat from Git staging before publishing.' }
    if ($staged | Where-Object { $_ -match '(^|/)(\.env|id_rsa|id_ed25519|password\.enc)(\.|$)|\.(pem|key|pfx)$' }) { throw 'Potential credential file staged. Review Git staging before publishing.' }
    # Generated QA output is not source text. It may already be staged by an
    # earlier updater run; do not discard that staging or alter those files.
    Run-Step 'git' @('--no-pager','diff','--cached','--check','--','.',':(exclude)output/**')
    Write-Host "[3/6] Committing and publishing LiteCP $version..."
    Run-Step 'git' @('commit','-m',"Update LiteCP to $version")
    Run-Step 'git' @('push','origin','main')
    $published = $true
    $revision = (& git rev-parse HEAD).Trim()
    Write-Host '[4/6] Preparing the exact published source for transfer...'
    $output = Join-Path ([IO.Path]::GetTempPath()) ('litecp-update-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $output | Out-Null
    $archive = Join-Path $output 'source.tar'
    Run-Step 'git' @('archive','--format=tar',"--output=$archive",$revision)
    $remote = '/root/litecp-manual-update-' + [guid]::NewGuid().ToString('N')
    $destination = "$User@$Server"
    if ($plinkExe) {
        Run-Step $plinkExe @('-P',"$Port",'-pw',$env:LITECP_UPDATE_PASSWORD,$destination,"umask 077; mkdir '$remote'")
        Run-Step $pscpExe @('-P',"$Port",'-pw',$env:LITECP_UPDATE_PASSWORD,$archive,"${destination}:${remote}/source.tar")
    } else {
        Run-Step 'ssh' @('-p',"$Port",$destination,"umask 077; mkdir '$remote'")
        Run-Step 'scp' @('-P',"$Port",$archive,"${destination}:${remote}/source.tar")
    }
    Write-Host '[5/6] Testing, building and deploying on the server...'
    $deployCommand = "cd '$remote' && tar -xf source.tar && bash scripts/deploy-published-update.sh '$version' '$revision'"
    if ($plinkExe) { Run-Step $plinkExe @('-P',"$Port",'-pw',$env:LITECP_UPDATE_PASSWORD,$destination,$deployCommand) }
    else { Run-Step 'ssh' @('-p',"$Port",$destination,$deployCommand) }
    Write-Host '[6/6] UPDATE SUCCESS. GitHub and LiteCP are updated.'
    Write-Host "Local transfer files: $output"
    Write-Host "Server source and previous binary: $remote"
    Start-Process explorer.exe -ArgumentList $output
} catch {
    Write-Host "UPDATE FAILED: $($_.Exception.Message)" -ForegroundColor Red
    if ($published) { Write-Host 'GitHub was updated, but deployment did not finish. Review the failed stage above.' }
    else { Write-Host 'Publishing was not confirmed. Review the failed stage above.' }
    Write-Host 'No automatic Git reset or file cleanup was performed.'
    exit 1
}
