; ============================================================
;  EXAMVAN Windows Installer — Inno Setup
;
;  Hasil: EXAMVAN-Setup.exe — SATU FILE, tidak butuh Python,
;  tidak butuh install apa-apa di PC siswa. Double-click → jalan.
;
;  Build (butuh Windows + Inno Setup 6, ATAU otomatis di CI):
;    windows\build-setup.bat            (CMD murni)
;    powershell -ExecutionPolicy Bypass -File windows\build-setup.ps1
;
;  Input : windows\dist\EXAMVAN.exe   (dihasilkan build-exe.bat / CI)
;  Output: windows\dist\EXAMVAN-Setup.exe
;
;  Prinsip: PrivilegesRequired=lowest → install per-user di
;  %LOCALAPPDATA%\Programs\EXAMVAN, TANPA dialog UAC, tanpa hak
;  admin. PC sekolah sering diblokir policy: install per-user
;  tetap jalan di situation di mana install ke Program Files
;  akan ditolak.
; ============================================================

#define AppName "EXAMVAN"
#define AppShortName "EXAMVAN"
#define AppPublisher "EXAMVAN"
#define AppExeName "EXAMVAN.exe"
#define AppMutex "EXAMVAN_Setup_Install"

; Versi/build/commit diisi oleh windows\installer\build_info.py, yang
; juga menormalkan APP_VERSION jadi format numerik. Default di sini
; hanya untuk iscc yang dipanggil tanpa define (build lokal tanpa
; build_info.py) — semua build nyata lewat build-setup.* / CI.
#ifndef AppVersion
  #define AppVersion "2.5.0"
#endif
; WAJIB format numerik X.X.X.X. Kalau diberi "v2.5.0" atau
; "2.5.0-rc1", ISCC gagal dengan "Invalid version number" — itu sebab
; kenapa build_info.py mengirim dua define terpisah, bukan satu.
#ifndef AppVersionInfo
  #define AppVersionInfo "2.5.0.0"
#endif
#ifndef AppBuild
  #define AppBuild "0"
#endif
#ifndef AppCommit
  #define AppCommit "local"
#endif

[Setup]
AppId={{7C3F1B0E-9A2D-4E51-8B7C-1D6E4A9F2B30}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} {#AppVersion} (build {#AppBuild}, commit {#AppCommit})
AppPublisher={#AppPublisher}
VersionInfoVersion={#AppVersionInfo}
VersionInfoDescription={#AppName} - Aplikasi Ujian Digital
; PENTING: lowest = per-user, tanpa UAC. Jangan diubah ke admin
; kecuali target environment dijamin boleh menulis HKLM.
PrivilegesRequired=lowest
DefaultDirName={localappdata}\Programs\{#AppShortName}
DefaultGroupName={#AppShortName}
DisableProgramGroupPage=yes
DisableDirPage=no
OutputDir=..\dist
OutputBaseFilename=EXAMVAN-Setup
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
; EXE PyInstaller onefile ~60-90 MB → butuh wizard realistis
SetupLogging=yes
UninstallLogging=yes
CloseApplications=yes
CloseApplicationsFilter=*.exe,*.bat
RestartApplications=no
; Cegah dua installer jalan bersamaan (RC Beta / multi-click)
AppMutex={#AppMutex}
MinVersion=10.0
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
; SmartScreen: exe belum code-signed. Installer ini TIDAK bisa
; menandatangani exe child-nya, jadi warning tetap mungkin muncul
; di langkah "Run" — itu perilaku Windows, bukan bug installer.
DisableWelcomePage=no

[Languages]
Name: "indonesian"; MessagesFile: "compiler:Languages\Indonesian.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "Buat shortcut di Desktop"; GroupDescription: "Shortcut tambahan:"
Name: "adminpw";     Description: "Konfigurasi password admin exit (supervisor)"; GroupDescription: "Konfigurasi:"; Flags: unchecked

[Files]
; EXE PyInstaller sudah onefile: tidak perlu installer framework,
; tidak perlu service, tidak perlu short path (sudah di .exe).
;
; Icon TIDAK dikopi terpisah. Shortcut mengambil icon dari
; EXAMVAN.exe itu sendiri (IconFilename tidak di-set = default),
; jadi file .png tidak perlu, dan tidak perlu menariknya dari
; desktop\pkg-build\ (pohon yang sudah basi).
Source: "..\dist\{#AppExeName}"; DestDir: "{app}"; Flags: ignoreversion

[Dirs]
; Dibuat supaya %LOCALAPPDATA%\EXAMVAN ada sejak instalasi — dipakai
; untuk admin_password.txt dan clipboard log.
; Folder .config\examvan (jawaban + app.log) TIDAK ada di sini.
Name: "{localappdata}\EXAMVAN"

[Icons]
Name: "{group}\{#AppName}";        Filename: "{app}\{#AppExeName}"
Name: "{group}\Uninstall {#AppName}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#AppName}";  Filename: "{app}\{#AppExeName}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#AppExeName}"; Description: "Jalankan {#AppName} sekarang"; \
    Flags: nowait postinstall skipifsilent

[UninstallDelete]
; Sisa file/day yang tidak tercatat di [Files] (log launcher, .spec PyInstaller)
Type: filesandordirs; Name: "{app}\*.log"
Type: filesandordirs; Name: "{app}\__pycache__"

[Code]
var
  AdminPasswordPage: TInputQueryWizardPage;
  AdminPasswordValue: String;

// ------------------------------------------------------------
// Prasyarat: Visual C++ Redistributable.
// PyQt5 butuh msvcp140/vcruntime140. Kalau belum ada, app akan
// crash dengan "DLL load failed" SETELAH installer selesai --
// user mengira instalasinya rusak. Lebih baik dicek di depan.
// ------------------------------------------------------------
function VCRedistPresent(): Boolean;
var
  Installed: AnsiString;
begin
  Result := RegQueryStringValue(HKLM,
    'SOFTWARE\Microsoft\VisualStudio\14.0\VC\Runtimes\x64',
    'Installed', Installed) and (Installed = '1');
end;

procedure InitializeWizard();
begin
  AdminPasswordValue := '';
  // Catatan urutan argumen CreateInputQueryPage:
  //   ParentID, Caption, Description, Prompt, var Value, Password
  // Password WAJIB True. Kalau tidak, password supervisor tampil
  // terang-terangan di layar kelas -- dan ini password yang
  // meny Allowing_close-without-password-middle-of-exam.
  AdminPasswordPage := CreateInputQueryPage(wpSelectTasks,
    'Password Admin Exit',
    'Password admin exit',
    'Password ini dibutuhkan supervisor untuk MENUTUP ujian yang sedang berjalan.' + #13#10#13#10 +
    'Boleh dikosongkan. Bila dikosongkan, fitur admin exit nonaktif' + #13#10 +
    '(fail-closed: tidak ada password lain yang bisa dipakai).' + #13#10#13#10 +
    'Tersimpan di: %LOCALAPPDATA%\EXAMVAN\admin_password.txt' + #13#10 +
    'Bisa diubah kapan saja dengan install ulang atau tulis ulang file itu.',
    'Password supervisor:',
    AdminPasswordValue,
    True);
end;

// ------------------------------------------------------------
// Password admin exit -> %LOCALAPPDATA%\EXAMVAN\admin_password.txt
//
// PENTING: env var EXAMVAN_ADMIN_PASSWORD tetap menang (script lama
// dan cara manual tidak berubah). File ini hanya FALLBACK supaya
// "1 klik langsung jalan" benar-benar tanpa langkah tambahan --
// kalau tidak, supervisor harus set env var tiap kali buka app.
// ------------------------------------------------------------
procedure CurStepChanged(CurStep: TSetupStep);
var
  PwFile: String;
  PwDir: String;
begin
  if (CurStep = ssPostInstall) and (not WizardNoTargets) then
  begin
    if WizardIsTaskSelected('adminpw') then
    begin
      PwDir := ExpandConstant('{localappdata}\EXAMVAN');
      if not DirExists(PwDir) then
        CreateDir(PwDir);
      PwFile := PwDir + '\admin_password.txt';

      // HAPUS DULU sebelum tulis. SaveStringToFile membuka file tanpa
      // truncate: password lama 20 karakter lalu diganti yang 8 akan
      // menyisakan 12 byte lama di akhir file. Akibatnya password
      // BARU ikut salah baca (file jadi 20 karakter) dan sisa
      // password lama masih bisa dibaca dari disk.
      if FileExists(PwFile) then
        DeleteFile(PwFile);

      if AdminPasswordValue <> '' then
        SaveStringToFile(AdminPasswordValue, PwFile, False);
    end;
  end;
end;

// ------------------------------------------------------------
// Uninstall: tawarkan hapus data siswa.
// Default TIDAK dihapus -- jawaban ujian yang belum terkirim dan
// log diagnosis sering dibutuhkan Berminggu-minggu setelah ujian.
//
// WAJIB DILINDUNGI UninstallSilent: MsgBox TETAP MUNCUL saat
// /VERYSILENT, jadi tanpa guard ini uninstall senyap (CI smoke
// test, uninstaller otomatis) akan MENGGANTUNG selamanya.
// ------------------------------------------------------------
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  DataDir: String;
begin
  if (CurUninstallStep = usPostUninstall) and (not UninstallSilent) then
  begin
    DataDir := ExpandConstant('{localappdata}\EXAMVAN');
    if DirExists(DataDir) then
    begin
      if MsgBox('Folder data EXAMVAN berikut masih ada:' + #13#10#13#10 +
                DataDir + #13#10#13#10 +
                'Isinya: konfigurasi server, password admin exit,' + #13#10 +
                'jawaban ujian yang belum terkirim, dan app.log' + #13#10 +
                '(untuk melapor masalah).' + #13#10#13#10 +
                'Hapus folder ini juga?',
                mbConfirmation, MB_YESNO) = IDYES then
        DelTree(DataDir, True, True, True);
    end;
  end;
end;

// ------------------------------------------------------------
// Instalasi selesai tapi app belum tentu bisa jalan.
// Beri tahu sekali, di tempat yang jelas, bukan 5 menit kemudian
// saat siswa klik dan dapat "DLL load failed".
// ------------------------------------------------------------
procedure CurPageChanged(CurPageID: Integer);
begin
  if (CurPageID = wpFinished) and (not WizardSilent) then
  begin
    if not VCRedistPresent() then
      MsgBox('Instalasi selesai, TETAPI Visual C++ Redistributable belum ada di PC ini.' + #13#10#13#10 +
             'Aplikasi kemungkinan gagal start dengan pesan "DLL load failed".' + #13#10 +
             'Perbaiki: buka https://aka.ms/vs/17/release/vc_redist.x64.exe' + #13#10 +
             '(klik dua kali, pilih "I agree", Install).' + #13#10#13#10 +
             'Satu kali saja per PC -- setelah itu tidak perlu diulang.',
             mbError, MB_OK);
  end;
end;
