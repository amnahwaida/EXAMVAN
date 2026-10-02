# EXAMVAN Windows Installer — check deps, create desktop shortcut
#
# Usage:
#   powershell -ExecutionPolicy Bypass -File install.ps1

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $PSCommandPath
$ProjectRoot = Split-Path -Parent $ScriptDir
$SourceDir = Join-Path $ProjectRoot "desktop"
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
    # SEMUA string yang mengandung tanda kurung WAJIB dikutip — string telanjang
    # seperti `Download Python (64-bit)` membuat PowerShell masuk mode ekspresi
    # saat ketemu '(' lalu gagal parse: "Unexpected token '-bit'".
    Write-Host "Download Python 3.10+ (64-bit) dari:"
    Write-Host "  https://www.python.org/downloads/"
    Write-Host ""
    Write-Warn "Keamanan: Verifikasi integritas berkas instalasi Python (.exe) menggunakan PowerShell:"
    Write-Host "  Get-FileHash -Algorithm SHA256 .\python-3.xx.x-amd64.exe"
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
& $VenvPython -m pip install -r (Join-Path $ProjectRoot "desktop\requirements.txt") --quiet
if ($LASTEXITCODE -ne 0) {
    Write-Err "Gagal install dependencies."
    Write-Host "Coba manual:"
    $reqFile = Join-Path $ProjectRoot 'desktop\requirements.txt'
    Write-Host "  $VenvPython -m pip install -r `"$reqFile`""
    pause
    exit 1
}
Write-Ok "Dependencies terinstall"

# ---- Test import ----
# Cek $LASTEXITCODE, BUKAN try/catch: try/catch PowerShell tidak menangkap
# exit code non-zero dari program eksternal, jadi dulu import gagal tetap
# dilaporkan "berhasil". Traceback sengaja TIDAK di-redirect supaya penyebab
# aslinya (DLL load failed, dll.) terlihat di layar.
& $VenvPython -c "import PyQt5; import fitz; print('OK')"
if ($LASTEXITCODE -ne 0) {
    Write-Err "Import PyQt5 gagal. Mungkin butuh Visual C++ Redistributable:"
    Write-Host "  https://aka.ms/vs/17/release/vc_redist.x64.exe"
    pause
    exit 1
}
Write-Ok "Import test berhasil"

# ---- Buat launcher .bat di desktop ----
$desktop = [Environment]::GetFolderPath("Desktop")
$launcherPath = Join-Path $desktop "EXAMVAN.bat"
$runBat = Join-Path $ScriptDir "run.bat"

# Path di-hardcode saat install (BUKAN via env var): env var User yang baru
# dibuat TIDAK terlihat oleh proses yang sudah berjalan (termasuk Explorer,
# yang melaunch double-click .bat) sampai logout/re-login.
# Launcher memanggil run.bat LANGSUNG (CMD murni), BUKAN run.ps1 via
# powershell — klik ikon desktop tidak boleh bergantung pada PowerShell,
# yang bisa gagal karena policy, PowerShell 7, atau env korporat korup.
# Backslash tidak perlu di-escape: cmd memperlakukannya literal di dalam kutip.
# Ditulis UTF-8 TANPA BOM via .NET: Out-File -Encoding ASCII mengubah
# karakter non-ASCII di path (mis. nama user) menjadi `?` sehingga launcher
# memanggil path yang salah. PS 5.1 tidak punya `utf8NoBOM`, jadi pakai
# UTF8Encoding($false) eksplisit.
$content = @"
@echo off
call "$runBat"
pause
"@
[System.IO.File]::WriteAllText($launcherPath, $content, (New-Object System.Text.UTF8Encoding $false))

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
Write-Host "    cd desktop"
Write-Host "    .venv\Scripts\python -m examvan"
Write-Host ""
Write-Host "  Uninstall: hapus folder desktop\.venv + shortcut desktop"
Write-Host ""
pause
