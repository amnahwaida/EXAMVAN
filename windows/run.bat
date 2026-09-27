@echo off
rem ============================================================
rem  EXAMVAN Windows Launcher -- 100% CMD, tanpa PowerShell
rem
rem  Usage:
rem    run.bat            (dari folder windows\)
rem    atau double-click  (auto setup venv saat pertama jalan)
rem
rem  Catatan: script ini memakai pushd ke desktop\ sebelum launch --
rem  package examvan TIDAK di-install ke venv, jadi `python -m examvan`
rem  hanya resolve bila CWD adalah folder desktop\.
rem ============================================================
setlocal
cd /d "%~dp0"
set "SOURCE_DIR=%~dp0..\desktop"
set "VENV_DIR=%SOURCE_DIR%\.venv"
set "VPY=%VENV_DIR%\Scripts\python.exe"

rem ---- Cek python ----
where python >nul 2>nul
if errorlevel 1 (
    echo [ERR] Python tidak ditemukan di PATH.
    echo       Download Python 3.12 64-bit dari https://www.python.org/downloads/
    echo       Pastikan centang "Add Python to PATH" saat install.
    pause
    exit /b 1
)

rem ---- Setup venv jika belum ada ----
if exist "%VPY%" goto :venv_ok
echo [INFO] Membuat virtual environment + install dependencies (sekali saja)...
if exist "%VENV_DIR%" rmdir /s /q "%VENV_DIR%"
python -m venv "%VENV_DIR%"
if errorlevel 1 (
    echo [ERR] Gagal membuat virtual environment.
    pause
    exit /b 1
)
"%VPY%" -m pip install --upgrade pip --quiet
"%VPY%" -m pip install PyQt5 PyMuPDF --quiet
if errorlevel 1 (
    echo [ERR] Gagal install dependencies. Coba manual:
    echo       "%VPY%" -m pip install PyQt5 PyMuPDF
    pause
    exit /b 1
)
echo [OK]   Dependencies siap.
goto :run

:venv_ok
rem ---- Cek PyQt5; install kalau belum ada ----
"%VPY%" -c "import PyQt5" >nul 2>nul
if errorlevel 1 (
    echo [INFO] PyQt5 belum terinstall, menginstall...
    "%VPY%" -m pip install PyQt5 PyMuPDF --quiet
)

:run
echo [INFO] Menjalankan EXAMVAN...
title EXAMVAN
pushd "%SOURCE_DIR%"
"%VPY%" -m examvan
set "RC=%ERRORLEVEL%"
popd
exit /b %RC%
