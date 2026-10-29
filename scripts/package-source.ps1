# Explicit source-tar packaging entry point. Preserves existing artifacts.
param([string]$Version='2.0.1')
$ErrorActionPreference='Stop'
if($Version -notmatch '^\d+\.\d+\.\d+$'){throw 'Invalid version'}
$root=Split-Path $PSScriptRoot -Parent
Set-Location $root
Get-Command tar.exe -ErrorAction Stop | Out-Null
$output=Join-Path $root ('.staging\install-'+$Version+'-'+(Get-Date -Format 'yyyyMMdd-HHmmss'))
if(Test-Path -LiteralPath $output){throw 'Output already exists'}
New-Item -ItemType Directory -Path $output | Out-Null
$source=Join-Path $output ('LiteCP-'+$Version)
New-Item -ItemType Directory -Path $source | Out-Null
Write-Host '[1/4] Gathering installation source (excluding private settings and Git history)...'
$files=@(Get-ChildItem -LiteralPath $root -File | Where-Object {($_.Extension -eq '.go' -and $_.Name -notlike '*_test.go') -or $_.Name -in @('go.mod','go.sum','README.md')})
foreach($folder in @('web','deploy','scripts')){
  $files+=Get-ChildItem -LiteralPath (Join-Path $root $folder) -File -Recurse | Where-Object {$_.FullName -notmatch '[\\/]__pycache__[\\/]' -and $_.Extension -ne '.pyc' -and ($folder -ne 'scripts' -or $_.Extension -in @('.sh','.py'))}
}
foreach($file in $files){
  $relative=$file.FullName.Substring($root.Length+1)
  $target=Join-Path $source $relative
  New-Item -ItemType Directory -Force -Path (Split-Path $target -Parent) | Out-Null
  if($file.Extension -eq '.sh'){
    [IO.File]::WriteAllText($target,([IO.File]::ReadAllText($file.FullName).Replace("`r`n","`n")),(New-Object Text.UTF8Encoding($false)))
  }else{[IO.File]::Copy($file.FullName,$target,$false)}
}
Write-Host '[2/4] Creating source tarball...'
$archive=Join-Path $output ('LiteCP-'+$Version+'-source.tar.gz')
& tar.exe -czf $archive -C $output ('LiteCP-'+$Version)
if($LASTEXITCODE -ne 0){throw 'Tar creation failed'}
Write-Host '[3/4] Verifying archive contents and checksum...'
$members=@(& tar.exe -tzf $archive)
if($LASTEXITCODE -ne 0 -or $members.Count -lt 10){throw 'Tar verification failed'}
if($members | Where-Object {$_ -match '(^|/)(\.git|update-local\.bat|secret\.key|password\.enc|__pycache__)(/|$)'}){throw 'Private content detected'}
$checksum=(Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
[IO.File]::WriteAllText($archive+'.sha256',($checksum+'  '+[IO.Path]::GetFileName($archive)+"`n"),(New-Object Text.UTF8Encoding($false)))
[IO.File]::WriteAllText((Join-Path $output 'install-litecp.sh'),([IO.File]::ReadAllText((Join-Path $PSScriptRoot 'bootstrap-install.sh')).Replace("`r`n","`n")),(New-Object Text.UTF8Encoding($false)))
Write-Host '[4/4] PACKAGE SUCCESS'
Write-Host $output
Start-Process explorer.exe -ArgumentList $output
