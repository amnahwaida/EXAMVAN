@echo off
rem ============================================================
rem  EXAMVAN Windows Builder (.exe portable) -- 100% CMD
rem
rem  Usage (dari folder windows\):
rem    build-exe.bat
rem
rem  Hasil: windows\dist\EXAMVAN.exe -- standalone ~60 MB,
rem  bisa di-copy ke komputer lain TANPA install Python.
rem
rem  Prasyarat: Python 3.10+ 64-bit di PATH + koneksi internet.
rem  Catatan: venv desktop\.venv dihapus & dibuat ulang bersih
rem  (sama seperti build-exe.ps1). Jalankan run.bat lagi setelah
rem  build bila ingin lanjut pakai mode source.
rem ============================================================
setlocal
cd /d "%~dp0"
set "SOURCE_DIR=%~dp0..\desktop"
set "OUTPUT_DIR=%~dp0dist"
set "VENV_DIR=%SOURCE_DIR%\.venv"
set "VPY=%VENV_DIR%\Scripts\python.exe"

echo.
echo   =========================================
echo     EXAMVAN Windows Builder (CMD)
echo   =========================================
echo.

rem ---- Cek python ----
where python >nul 2>nul
if errorlevel 1 (
    echo [ERR] Python tidak ditemukan di PATH.
    echo       Download Python 3.12 64-bit dari https://www.python.org/downloads/
    echo       Pastikan centang "Add Python to PATH" saat install.
    pause
    exit /b 1
)
python --version
echo.

rem ---- Venv bersih ----
echo [INFO] Membuat virtual environment bersih...
if exist "%VENV_DIR%" rmdir /s /q "%VENV_DIR%"
python -m venv "%VENV_DIR%"
if errorlevel 1 (
    echo [ERR] Gagal membuat virtual environment.
    pause
    exit /b 1
)

rem ---- Dependencies ----
echo [INFO] Menginstall PyQt5 + PyMuPDF + PyInstaller...
"%VPY%" -m pip install --upgrade pip --quiet
if errorlevel 1 goto :pipfail
"%VPY%" -m pip install PyQt5 PyMuPDF pyinstaller --quiet
if errorlevel 1 goto :pipfail
echo [OK]   Dependencies siap.
goto :build

:pipfail
echo [ERR]  Gagal install dependencies. Cek koneksi internet / proxy kantor.
echo        Coba manual tanpa --quiet untuk melihat error:
echo        "%VPY%" -m pip install PyQt5 PyMuPDF pyinstaller
pause
exit /b 1

:build
echo [INFO] Build EXAMVAN.exe dengan PyInstaller -- butuh beberapa menit...
if not exist "%OUTPUT_DIR%" mkdir "%OUTPUT_DIR%"

rem Entry point = desktop\main.py stub, BUKAN examvan\__main__.py:
rem PyInstaller menjalankan file entry sebagai script lepas, sedangkan
rem __main__.py memakai relative import yang crash saat exe dijalankan.
rem --add-data relatif ke CWD, jadi CWD wajib folder desktop\.
pushd "%SOURCE_DIR%"
"%VPY%" -m PyInstaller ^
    --onefile ^
    --windowed ^
    --name "EXAMVAN" ^
    --distpath "%OUTPUT_DIR%" ^
    --specpath "%OUTPUT_DIR%" ^
    --workpath "%OUTPUT_DIR%\build" ^
    --hidden-import examvan ^
    --hidden-import examvan.security ^
    --hidden-import examvan.ui ^
    --hidden-import examvan.ui.styles ^
    --hidden-import examvan.ui.timer ^
    --hidden-import examvan.ui.pdf_viewer ^
    --hidden-import examvan.ui.answer_sheet ^
    --hidden-import examvan.ui.exam_viewer ^
    --hidden-import examvan.ui.identity_dialog ^
    --hidden-import examvan.ui.server_config ^
    --hidden-import examvan.security.base ^
    --hidden-import examvan.security.enforcer ^
    --hidden-import examvan.security.windows_backend ^
    --exclude-module examvan.security.x11 ^
    --exclude-module examvan.security.linux_backend ^
    --exclude-module examvan.security.kiosk ^
    --add-data "examvan;examvan" ^
    main.py
set "RC=%ERRORLEVEL%"
popd

if not "%RC%"=="0" (
    echo [ERR] PyInstaller gagal dengan exit code %RC%. Lihat pesan di atas.
    pause
    exit /b %RC%
)

rem ---- Verifikasi hasil ----
for %%F in ("%OUTPUT_DIR%\EXAMVAN.exe") do set "SIZE=%%~zF"
if not defined SIZE (
    echo [ERR] Build selesai tapi EXAMVAN.exe tidak ditemukan di %OUTPUT_DIR%
    pause
    exit /b 1
)
set /a SIZE_MB=%SIZE% / 1048576
echo.
echo   =========================================
echo     BUILD SUCCESS
echo   =========================================
echo.
echo   Executable: %OUTPUT_DIR%\EXAMVAN.exe
echo   Size      : ~%SIZE_MB% MB
echo.
echo   Copy ke USB / komputer lain, jalankan tanpa install Python.
echo   Catatan: exe build lokal tidak di-commit -- exe resmi tersedia
echo   di release GitHub "exe-latest" hasil build CI.
echo.
pause
exit /b 0
