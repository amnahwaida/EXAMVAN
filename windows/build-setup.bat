@echo off
rem ============================================================
rem  EXAMVAN Windows Installer Builder -- 100%% CMD
rem
rem  Usage (dari folder windows\):
rem    build-setup.bat
rem
rem  Hasil: windows\dist\EXAMVAN-Setup.exe
rem    SATU FILE, double-click di PC siswa, TANPA install Python
rem    dan tanpa install apa-apa. Bedanya dari EXAMVAN.exe
rem    (portable): Setup.exe bikin shortcut + Start Menu + uninstaller.
rem
rem  Prasyarat:
rem    1. windows\dist\EXAMVAN.exe sudah ada
rem       (kalau belum: jalankan build-exe.bat lebih dulu)
rem    2. Inno Setup 6 terpasang
rem       -> https://jrsoftware.org/isdl.php  (download, install,
rem          biarkan default)
rem
rem  Script ini HANYA dijalankan oleh pengelola (bukan guru/siswa),
rem  jadi boleh memanggil python untuk hitung versi. Yang dipakai
rem  siswa nanti -- EXAMVAN-Setup.exe -- tetap self-contained: tidak
rem  butuh python, PowerShell, atau apa pun.
rem
rem  Aman dijalankan berulang: tidak menghapus venv, tidak
rem  uninstall apa pun.
rem ============================================================
setlocal
cd /d "%~dp0"
set "DIST_DIR=%~dp0dist"
set "APP_EXE=%DIST_DIR%\EXAMVAN.exe"
set "SETUP_EXE=%DIST_DIR%\EXAMVAN-Setup.exe"
set "ISS=%~dp0installer\examvan.iss"

echo.
echo   =========================================
echo     EXAMVAN Installer Builder (CMD)
echo   =========================================
echo.

rem ---- 1. Cek EXAMVAN.exe ----
if not exist "%APP_EXE%" (
    echo [ERR]  EXAMVAN.exe tidak ditemukan di dist\.
    echo.
    echo        Build dulu aplikasinya:
    echo          build-exe.bat
    echo.
    pause
    exit /b 1
)
for %%F in ("%APP_EXE%") do echo [OK]   Input : %%~nxF  (%%~zF bytes)

rem ---- 1b. Tolak exe yang lebih tua dari source ------------------------
rem Tanpa guard ini build hanya menguji -exist. Bukti di working tree:
rem windows/dist/EXAMVAN.exe mtime 27 Sep, source terbaru 30 Sep —
rem build hari itu mencetak BUILD SUCCESS dan mengemas kode 3 hari lalu
rem untuk dibagikan ke siswa.
set "PY=%~dp0..\desktop\.venv\Scripts\python.exe"
if not exist "%PY%" set "PY=python"
"%PY%" "%~dp0installer\check_exe_freshness.py" --exe "%APP_EXE%" --source "%~dp0..\desktop"
if errorlevel 2 (
    echo.
    echo [ERR]  Tidak bisa memastikan EXAMVAN.exe segar.
    echo.
    pause
    exit /b 1
)
if errorlevel 1 (
    echo.
    echo [ERR]  EXAMVAN.exe lebih tua dari source — build DIBATAS.
    echo        Jalankan windows\build-exe.bat lebih dulu.
    echo.
    pause
    exit /b 1
)

rem ---- 2. Cari ISCC.exe (Inno Setup) ----
rem Urutan: PATH -> lokasi install umum -> registry. INI yang sering
rem bikin script ini gagal "tidak ditemukan" padahal Inno Setup
rem sudah terpasang, karena iscc.exe tidak pernah masuk PATH.
set "ISCC="
where iscc >nul 2>nul && set "ISCC=iscc"

if not defined ISCC (
    for %%P in (
        "%ProgramFiles(x86)%\Inno Setup 6\ISCC.exe"
        "%ProgramFiles%\Inno Setup 6\ISCC.exe"
        "%ProgramFiles(x86)%\Inno Setup 5\ISCC.exe"
    ) do (
        if not defined ISCC if exist %%P set "ISCC=%%~P"
    )
)

if not defined ISCC (
    for /f "tokens=2,*" %%A in ('reg query "HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Inno Setup 6_is1" /v InstallLocation 2^>nul ^| findstr InstallLocation') do (
        if not defined ISCC if exist "%%B\ISCC.exe" set "ISCC=%%B\ISCC.exe"
    )
)

if not defined ISCC (
    echo [ERR]  Inno Setup 6 tidak ditemukan.
    echo.
    echo        Download ^& install dari:
    echo          https://jrsoftware.org/isdl.php
    echo.
    echo        Setelah terpasang, jalankan build-setup.bat lagi.
    echo.
    pause
    exit /b 1
)
echo [OK]   ISCC  : %ISCC%

rem ---- 3. Define untuk ISCC ----
rem Versi/build/commit dihitung windows\installer\build_info.py, bukan
rem di sini: file itu satu-satunya yang menormalkan APP_VERSION jadi
rem format numerik yang diminta VersionInfoVersion, dan logikanya sudah
rem ada testnya. Hitung ulang versi di tiga tempat = tiga tempat salah.
set "PY=%~dp0..\desktop\.venv\Scripts\python.exe"
if not exist "%PY%" set "PY=python"
"%PY%" -c "import sys" >nul 2>nul
if errorlevel 1 (
    echo [ERR]  Python tidak bisa dijalankan.
    echo        Contohnya harus: "%PY%" --version
    echo.
    pause
    exit /b 1
)
set "ISCC_DEFINES="
for /f "usebackq delims=" %%D in (`"%PY%" "%~dp0installer\build_info.py"`) do set "ISCC_DEFINES=%%D"
if not defined ISCC_DEFINES (
    echo [ERR]  build_info.py tidak menghasilkan define versi.
    pause
    exit /b 1
)
echo [INFO] Define: %ISCC_DEFINES%

rem ---- 4. Compile ----
echo.
echo [INFO] Compiling installer dengan Inno Setup (1-3 menit)...
rem Hapus setup kemarin SEBELUM compile: tanpa ini, kegagalan ISCC mencetak
rem BUILD SUCCESS di atas artefak basi (cek `if exist` di bawah lolos).
if exist "%SETUP_EXE%" del "%SETUP_EXE%"
rem ISCC_DEFINES sengaja TIDAK dikutip: ISCC menerima setiap
rem "/DNama=Nilai" sebagai argumen terpisah, dan mengutip utuhnya
rem akan memperlakukannya sebagai satu argumen yang tidak dikenal.
"%ISCC%" %ISCC_DEFINES% "%ISS%"
if errorlevel 1 (
    echo.
    echo [ERR]  Inno Setup gagal. Pesan error di atas.
    echo        Causes paling sering:
    echo          - EXAMVAN.exe tidak ada / build-exe.bat belum dijalankan
    echo          - icon examvan.png tidak ditemukan di path [Files]
    echo          - "Invalid version number" -- build_info.py gagal
    echo            menghasilkan AppVersionInfo numerik
    echo.
    pause
    exit /b 1
)

rem ---- 5. Verifikasi hasil ----
if not exist "%SETUP_EXE%" (
    echo [ERR]  Compile selesai tapi EXAMVAN-Setup.exe tidak ditemukan.
    pause
    exit /b 1
)
for %%F in ("%SETUP_EXE%") do set "SIZE=%%~zF"
set /a SIZE_MB=%SIZE% / 1048576

echo.
echo   =========================================
echo     BUILD SUCCESS
echo   =========================================
echo.
echo   Installer : %SETUP_EXE%
echo   Size      : ~%SIZE_MB% MB
echo.
echo   Distribusi ke guru/siswa:
echo     1. Copy SATU file EXAMVAN-Setup.exe
echo     2. Double-click di PC siswa
echo     3. Selesai -- tidak perlu install Python
echo.
echo   PUBLISH SHA256 di release notes supaya guru bisa verifikasi
echo   file yang mereka download. Hitung manual:
echo     certutil -hashfile "%SETUP_EXE%" SHA256
echo.
echo   EXAMVAN-Setup.exe belum code-signed, jadi Windows SmartScreen
echo   bisa tampil "Windows protected your PC". Klik "More info"
echo   lalu "Run anyway".
echo.
pause
exit /b 0
