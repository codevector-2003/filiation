# Builds the Windows installer: fil.exe and Filiation.exe for 64-bit Windows,
# then Inno Setup's compiler over filiation.iss (D37). Used by CI for pull
# requests and releases, and by hand:
#
#   pwsh desktop/installer/build.ps1 -Version 0.4.2
#
# Prints the installer's path. Inno Setup 6 is installed with Chocolatey when
# it is missing, as on a fresh CI runner.
param(
    [Parameter(Mandatory = $true)][string]$Version
)
$ErrorActionPreference = 'Stop'

$here = $PSScriptRoot
$root = Resolve-Path (Join-Path $here '..\..')
$build = Join-Path $here 'build'
New-Item -ItemType Directory -Force $build | Out-Null

# Hard rule 4: fil is one static binary, no cgo. The desktop shell needs none
# on Windows either: Wails drives WebView2 from pure Go there.
$env:CGO_ENABLED = '0'
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'

Push-Location $root
try {
    go build -trimpath -ldflags "-s -w -X main.Version=$Version" -o (Join-Path $build 'fil.exe') ./cmd/fil
    if ($LASTEXITCODE -ne 0) { throw 'building fil failed' }
} finally { Pop-Location }

Push-Location (Join-Path $root 'desktop')
try {
    go build -trimpath -tags 'desktop,production' -ldflags "-s -w -H windowsgui -X main.version=$Version" -o (Join-Path $build 'Filiation.exe') .
    if ($LASTEXITCODE -ne 0) { throw 'building the desktop app failed' }
} finally { Pop-Location }

$iscc = Join-Path ${env:ProgramFiles(x86)} 'Inno Setup 6\ISCC.exe'
if (-not (Test-Path $iscc)) {
    choco install innosetup --no-progress -y | Out-Host
    if (-not (Test-Path $iscc)) { throw "Inno Setup 6 is not installed at $iscc" }
}
& $iscc /Q "/DAppVersion=$Version" '/DSourceDir=build' (Join-Path $here 'filiation.iss') | Out-Host
if ($LASTEXITCODE -ne 0) { throw 'Inno Setup failed' }

Get-ChildItem (Join-Path $here 'Output') -Filter "Filiation-Setup-$Version-*.exe" | Select-Object -ExpandProperty FullName
