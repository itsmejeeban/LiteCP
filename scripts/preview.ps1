param(
    [int]$Port = 4173
)

$ErrorActionPreference = 'Stop'
$webRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\web'))
$listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, $Port)
$listener.Start()
Write-Host "LiteCP preview: http://127.0.0.1:$Port/?demo=1"

$mimeTypes = @{
    '.html' = 'text/html; charset=utf-8'
    '.css'  = 'text/css; charset=utf-8'
    '.js'   = 'application/javascript; charset=utf-8'
    '.svg'  = 'image/svg+xml'
    '.png'  = 'image/png'
}

try {
    while ($true) {
        $client = $listener.AcceptTcpClient()
        $stream = $client.GetStream()
        $reader = [System.IO.StreamReader]::new($stream, [System.Text.Encoding]::ASCII, $false, 1024, $true)
        $requestLine = $reader.ReadLine()
        while ($reader.ReadLine()) { }
        $requestTarget = if ($requestLine) { ($requestLine -split ' ')[1] } else { '/' }
        $requestPath = ([System.Uri]::new("http://127.0.0.1$requestTarget")).AbsolutePath
        $relative = [System.Uri]::UnescapeDataString($requestPath).TrimStart('/')
        if ([string]::IsNullOrWhiteSpace($relative)) { $relative = 'index.html' }
        $candidate = [System.IO.Path]::GetFullPath((Join-Path $webRoot $relative))
        if (-not $candidate.StartsWith($webRoot, [System.StringComparison]::OrdinalIgnoreCase) -or -not (Test-Path -LiteralPath $candidate -PathType Leaf)) {
            $header = [System.Text.Encoding]::ASCII.GetBytes("HTTP/1.1 404 Not Found`r`nContent-Length: 0`r`nConnection: close`r`n`r`n")
            $stream.Write($header, 0, $header.Length)
            $reader.Dispose(); $stream.Dispose(); $client.Dispose()
            continue
        }
        $bytes = [System.IO.File]::ReadAllBytes($candidate)
        $extension = [System.IO.Path]::GetExtension($candidate).ToLowerInvariant()
        $contentType = if ($mimeTypes.ContainsKey($extension)) { $mimeTypes[$extension] } else { 'application/octet-stream' }
        $headerText = "HTTP/1.1 200 OK`r`nContent-Type: $contentType`r`nContent-Length: $($bytes.Length)`r`nCache-Control: no-store`r`nConnection: close`r`n`r`n"
        $header = [System.Text.Encoding]::ASCII.GetBytes($headerText)
        $stream.Write($header, 0, $header.Length)
        $stream.Write($bytes, 0, $bytes.Length)
        $reader.Dispose(); $stream.Dispose(); $client.Dispose()
    }
}
finally {
    $listener.Stop()
}
