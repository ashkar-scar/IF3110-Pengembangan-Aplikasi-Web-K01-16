param([string]$Path = (Join-Path $PSScriptRoot '..\.env'))

if (-not (Test-Path -LiteralPath $Path)) {
    throw "File konfigurasi tidak ditemukan: $Path"
}

foreach ($line in Get-Content -LiteralPath $Path) {
    $entry = $line.Trim()
    if ($entry -eq '' -or $entry.StartsWith('#')) { continue }
    $parts = $entry.Split('=', 2)
    if ($parts.Count -ne 2 -or $parts[0] -notmatch '^[A-Z_][A-Z0-9_]*$') {
        throw 'Format konfigurasi harus NAMA=nilai'
    }
    [Environment]::SetEnvironmentVariable($parts[0], $parts[1], 'Process')
}
