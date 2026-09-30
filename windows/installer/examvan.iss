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
; Sumber kebenaran ikon: windows\installer\examvan.ico, di-commit.
; Regenerasi (hanya saat ikon brand berubah):
;   python windows\installer\make_icon.py
; Build TIDAK PERNAH memanggilnya — .ico yang sudah jadi ikut repo,
; jadi tidak ada syarat ImageMagick/Pillow di PC guru, siswa, atau CI.
#define AppIconName "examvan.ico"
#define SetupMutexName "EXAMVAN_Setup_Install"

; Versi/build/commit diisi oleh windows\installer\build_info.py, yang
; juga menormalkan APP_VERSION jadi format numerik. Default di sini
; hanya untuk iscc yang dipanggil tanpa define (build lokal tanpa
; build_info.py) — semua build nyata lewat build-setup.* / CI.
#ifndef AppVersion
  #define AppVersion "2.5.1"
#endif
; WAJIB format numerik X.X.X.X. Kalau diberi "v2.5.0" atau
; "2.5.0-rc1", ISCC gagal dengan "Invalid version number" — itu sebab
; kenapa build_info.py mengirim dua define terpisah, bukan satu.
#ifndef AppVersionInfo
  #define AppVersionInfo "2.5.1.0"
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
; Ikon untuk installer itu sendiri. Tanpa ini, EXAMVAN-Setup.exe — file
; yang benar-benar dibagikan ke siswa dan yang mereka klik dua kali —
; menampilkan ikon default Inno Setup, bukan ikon EXAMVAN.
SetupIconFile={#AppIconName}
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
; EXE PyInstaller onefile ~60-90 MB → butuh wizard realistis
SetupLogging=yes
UninstallLogging=yes
CloseApplications=yes
CloseApplicationsFilter=*.exe,*.bat
RestartApplications=no
; Cegah dua installer jalan bersamaan (RC Beta / multi-click).
;
; `SetupMutex`, bukan `AppMutex`. AppMutex membuat installer MENOLAK jalan
; selama aplikasi memegang mutex itu, dan mewajibkan aplikasi memanggil
; CreateMutex dengan nama yang cocok — tidak ada CreateMutex di mana pun di
; desktop/, jadi pemeriksaan itu tidak pernah bisa menyala. Mencegah dua
; installer jalan bersamaan adalah `SetupMutex`.
SetupMutex={#SetupMutexName}
MinVersion=10.0
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
; SmartScreen: exe belum code-signed. Installer ini TIDAK bisa
; menandatangani exe child-nya, jadi warning tetap mungkin muncul
; di langkah "Run" — itu perilaku Windows, bukan bug installer.
DisableWelcomePage=no

[Languages]
; HANYA bahasa bawaan. Inno Setup tidak menyertakan
; Indonesian.isl — daftar resminya ~20 bahasa (BrazilianPortuguese, French,
; German, Spanish, dll.) dan Indonesia tidak ada di sana. Merujuk ke
; compiler:Languages\Indonesian.isl membuat SELURUH compile gagal dengan
; "Couldn't open include file", bukan cuma mengganti label.
;
; Konsekuensinya tombol standar wizard (Next / Back / Install / Cancel /
; Yes / No) berbahasa Inggris, sedangkan semua teks milik EXAMVAN sendiri
; (judul halaman, deskripsi, pesan) tetap bahasa Indonesia. Untuk
; organisasi sekolah, label Inggris di 6 tombol lebih baik daripada
; installer yang gagal di-build.
;
; Kalau suatu hari Indonesia punya file .isl resmi, tambahkan di sini.
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
; Hanya satu task. Task "konfigurasi password admin exit" yang sebelumnya
; ada di sini DIHAPUS: halaman password sekarang selalu tampil, dan
; mengosongkan kedua kolom sudah cukup untuk menyatakan admin exit
; nonaktif. Checkbox yang tidak dibaca hanya menambah satu kondisi
; yang bisa salah tanpa untung nyata.
Name: "desktopicon"; Description: "Buat shortcut di Desktop"; GroupDescription: "Shortcut tambahan:"

[Files]
; EXE PyInstaller sudah onefile: tidak perlu installer framework,
; tidak perlu service, tidak perlu short path (sudah di .exe).
;
Source: "..\dist\{#AppExeName}"; DestDir: "{app}"; Flags: ignoreversion

; Ikon di-bundle ke {app} karena ketiga shortcut menunjuk
; IconFilename ke "{app}\examvan.ico". Dua hal harus benar-benar
;Template: file itu harus ADA di {app} ketika shell menulis .lnk.
;
; Versi sebelumnya memakai `Flags: dontcopy` dengan alasan "ikon cuma
; dipakai saat install, jadi jangan jadi file aplikasi". Itu salah:
; `dontcopy` berarti file TIDAK disalin ke {app}, sedangkan
; IconFilename menunjuk ke {app}\examvan.ico. Hasilnya .lnk menunjuk ke
; file yang tidak ada dan shortcut jatuh ke ikon default -- persis hal
; yang seharusnya dicegah. Smoke test CI yang menangkapnya:
; "examvan.ico tidak ikut ter-install".
;
; Ikon ikut terhapus saat uninstall, dan itu memang yang benar: file
; yang dipasang installer harus dibersihkan installer.
Source: "{#AppIconName}"; DestDir: "{app}"; Flags: ignoreversion

[Dirs]
; Dibuat supaya %LOCALAPPDATA%\EXAMVAN ada sejak instalasi — dipakai
; untuk admin_password.txt dan clipboard log.
; Folder .config\examvan (jawaban + app.log) TIDAK ada di sini.
Name: "{localappdata}\EXAMVAN"

[Icons]
; IconFilename diset eksplisit, bukan diwarisi dari {app}\EXAMVAN.exe.
; Secara mekanika warisan itu bekerja, tapi kalau build exe gagal
; diam-diam (ikon default ikut ter-bundle) semua shortcut ikut salah —
; dan tidak ada yang mengatakannya. Shortcut uninstall memakai ikon yang
; sama supaya Start Menu dan desktop terlihat satu keluarga.
Name: "{group}\{#AppName}";        Filename: "{app}\{#AppExeName}"; IconFilename: "{app}\{#AppIconName}"
Name: "{group}\Uninstall {#AppName}"; Filename: "{uninstallexe}"; IconFilename: "{app}\{#AppIconName}"
Name: "{autodesktop}\{#AppName}";  Filename: "{app}\{#AppExeName}"; IconFilename: "{app}\{#AppIconName}"; Tasks: desktopicon

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

// ------------------------------------------------------------
// Prasyarat: Visual C++ Redistributable.
// PyQt5 butuh msvcp140.dll + vcruntime140.dll. Kalau tidak ada,
// app crash dengan "DLL load failed" SETELAH installer selesai --
// user mengira instalasinya rusak. Lebih baik dicek di depan.
//
// Kenapa cek FILE, bukan registry: versi pertama memakai
//   RegQueryStringValue(HKLM, '...', 'Installed', Installed) and (Installed = '1')
// dengan `Installed: AnsiString`. Deklarasi aslinya adalah
//   RegQueryStringValue(const RootKey: HKEY; const SubKeyName, ValueName: String;
//     var ResultStr: String): Boolean
// yaitu var String, BUKAN AnsiString — jadi parameter var-nya tidak
// cocok dan ISCC melaporkan "Type mismatch" di kolom `and` (operator
// yang dilaporkan, bukan penyebabnya).
//
// Fungsi ini sekarang tidak memakai registry sama sekali. Memeriksa
// DLL-nya sendiri jauh lebih langsung: file yang hilang itu PERSIS
// penyebab "DLL load failed" yang ingin dicegah, tanpa harus menebak
// versi runtime yang terpasang.
//
// Ditulis tanpa operator boolean (early Exit) supaya tidak bergantung
// pada parsing `and` yang baru saja terbukti rewel.
// {sys} = C:\Windows\System32.
// ------------------------------------------------------------
function VCRedistPresent(): Boolean;
begin
  // KETIGA dll, bukan dua. Runtime MSVC x64 yang di-link Qt5Core.dll adalah
  // msvcp140.dll + vcruntime140.dll + vcruntime140_1.dll. Mesin dengan
  // redist lama/parsial punya dua yang pertama tapi tidak yang ketiga:
  // installer tetap diam dan siswa tetap dapat "DLL load failed" — persis
  // hasil yang dicegah oleh cek ini.
  Result := FileExists(ExpandConstant('{sys}\msvcp140.dll'))
        and FileExists(ExpandConstant('{sys}\vcruntime140.dll'))
        and FileExists(ExpandConstant('{sys}\vcruntime140_1.dll'));
end;

procedure InitializeWizard();
begin
  // Tanda tangan ASLI (docs + source Inno Setup ScriptDlg.pas):
  //
  //   function CreateInputQueryPage(const AfterID: Integer;
  //     const ACaption, ADescription, ASubCaption: String): TInputQueryWizardPage;
  //
  // EMPAT argumen, bukan enam. Field input dibuat terpisah lewat
  // Page.Add(Prompt, IsPassword), dan isinya dibaca lewat Page.Values[i].
  // Versi sebelumnya menebak enam argumen (dengan var Value + Password)
  // dan ISCC menolaknya dengan "Invalid number of parameters".
  //
  // Password WAJIB True di Add. Kalau tidak, password supervisor tampil
  // terang-terangan di layar kelas -- dan ini password yang
  // mengizinkan menutup ujian di tengah jalan.
  //
  // Halaman ini selalu tampil (tanpa checkbox task): menyisakan satu
  // kondisi lebih sedikit untuk salah, dan mengosongkan kedua kolom
  // sudah cukup untuk menyatakan "admin exit nonaktif".
  AdminPasswordPage := CreateInputQueryPage(wpSelectTasks,
    'Password Admin Exit',
    'Password admin exit',
    'Password ini dibutuhkan supervisor untuk MENUTUP ujian yang sedang berjalan.' + #13#10#13#10 +
    'Boleh dikosongkan. Bila dikosongkan, fitur admin exit nonaktif' + #13#10 +
    '(fail-closed: tidak ada password lain yang bisa dipakai).' + #13#10#13#10 +
    'Tersimpan di: %LOCALAPPDATA%\EXAMVAN\admin_password.txt' + #13#10 +
    'Bisa diubah kapan saja dengan install ulang atau tulis ulang file itu.');
  AdminPasswordPage.Add('&Password supervisor:', True);
  AdminPasswordPage.Add('&Ulangi password:', True);
end;

// ------------------------------------------------------------
// Password admin exit -> %LOCALAPPDATA%\EXAMVAN\admin_password.txt
//
// PENTING: env var EXAMVAN_ADMIN_PASSWORD tetap menang (script lama
// dan cara manual tidak berubah). File ini hanya FALLBACK supaya
// "1 klik langsung jalan" benar-benar tanpa langkah tambahan --
// kalau tidak, supervisor harus set env var tiap kali buka app.
//
// Nilai HARUS dibaca di sini, bukan di InitializeWizard: event itu
// jalan sebelum halaman tampil, jadi Values[] masih kosong.
// ------------------------------------------------------------
// ------------------------------------------------------------
// Baca password admin exit yang sudah tersimpan.
//
// Dipakai CurStepChanged supaya instalasi senyap (yang tidak menampilkan
// halaman password) bisa menulis ulang password yang sudah ada alih-alih
// menghapusnya, dan supaya upgrade interaktif menampilkan password aktif.
//
// Aman untuk file 0 byte, file yang tidak bisa dibuka, dan file tanpa
// baris kosong di akhir: semua menghasilkan string kosong, yang
// pemanggil perlakukan sebagai "tidak/password tidak dikonfigurasi".
//
// Versi sebelumnya memakai FileOpen/FileSeek/FileRead/FileClose dengan
// THandle. itu API Win32 Delphi, BUKAN Pascal Script Inno Setup — dan
// tidak ada unit `FileFunc` yang mengimpornya, jadi ISCC menolak dengan
//     Unknown identifier 'FileOpen'
// sebelum sempat dieksekusi. Serangkaian handle, try/finally, dan
// SetLength(Buffer, FileSeek(...)) yang tidak pernah bisa berjalan.
//
// `LoadStringFromFile` adalah support function yang benar-benar ada:
// membaca SELURUH isi file ke dalam S, mengembalikan False kalau file
// tidak bisa dibuka. Tepat menutup ketiga kasus yang dikomentari di
// atas tanpa satu pun alur error manual.
// ------------------------------------------------------------
function ReadPasswordFromFile(const PwFile: String): String;
var
  Buffer: AnsiString;
begin
  Result := '';
  if not FileExists(PwFile) then
    Exit;
  if not LoadStringFromFile(PwFile, Buffer) then
    Exit;
  // File ditulis tanpa newline, tapi yang diedit manual di Notepad bisa
  // punya CRLF — jadi selalu strip.
  Result := Trim(Buffer);
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  PwFile: String;
  PwDir: String;
  PwValue: String;
  PwRepeat: String;
begin
  // Tidak ada guard "WizardNoTargets" di sini (sebelumnya dipakai, tapi
  // identifier itu TIDAK ADA di Support Functions Reference Inno Setup
  // sehingga ISCC menolak dengan "Unknown identifier"). Guardnya memang
  // tidak perlu: ssPostInstall hanya akan jalan kalau instalasi benar-benar
  // berjalan, jadi membatalkan di halaman folder tidak akan sampai ke
  // sini. WizardSilent dan UninstallSilent di bawah keduanya identifier
  // yang nyata.
  if CurStep = ssPostInstall then
  begin
    PwDir := ExpandConstant('{localappdata}\EXAMVAN');
    if not DirExists(PwDir) then
      CreateDir(PwDir);
    PwFile := PwDir + '\admin_password.txt';

    // Password lama dimuat ke Values[0] HANYA untuk instalasi SENYAP.
    //
    // Versi sebelumnya melakukan ini tanpa syarat, dengan alasan "supaya
    // instalasi senyap bisa menulis ulang apa yang sudah ada". Tapi kalau
    // halamannya TAMPIL, Values[0] adalah apa yang baru saja diketik
    // pengguna -- dan kalau pengguna sengaja mengosongkan kolom, baris ini
    // menimpanya kembali dengan password lama. Akibatnya "nonaktifkan
    // password exit" tidak bisa dilakukan sama sekali: satu-satunya cara
    // adalah reinstall lalu mengetik password yang terlupa, dan password
    // lama tetap masih berlaku.
    //
    // Untuk instalasi senyap Values[] kosong bukan pilihan pengguna --
    // halamannya memang tidak pernah tampil -- jadi yang dilakukan adalah
    // mempertahankan password yang sudah ada, persis seperti "Boleh
    // dikosongkan" di teks halaman promises.
    if WizardSilent then
    begin
      if (AdminPasswordPage.Values[0] = '') and FileExists(PwFile) then
        AdminPasswordPage.Values[0] := ReadPasswordFromFile(PwFile);
    end;

    PwValue := AdminPasswordPage.Values[0];
    PwRepeat := AdminPasswordPage.Values[1];
    if PwRepeat = '' then
      PwRepeat := PwValue;      // tidak ada kolom konfirmasi = tidak ada cek

    // Dua kolom isian harus sama. Kalau tidak, JANGAN diam-diam pakai
    // yang pertama: biasanya itu salah ketik, dan password hasil salah
    // ketik = supervisor terkunci di luar kelas saat ujian berjalan.
    if (PwValue <> '') and (PwValue <> PwRepeat) then
    begin
      MsgBox('Dua password tidak sama. Password TIDAK disimpan.', mbError, MB_OK);
      // Password LAMA dikembalikan, BUKAN diganti string kosong.
      //
      // Versi sebelumnya menulis `PwValue := ''` dengan komentar "password
      // lama tetap dibiarkan". Itu tidak benar: string kosong langsung
      // jatuh ke cabang hapus di bawah, jadi salah ketik justru
      // MENGHAPUS password yang masih working -- persis kebalikan dari
      // niatnya. Mengembalikan isi lama juga membuat tulis-ulang di bawah
      // menyimpan kembali nilai yang sama, jadi file berubah nihil.
      PwValue := ReadPasswordFromFile(PwFile);
    end;

    // Hapus DAN tulis di cabang yang sama.
    //
    // DeleteFile sebelum SaveStringToFile itu wajib: SaveStringToFile
    // membuka file tanpa truncate, jadi password lama 20 karakter lalu
    // diganti yang 8 menyisakan 12 byte lama di akhir file. Akibatnya
    // password BARU ikut salah baca dan sisa password lama masih bisa
    // dibaca dari disk.
    //
    // Yang sebelumnya salah: hapus tanpa syarat, tulis bersyarat.
    //
    // Urutan argumen SaveStringToFile adalah (FileName, S, Append) --
    // nama file lebih dulu. Versi sebelumnya menulis
    // SaveStringToFile(PwValue, PwFile, False), jadi isinya
    // "rahasia-smoke-123" dan tujuannya
    // "C:\...\admin_password.txt": bersama DeleteFile di atas, file
    // password benar-benar dihapus lalu tidak pernah dibuat ulang, dan
    // keluar exit 0. Pola ini tidak ketahuan karena kedua parameter
    // bertipe String dan terisi variabel yang sama-sama valid.
    //
    // Hasil return juga WAJIB diperiksa. Menulis password tanpa
    // memverifikasi berubah jadi kehilangan data senyap: supervisor
    // terkunci di luar kelas saat ujian berjalan, tanpa pesan.
    if PwValue <> '' then
    begin
      if FileExists(PwFile) then
        DeleteFile(PwFile);
      if not SaveStringToFile(PwFile, PwValue, False) then
        RaiseException('Gagal menulis password admin exit ke:' + #13#10 +
          PwFile + #13#10#13#10 +
          'Password lama sudah dihapus dan TIDAK tersimpan. ' +
          'Set ulang password secara manual sebelum ujian berikutnya.');
    end
    else if not WizardSilent then
    begin
      // Password sengaja dikosongkan OLEH PENGGUNA di halaman yang tampil,
      // jadi password lama harus dihapus. Tanpa cabang ini, mengosongkan
      // kolom tidak melakukan apa-apa: Values[0] sudah diisi password
      // lama, jadi satu-satunya cara "menonaktifkan" password exit
      // adalah reinstall lalu mengetik password yang lupa -- dan
      // password lama tetap masih berlaku.
      //
      // Syaratnya penting: hanya untuk instalasi yang TAMPIL. Senyap
      // (/VERYSILENT) tidak pernah menampilkan halaman, Values[] kosong,
      // dan `PwValue = ''` di sana berarti "tidak tahu" -- bukan
      // "mau dihapus". Tanpa guard WizardSilent, setiap upgrade senyap
      // akan menghapus password seluruh lab -- persis R1 yang sedang
      // diuji smoke test ini.
      if FileExists(PwFile) then
        DeleteFile(PwFile);
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
  PwDir: String;
  DataDir: String;
  LogDir: String;
  Found: Boolean;
begin
  // Path DIHITUNG lebih dulu, tanpa syarat apa pun.
  //
  // Versi sebelumnya menaruh semua ExpandConstant di dalam
  // `if ... and (not UninstallSilent)`, jadi hanya berjalan saat uninstall
  // INTERAKTIF. Smoke test CI meng-uninstall dengan /VERYSILENT, jadi
  // cabang itu tidak pernah dieksekusi -- dan constant yang salah di
  // dalamnya lolos ke rilis.
  //
  // `{userprofile}` adalah constant yang TIDAK ADA di Inno Setup. Instalasi
  // tidak mengeluh karena string ini baru dievaluasi saat uninstall
  // interaktif, dan user asli justru menemukannya: "Cannot find
  // 'userprofile'". Bentuk yang benar untuk environment variable adalah
  // {%NAME} (dengan dua kurung kurawal), jadi sekarang `{%USERPROFILE}`.
  //
  // Memindahkan perhitungan ke sini berarti uninstall senyap di CI juga
  // mengevaluasinya: constant yang salah sekarang menggagalkan build.
  PwDir := ExpandConstant('{localappdata}\EXAMVAN');
  DataDir := ExpandConstant('{%USERPROFILE}\.config\examvan');
  LogDir := DataDir;

  if (CurUninstallStep = usPostUninstall) and (not UninstallSilent) then
  begin
    // DUA folder, dan isinya harus disebut apa adanya.
    //
    // %LOCALAPPDATA%\EXAMVAN hanya berisi admin_password.txt.
    // %USERPROFILE%\.config\examvan yang holding jawaban ujian yang belum
    // terkirim, config (URL server + token + identitas), app.log, dan
    // windows_state.json. Prompt lama hanya menyebut yang pertama tapi
    // mendeskripsikannya sebagai holding empat hal — jadi "Ya" tidak
    // menghapus apa pun, dan "No" (untuk melindungi jawaban) tidak
    // melindungi apa pun juga.
    Found := False;
    if DirExists(PwDir) then
      Found := True;
    if DirExists(DataDir) then
      Found := True;

    if Found then
    begin
      if MsgBox('Folder data EXAMVAN berikut masih ada:' + #13#10#13#10 +
                PwDir + #13#10 +
                '  -> password admin exit' + #13#10#13#10 +
                LogDir + #13#10 +
                '  -> jawaban ujian yang belum terkirim, config (URL server,' + #13#10 +
                '     token, identitas), app.log, windows_state.json' + #13#10#13#10 +
                'Hapus KEDUA folder ini juga?' + #13#10#13#10 +
                'Pilih No bila masih ada jawaban yang belum terkirim.',
                mbConfirmation, MB_YESNO) = IDYES then
      begin
        if DirExists(PwDir) then
          DelTree(PwDir, True, True, True);
        if DirExists(DataDir) then
          DelTree(DataDir, True, True, True);
      end;
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
