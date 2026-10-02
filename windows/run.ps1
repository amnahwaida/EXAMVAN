# EXAMVAN Windows Launcher — auto setup venv + run
#
# Usage:
#   powershell -ExecutionPolicy Bypass -File run.ps1
#   (atau double-click shortcut EXAMVAN.bat buatan install.ps1)
#
# Catatan: script ini memakai Set-Location ke desktop/ sebelum launch —
# package examvan TIDAK di-install ke venv (tidak ada pip install -e /
# pyproject.toml), jadi `python -m examvan` hanya resolve bila CWD adalah
# folder desktop/. Tanpa itu, double-click EXAMVAN.bat (CWD = lokasi
# shortcut) gagal dengan "No module named examvan".

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $PSCommandPath
$ProjectRoot = Split-Path -Parent $ScriptDir
$SourceDir = Join-Path $ProjectRoot "desktop"
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
    & $VenvPython -m pip install -r (Join-Path $ProjectRoot "desktop\requirements.txt") --quiet
    if ($LASTEXITCODE -ne 0) {
        Write-Err "Gagal install dependencies"
        Write-Host "Coba install manual:"
        $reqFile = Join-Path $ProjectRoot 'desktop\requirements.txt'
        Write-Host "  $VenvPython -m pip install -r `"$reqFile`""
        pause
        exit 1
    }
    Write-Ok "Dependencies siap"
} else {
    Write-Info "Virtual environment sudah ada"
}

# ---- Cek PyQt5 ----
# Cek $LASTEXITCODE, bukan try/catch: try/catch PowerShell tidak menangkap
# exit code non-zero dari program eksternal, jadi dulu import gagal tidak
# terdeteksi dan app tetap dicoba dijalankan lalu crash.
& $VenvPython -c "import PyQt5"
if ($LASTEXITCODE -ne 0) {
    Write-Info "PyQt5 belum terinstall / gagal load, menginstall..."
    & $VenvPython -m pip install -r (Join-Path $ProjectRoot "desktop\requirements.txt") --quiet
    # Jalur ini sebelumnya TIDAK mengecek exit code, sementara jalur
    # first-time di atas flicked. venv parsial lalu menghasilkan raw
    # traceback, bukan pesan yang bisa ditindaklanjuti.
    if ($LASTEXITCODE -ne 0) {
        Write-Err "Gagal install dependencies"
        Write-Host "Coba install manual:"
        $reqFile = Join-Path $ProjectRoot 'desktop\requirements.txt'
        Write-Host "  $VenvPython -m pip install -r `"$reqFile`""
        pause
        exit 1
    }
}

# ---- Jalankan ----
Write-Info "Menjalankan EXAMVAN..."
$host.UI.RawUI.WindowTitle = "EXAMVAN"

# CWD HARUS desktop/ agar `python -m examvan` menemukan package (examvan
# tidak ter-install ke venv; Python resolve -m dari CWD). Pindah SETELAH
# semua path absolut ($ScriptDir/$SourceDir/$VenvDir) sudah dihitung.
Set-Location $SourceDir

& $VenvPython -m examvan
