$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'publish-git.ps1')
$script:revision = 'a' * 40
$script:old = 'b' * 40
function Start-Sleep { param($Seconds) }
function Invoke-PublishGit([string[]]$Arguments) {
    $script:calls += ,$Arguments
    if ($script:queue.Count -eq 0) { throw 'Unexpected Git call' }
    $next = $script:queue[0]
    if ($script:queue.Count -eq 1) { $script:queue = @() }
    else { $script:queue = @($script:queue[1..($script:queue.Count - 1)]) }
    return $next
}
function Remote([string]$Hash) { return @{Code=0; Lines=@("$Hash`trefs/heads/main")} }
function Assert([bool]$Condition, [string]$Message) { if (!$Condition) { throw $Message } }

$script:calls=@(); $script:queue=@((Remote $revision))
Publish-GitRevision $revision
Assert ($calls.Count -eq 1) 'Already-published commit was pushed again'

$script:calls=@(); $script:queue=@((Remote $old), @{Code=1;Lines=@('remote: fatal error in commit_refs')}, (Remote $old), (Remote $old), @{Code=0;Lines=@('accepted')}, (Remote $revision))
Publish-GitRevision $revision
$pushes=@($calls | Where-Object { $_[0] -eq 'push' })
Assert ($pushes.Count -eq 2) 'Temporary remote failure did not retry'
foreach ($push in $pushes) { Assert ($push[2] -eq "${revision}:refs/heads/main") 'Retry changed the release commit'; Assert (!($push -contains '--force')) 'Force push is forbidden' }

$script:calls=@(); $script:queue=@((Remote $old), @{Code=1;Lines=@('connection reset')}, (Remote $revision))
Publish-GitRevision $revision
Assert ($calls.Count -eq 3) 'Successful remote update was not detected after connection loss'

$script:calls=@(); $script:queue=@((Remote $old), @{Code=1;Lines=@('remote: protected branch hook declined')}, (Remote $old))
$failed=$false
try { Publish-GitRevision $revision } catch { $failed=$true }
Assert $failed 'Protected branch rejection was ignored'
Assert ($calls.Count -eq 3) 'Protected branch rejection was retried'

$script:calls=@(); $script:queue=@()
1..3 | ForEach-Object { $script:queue += @((Remote $old), @{Code=1;Lines=@('remote: fatal error in commit_refs')}, (Remote $old)) }
$failed=$false
try { Publish-GitRevision $revision } catch { $failed=$true }
Assert $failed 'Exhausted temporary failures were reported as success'
Assert (@($calls | Where-Object { $_[0] -eq 'push' }).Count -eq 3) 'Retry count was not bounded'
Write-Host 'PASS: exact revision publishing, transient retry, remote verification and protected-branch rejection.'
