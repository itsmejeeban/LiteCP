# Safe, bounded retries for publishing one immutable revision. Never force-push.
function Invoke-PublishGit([string[]]$Arguments) {
    $saved = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        $lines = @(& git @Arguments 2>&1 | ForEach-Object { "$_" })
        $code = $LASTEXITCODE
    } finally { $ErrorActionPreference = $saved }
    return @{ Code = $code; Lines = $lines }
}
function Get-PublishRemoteMain {
    $result = Invoke-PublishGit @('ls-remote','--exit-code','origin','refs/heads/main')
    if ($result.Code -ne 0) { throw 'Cannot verify GitHub main. No deployment will start; check network and Git credentials.' }
    $matches = @($result.Lines | Where-Object { $_ -match '^[0-9a-f]{40,64}\s+refs/heads/main$' })
    if ($matches.Count -ne 1) { throw 'GitHub main returned an unexpected reference. Publishing stopped.' }
    return ($matches[0] -split '\s+')[0]
}
function Publish-GitRevision([string]$Revision) {
    if ($Revision -notmatch '^[0-9a-f]{40,64}$') { throw 'Invalid publishing revision.' }
    for ($attempt = 1; $attempt -le 3; $attempt++) {
        if ((Get-PublishRemoteMain) -eq $Revision) {
            Write-Host 'GitHub already contains the exact release commit. Continuing safely.'
            return
        }
        $result = Invoke-PublishGit @('push','origin',"${Revision}:refs/heads/main")
        $result.Lines | ForEach-Object { Write-Host $_ }
        # A network interruption can hide a successful remote update. Verify it
        # before deciding whether another push is needed.
        if ((Get-PublishRemoteMain) -eq $Revision) { return }
        $message = $result.Lines -join "`n"
        $temporary = $message -match '(?i)fatal error in commit_refs|HTTP (?:500|502|503|504)|remote: internal server error|connection (?:reset|timed out)|could not resolve host|RPC failed.*(?:HTTP 5\d\d|curl (?:18|28|52|56))'
        if (!$temporary -or $attempt -eq 3) {
            throw "GitHub did not accept release commit $Revision (exit $($result.Code)). Local commit retained. Retry update.bat after resolving the remote error; no force-push or reset is needed."
        }
        Write-Host "Temporary GitHub publishing failure. Retrying the same commit ($($attempt + 1)/3)..." -ForegroundColor Yellow
        Start-Sleep -Seconds 2
    }
}
