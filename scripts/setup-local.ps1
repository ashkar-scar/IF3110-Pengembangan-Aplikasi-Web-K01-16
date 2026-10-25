$ErrorActionPreference = 'Stop'
$repoPath = Split-Path -Parent $PSScriptRoot
$envPath = Join-Path $repoPath '.env'

if (-not (Test-Path -LiteralPath $envPath)) {
    Copy-Item -LiteralPath (Join-Path $repoPath '.env.example') -Destination $envPath
}
. (Join-Path $PSScriptRoot 'load-env.ps1') -Path $envPath

foreach ($name in @('TARGET_CREDENTIAL_KEY', 'ADMIN_PASSWORD', 'ADMIN_USERNAME', 'DEMO_PASSWORD')) {
    if ([string]::IsNullOrWhiteSpace([Environment]::GetEnvironmentVariable($name, 'Process'))) {
        if ($name -eq 'ADMIN_USERNAME') {
            $value = 'admin'
        } else {
            $randomBytes = New-Object byte[] 32
            $generator = [Security.Cryptography.RandomNumberGenerator]::Create()
            try { $generator.GetBytes($randomBytes) } finally { $generator.Dispose() }
            $value = [Convert]::ToBase64String($randomBytes)
        }
        [Environment]::SetEnvironmentVariable($name, $value, 'Process')
        [IO.File]::AppendAllText($envPath, "`n$name=$value`n", [Text.UTF8Encoding]::new($false))
    }
}

Push-Location -LiteralPath $repoPath
try {
    docker compose up -d --wait
    if ($LASTEXITCODE -ne 0) { throw 'PostgreSQL fixture gagal dijalankan' }
    go run ./cmd/setup
    if ($LASTEXITCODE -ne 0) { throw 'Setup platform gagal' }
    go run ./cmd/demo
    if ($LASTEXITCODE -ne 0) { throw 'Seed demo gagal' }
    Write-Host 'Setup selesai. Username dan password admin tersimpan di .env lokal.'
} finally {
    Pop-Location
}
