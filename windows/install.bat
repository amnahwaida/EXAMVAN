@echo off
rem ============================================================
rem  EXAMVAN Windows Installer -- 100% CMD, tanpa PowerShell
rem
rem  Usage (dari folder windows\):
rem    install.bat
rem
rem  Yang dilakukan:
rem    1. Cek python di PATH
rem    2. Venv di desktop\.venv -- dibuat HANYA jika belum ada /
rem       rusak (aman dijalankan ulang tanpa download ulang)
rem    3. Buat shortcut EXAMVAN.bat di desktop (panggil run.bat,
rem       TANPA PowerShell sama sekali)
rem ============================================================
setlocal
cd /d "%~dp0"
set "PROJECT_ROOT=%~dp0.."
set "SOURCE_DIR=%PROJECT_ROOT%\desktop"
set "VENV_DIR=%SOURCE_DIR%\.venv"
set "VPY=%VENV_DIR%\Scripts\python.exe"

echo.
echo   =========================================
echo     EXAMVAN Windows Installer (CMD)
echo   =========================================
echo.

rem ---- 1. Cek python ----
echo [INFO] Memeriksa Python...
where python >nul 2>nul
if errorlevel 1 (
    echo [ERR]  Python tidak ditemukan di PATH.
    echo.
    echo       Download Python 3.12 64-bit dari https://www.python.org/downloads/
    echo       PASTIKAN centang "Add Python to PATH" saat install.
    echo.
    echo       Cek juga: where python
    echo       Kalau hasilnya WindowsApps\python.exe, PATH python.org belum masuk.
    pause
    exit /b 1
)
python --version
echo.

rem ---- 2. Venv: pakai yang sehat, buat baru hanya jika perlu ----
rem `if errorlevel` dicek dinamis (aman di dalam blok kurung, tidak butuh
rem delayed expansion). Venv sehat = python.exe ada dan import PyQt5+fitz
rem sukses -- sehingga re-run installer cepat, tidak wipe install lama.
set "NEED_SETUP=1"
if exist "%VPY%" (
    "%VPY%" -c "import PyQt5, fitz" >nul 2>nul
    if not errorlevel 1 set "NEED_SETUP=0"
)

if "%NEED_SETUP%"=="0" goto :shortcut

echo [INFO] Membuat virtual environment baru di desktop\.venv ...
if exist "%VENV_DIR%" rmdir /s /q "%VENV_DIR%"
python -m venv "%VENV_DIR%"
if errorlevel 1 (
    echo [ERR]  Gagal membuat virtual environment.
    pause
    exit /b 1
)

echo [INFO] Menginstall PyQt5 + PyMuPDF (butuh koneksi internet)...
"%VPY%" -m pip install --upgrade pip
if errorlevel 1 (
    echo [ERR]  Gagal upgrade pip. Cek koneksi internet / proxy kantor.
    pause
    exit /b 1
)
"%VPY%" -m pip install PyQt5 PyMuPDF
if errorlevel 1 (
    echo [ERR]  Gagal install dependencies. Pesan error di atas.
    echo        - Kalau ada "No matching distribution": jaringan memblokir pypi.org
    echo        - Kalau sukses tapi app tetap error nanti: install Visual C++
    echo          Redistributable: https://aka.ms/vs/17/release/vc_redist.x64.exe
    pause
    exit /b 1
)

echo [INFO] Test import PyQt5 + PyMuPDF...
"%VPY%" -c "import PyQt5, fitz; print('import OK')"
if errorlevel 1 (
    echo [ERR]  Import gagal. Biasanya karena Visual C++ Redistributable tidak ada:
    echo        https://aka.ms/vs/17/release/vc_redist.x64.exe
    pause
    exit /b 1
)
echo [OK]   Dependencies terinstall dan import OK.
echo.

:shortcut
rem ---- 3. Buat shortcut di desktop ----
rem Path di-hardcode saat install (env var user yang baru dibuat tidak
rem terlihat oleh Explorer yang sudah berjalan sampai logout/re-login).
rem Shortcut memanggil run.bat LANGSUNG (CMD murni) -- BUKAN run.ps1 via
rem powershell: klik ikon desktop tidak boleh bergantung PowerShell,
rem yang bisa gagal karena policy, PowerShell 7, atau lingkungan kantor.
for /f "tokens=2,*" %%A in ('reg query "HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Shell Folders" /v Desktop 2^>nul ^| findstr Desktop') do set "USER_DESKTOP=%%B"
if not defined USER_DESKTOP set "USER_DESKTOP=%USERPROFILE%\Desktop"

> "%USER_DESKTOP%\EXAMVAN.bat" (
    @echo @echo off
    @echo call "%~dp0run.bat"
    @echo pause
)
echo [OK]   Shortcut dibuat: %USER_DESKTOP%\EXAMVAN.bat
echo.

rem ---- 4. Selesai ----
echo   =========================================
echo     INSTALASI SELESAI!
echo   =========================================
echo.
echo   Jalankan EXAMVAN:
echo     - Double-click EXAMVAN.bat di desktop, ATAU
echo     - Double-click windows\run.bat langsung, ATAU
echo     - Dari CMD: desktop\.venv\Scripts\python.exe -m examvan  (dari folder desktop)
echo.
echo   Password admin exit (opsional, sebelum menjalankan):
echo     set EXAMVAN_ADMIN_PASSWORD=rahasia123
echo.
pause
exit /b 0
