# EXAMVAN Windows Installer — check deps, create desktop shortcut
#
# Usage:
#   powershell -ExecutionPolicy Bypass -File install.ps1

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $PSCommandPath
$ProjectRoot = Split-Path -Parent $ScriptDir
$SourceDir = Join-Path $ProjectRoot "linux"
$VenvDir = Join-Path $SourceDir ".venv"
$VenvPython = Join-Path $VenvDir "Scripts\python.exe"

# ---- Warna ----
function Write-Info  { Write-Host "[INFO]" -ForegroundColor Blue -NoNewline; Write-Host " $args" }
function Write-Ok   { Write-Host "[OK]" -ForegroundColor Green -NoNewline; Write-Host " $args" }
function Write-Err  { Write-Host "[ERR]" -ForegroundColor Red -NoNewline; Write-Host " $args" }
function Write-Warn { Write-Host "[WARN]" -ForegroundColor Yellow -NoNewline; Write-Host " $args" }

Write-Host ""
Write-Host "  ╔═══════════════════════════════════════╗"
Write-Host "  ║     EXAMVAN Windows Installer          ║"
Write-Host "  ╚═══════════════════════════════════════╝"
Write-Host ""

# ---- Check Python ----
Write-Info "Memeriksa Python..."
$py = Get-Command python -ErrorAction SilentlyContinue
if (-not $py) {
    Write-Err "Python tidak ditemukan!"
    Write-Host ""
    Write-Host "Download Python 3.10+ (64-bit) dari:"
    Write-Host "  https://www.python.org/downloads/"
    Write-Host ""
    Write-Host "PASTIKAN centang 'Add Python to PATH' saat install."
    pause
    exit 1
}
$pyVer = & python -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')"
Write-Ok "Python $pyVer — $($py.Source)"

# ---- Setup venv ----
Write-Info "Membuat virtual environment..."
if (Test-Path $VenvDir) {
    Remove-Item -Recurse -Force $VenvDir
}
& python -m venv $VenvDir
if (-not (Test-Path $VenvPython)) {
    Write-Err "Gagal membuat virtual environment"
    pause
    exit 1
}
Write-Ok "Virtual environment siap"

# ---- Install dependencies ----
Write-Info "Menginstall dependencies (PyQt5 + PyMuPDF)..."
& $VenvPython -m pip install --upgrade pip --quiet
& $VenvPython -m pip install PyQt5 PyMuPDF --quiet
if ($LASTEXITCODE -ne 0) {
    Write-Err "Gagal install dependencies."
    Write-Host "Coba manual:"
    Write-Host "  $VenvPython -m pip install PyQt5 PyMuPDF"
    pause
    exit 1
}
Write-Ok "Dependencies terinstall"

# ---- Test import ----
try {
    & $VenvPython -c "import PyQt5; import fitz; print('OK')" 2>$null
    Write-Ok "Import test berhasil"
} catch {
    Write-Err "Import PyQt5 gagal. Mungkin butuh Visual C++ Redistributable:"
    Write-Host "  https://aka.ms/vs/17/release/vc_redist.x64.exe"
    pause
    exit 1
}

# ---- Buat launcher .bat di desktop ----
$desktop = [Environment]::GetFolderPath("Desktop")
$launcherPath = Join-Path $desktop "EXAMVAN.bat"
$runScript = Join-Path $ScriptDir "run.ps1"

@"
@echo off
powershell -ExecutionPolicy Bypass -File "%EXAMVAN_DIR%\run.ps1"
pause
"@ | Out-File -FilePath $launcherPath -Encoding ASCII

# Set env var so .bat can find run.ps1
[Environment]::SetEnvironmentVariable("EXAMVAN_DIR", $ScriptDir, "User")

Write-Ok "Launcher dibuat: $launcherPath"

# ---- Selesai ----
Write-Host ""
Write-Host "  ╔═══════════════════════════════════════╗"
Write-Host "  ║        INSTALASI SELESAI!              ║"
Write-Host "  ╚═══════════════════════════════════════╝"
Write-Host ""
Write-Host "  Jalankan EXAMVAN dari shortcut di desktop:"
Write-Host "    EXAMVAN.bat"
Write-Host ""
Write-Host "  Atau dari terminal:"
Write-Host "    cd linux"
Write-Host "    .venv\Scripts\python -m examvan"
Write-Host ""
Write-Host "  Uninstall: hapus folder linux\.venv + shortcut desktop"
Write-Host ""
pause
