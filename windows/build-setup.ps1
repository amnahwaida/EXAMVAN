# Build EXAMVAN Windows Installer (EXAMVAN-Setup.exe) via Inno Setup
#
# Usage:
#   powershell -ExecutionPolicy Bypass -File build-setup.ps1
#
# Input : windows\dist\EXAMVAN.exe   (dari build-exe.bat / build-exe.ps1)
# Output: windows\dist\EXAMVAN-Setup.exe
#
# Hasilnya SATU FILE yang bisa di-copy ke PC siswa: tidak perlu
# install Python, tidak perlu install apa-apa. Setara CMD-nya:
# build-setup.bat (build-setup.bat lebih disarankan — repo ini
# sengaja migrating ke CMD murni karena PowerShell sering diblokir
# policy di PC sekolah; script ini dipertahankan untuk CI/kelolaan).

$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $PSCommandPath
$DistDir = Join-Path $ScriptDir "dist"
$AppExe = Join-Path $DistDir "EXAMVAN.exe"
$SetupExe = Join-Path $DistDir "EXAMVAN-Setup.exe"
$IssPath = Join-Path $ScriptDir "installer\examvan.iss"

function Write-Info { Write-Host "[INFO]" -ForegroundColor Blue -NoNewline; Write-Host " $args" }
function Write-Ok { Write-Host "[OK]" -ForegroundColor Green -NoNewline; Write-Host " $args" }
function Write-Err { Write-Host "[ERR]" -ForegroundColor Red -NoNewline; Write-Host " $args" }

Write-Host ""
Write-Host "  =========================================" -ForegroundColor Cyan
Write-Host "    EXAMVAN Installer Builder" -ForegroundColor Cyan
Write-Host "  =========================================" -ForegroundColor Cyan
Write-Host ""

# ---- Cek input ----
if (-not (Test-Path $AppExe)) {
    Write-Err "EXAMVAN.exe tidak ditemukan di dist\."
    Write-Host "  Build dulu aplikasinya: build-exe.bat"
    exit 1
}
$appSize = [math]::Round((Get-Item $AppExe).Length / 1MB, 2)
Write-Ok "Input: EXAMVAN.exe ($appSize MB)"

# ---- Cari ISCC.exe ----
# ISCC tidak pernah masuk PATH -> uninstall registry + lokasi umum.
$iscc = $null
$onPath = Get-Command iscc -ErrorAction SilentlyContinue
if ($onPath) {
    $iscc = $onPath.Source
} else {
    $candidates = @(
        "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe",
        "$env:ProgramFiles\Inno Setup 6\ISCC.exe",
        "${env:ProgramFiles(x86)}\Inno Setup 5\ISCC.exe"
    )
    foreach ($c in $candidates) {
        if ($c -and (Test-Path $c)) { $iscc = $c; break }
    }
}
if (-not $iscc) {
    $key = "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Inno Setup 6_is1"
    if (Test-Path $key) {
        $loc = (Get-ItemProperty $key -ErrorAction SilentlyContinue).InstallLocation
        if ($loc -and (Test-Path (Join-Path $loc "ISCC.exe"))) {
            $iscc = Join-Path $loc "ISCC.exe"
        }
    }
}
if (-not $iscc) {
    Write-Err "Inno Setup 6 tidak ditemukan."
    Write-Host "  Download & install: https://jrsoftware.org/isdl.php"
    Write-Host "  Lalu jalankan ulang script ini."
    exit 1
}
Write-Ok "ISCC: $iscc"

# ---- Define untuk ISCC ----
# Versi/build/commit dihitung windows\installer\build_info.py — bukan
# di sini. File itu satu-satunya yang menormalkan APP_VERSION jadi
# format numerik yang diminta VersionInfoVersion (kirim "v2.5.0" ke
# ISCC = compile gagal), dan logikanya sudah ada testnya.
$buildInfo = Join-Path $ScriptDir "installer\build_info.py"
if (-not (Test-Path $buildInfo)) {
    Write-Err "installer\build_info.py tidak ditemukan."
    exit 1
}
$defines = (& python $buildInfo)
if ($LASTEXITCODE -ne 0 -or -not $defines) {
    Write-Err "build_info.py gagal menghasilkan define versi."
    exit 1
}
Write-Info "Define: $defines"

# ---- Compile ----
Write-Info "Compiling installer dengan Inno Setup (1-3 menit)..."
# $defines dipecah jadi argumen terpisah: ISCC menerima setiap
# "/DNama=Nilai" sendiri, mengutip utuhnya akan jadi argumen tak dikenal.
# (Berbeda dari baris PowerShell biasa, di sini backtick + split
# dipakai justru karena TIDAK boleh mengutip tiap elemennya.)
$isccArgs = @($defines.Trim().Split(" ")) + @($IssPath)

& $iscc @isccArgs
if ($LASTEXITCODE -ne 0) {
    Write-Err "Inno Setup gagal (exit $LASTEXITCODE). Pesan error di atas."
    exit 1
}

# ---- Verifikasi ----
if (-not (Test-Path $SetupExe)) {
    Write-Err "Compile selesai tapi EXAMVAN-Setup.exe tidak ditemukan."
    exit 1
}
$size = [math]::Round((Get-Item $SetupExe).Length / 1MB, 2)
$sha = (Get-FileHash -Algorithm SHA256 $SetupExe).Hash

Write-Host ""
Write-Host "  =========================================" -ForegroundColor Green
Write-Host "    BUILD SUCCESS" -ForegroundColor Green
Write-Host "  =========================================" -ForegroundColor Green
Write-Host ""
Write-Host "  Installer: $SetupExe" -ForegroundColor Cyan
Write-Host "  Size     : $size MB" -ForegroundColor Cyan
Write-Host "  SHA256   : $sha" -ForegroundColor Cyan
Write-Host ""
Write-Host "  Distribusi ke guru/siswa:"
Write-Host "    1. Copy SATU file EXAMVAN-Setup.exe"
Write-Host "    2. Double-click di PC siswa"
Write-Host "    3. Selesai -- tidak perlu install Python"
Write-Host ""
Write-Host "  PUBLISH SHA256 di atas ke release notes supaya guru bisa"
Write-Host "  memverifikasi file yang mereka download."
Write-Host ""
Write-Host "  Belum code-signed -> SmartScreen bisa tampil 'Windows"
Write-Host "  protected your PC'. Klik 'More info' lalu 'Run anyway'."
Write-Host ""
