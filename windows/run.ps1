# EXAMVAN Windows Launcher — auto setup venv + run
#
# Usage:
#   powershell -ExecutionPolicy Bypass -File run.ps1

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $PSCommandPath
$ProjectRoot = Split-Path -Parent $ScriptDir
$SourceDir = Join-Path $ProjectRoot "linux"
$VenvDir = Join-Path $SourceDir ".venv"
$VenvPython = Join-Path $VenvDir "Scripts\python.exe"

function Write-Info  { Write-Host "[INFO]" -ForegroundColor Blue -NoNewline; Write-Host " $args" }
function Write-Ok   { Write-Host "[OK]" -ForegroundColor Green -NoNewline; Write-Host " $args" }
function Write-Err  { Write-Host "[ERR]" -ForegroundColor Red -NoNewline; Write-Host " $args" }

# ---- Check Python ----
$py = Get-Command python -ErrorAction SilentlyContinue
if (-not $py) {
    Write-Err "Python tidak ditemukan."
    Write-Host ""
    Write-Host "Download Python 3.10+ dari: https://www.python.org/downloads/"
    Write-Host "Pastikan centang 'Add Python to PATH' saat install."
    pause
    exit 1
}
$pyVer = & python -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')"
Write-Info "Python $pyVer — $($py.Source)"

# ---- Setup venv jika belum ada ----
if (-not (Test-Path $VenvPython)) {
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
    Write-Info "Menginstall PyQt5 + PyMuPDF..."
    & $VenvPython -m pip install --upgrade pip --quiet
    & $VenvPython -m pip install PyQt5 PyMuPDF --quiet
    if ($LASTEXITCODE -ne 0) {
        Write-Err "Gagal install dependencies"
        Write-Host "Coba install manual:"
        Write-Host "  $VenvPython -m pip install PyQt5 PyMuPDF"
        pause
        exit 1
    }
    Write-Ok "Dependencies siap"
} else {
    Write-Info "Virtual environment sudah ada"
}

# ---- Cek PyQt5 ----
try {
    & $VenvPython -c "import PyQt5" 2>$null
} catch {
    Write-Info "PyQt5 belum terinstall, menginstall..."
    & $VenvPython -m pip install PyQt5 PyMuPDF --quiet
}

# ---- Jalankan ----
Write-Info "Menjalankan EXAMVAN..."
$host.UI.RawUI.WindowTitle = "EXAMVAN"
& $VenvPython -m examvan
