# Cross-compile swisstransfer for every supported platform into dist\ (PowerShell, needs Go only).
$ErrorActionPreference = "Stop"
Set-Location (Join-Path $PSScriptRoot "..")
$Version = if ($env:VERSION) { $env:VERSION } else { try { (git describe --tags --always --dirty 2>$null) } catch { "dev" } }
if (-not $Version) { $Version = "dev" }
$LdFlags = "-s -w -X github.com/kallioli/swisstransfert-cli/internal/cli.Version=$Version"
if (Test-Path dist) { Remove-Item -Recurse -Force dist }
New-Item -ItemType Directory dist | Out-Null
$env:CGO_ENABLED = "0"
foreach ($t in "linux/amd64","linux/arm64","darwin/amd64","darwin/arm64","windows/amd64","windows/arm64") {
  $os, $arch = $t.Split("/")
  $ext = if ($os -eq "windows") { ".exe" } else { "" }
  Write-Host "building $os/$arch"
  $env:GOOS = $os; $env:GOARCH = $arch
  go build -trimpath -ldflags $LdFlags -o "dist/swisstransfer_${Version}_${os}_${arch}${ext}" ./cmd/swisstransfer
  if ($LASTEXITCODE -ne 0) { throw "build failed for $t" }
}
Remove-Item Env:GOOS, Env:GOARCH
Get-ChildItem dist | Get-FileHash -Algorithm SHA256 | ForEach-Object { "$($_.Hash.ToLower())  $(Split-Path $_.Path -Leaf)" } | Set-Content dist/SHA256SUMS
Get-Content dist/SHA256SUMS
