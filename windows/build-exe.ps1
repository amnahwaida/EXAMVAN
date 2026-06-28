# Build EXAMVAN Windows Executable
#
# Prerequisites: Python 3.10+ (64-bit), Windows 10/11 SDK
#
# Usage:
#   powershell -ExecutionPolicy Bypass -File build-exe.ps1

$ErrorActionPreference = "Stop"

$ProjectRoot = Split-Path -Parent $PSScriptRoot
$SourceDir = Join-Path $ProjectRoot "linux"
$OutputDir = Join-Path $ProjectRoot "windows\dist"

Write-Host "=== EXAMVAN Windows Builder ===" -ForegroundColor Cyan
Write-Host "Source: $SourceDir"
Write-Host ""

# ---- Check Python ----
$py = Get-Command python -ErrorAction SilentlyContinue
if (-not $py) {
    Write-Error "Python not found. Install Python 3.10+ from python.org"
    exit 1
}
Write-Host "Python: $($py.Source)" -ForegroundColor Green

# ---- Create & activate venv ----
$VenvDir = Join-Path $SourceDir ".venv"
if (Test-Path $VenvDir) {
    Remove-Item -Recurse -Force $VenvDir
}

Write-Host "Creating virtual environment..." -ForegroundColor Yellow
& python -m venv $VenvDir
$VenvPython = Join-Path $VenvDir "Scripts\python.exe"
$VenvPip = Join-Path $VenvDir "Scripts\pip.exe"

# ---- Install dependencies ----
Write-Host "Installing dependencies..." -ForegroundColor Yellow
& $VenvPip install --upgrade pip --quiet
& $VenvPip install PyQt5 PyMuPDF pyinstaller --quiet

# ---- Build with PyInstaller ----
Write-Host "Building executable..." -ForegroundColor Yellow
if (-not (Test-Path $OutputDir)) {
    New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null
}

& $VenvPython -m PyInstaller `
    --onefile `
    --windowed `
    --name "EXAMVAN" `
    --distpath $OutputDir `
    --specpath $OutputDir `
    --workpath "$OutputDir\build" `
    --hidden-import examvan `
    --hidden-import examvan.security `
    --hidden-import examvan.ui `
    --hidden-import examvan.ui.styles `
    --hidden-import examvan.ui.timer `
    --hidden-import examvan.ui.pdf_viewer `
    --hidden-import examvan.ui.answer_sheet `
    --hidden-import examvan.ui.exam_viewer `
    --hidden-import examvan.ui.identity_dialog `
    --hidden-import examvan.ui.server_config `
    --hidden-import examvan.security.base `
    --hidden-import examvan.security.enforcer `
    --hidden-import examvan.security.windows_backend `
    --exclude-module examvan.security.x11 `
    --exclude-module examvan.security.linux_backend `
    --exclude-module examvan.security.kiosk `
    --add-data "examvan;examvan" `
    (Join-Path $SourceDir "examvan\__main__.py")

# ---- Done ----
$ExePath = Join-Path $OutputDir "EXAMVAN.exe"
if (Test-Path $ExePath) {
    Write-Host "" -ForegroundColor Green
    Write-Host "=== BUILD SUCCESS ===" -ForegroundColor Green
    Write-Host "Executable: $ExePath" -ForegroundColor Cyan
    Write-Host "Size: $((Get-Item $ExePath).Length / 1MB) MB" -ForegroundColor Cyan
} else {
    Write-Error "Build failed — EXAVAN.exe not found in $OutputDir"
    exit 1
}
