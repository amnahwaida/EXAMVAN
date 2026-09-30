r"""R6: identifier, signature, dan alur Pascal Script di [Code] harus benar.

Tiga kelas bug yang hanya bisa ketahuan dengan compiles, dan tidak
pernah tertangkap karena test lama hanya membaca `.iss` sebagai teks.

1. Identifier yang tidak ada
---------------------------
`ReadPasswordFromFile` memakai `FileOpen`, `FileSeek`, `FileRead`,
`FileClose`, `FileEnd`, `FileOpenExisting`, `FileShareReadWrite`,
`FileShareDelete`, dan `THandle`. Semuanya API Win32 Delphi, bukan
Pascal Script Inno Setup. Build berhenti dengan:

    Error on line 261 in ...examvan.iss: Column 13:
    Unknown identifier 'FileOpen'

Tidak ada unit `FileFunc` yang bisa mengimpornya -- unit itu tidak ada di
distribusi Inno Setup sama sekali, jadi `uses FileFunc;` hanya menghasilkan
error lain yang lebih membingungkan.

2. Argumen yang tertukar, padahal compiles
-----------------------------------------
examvan.iss menulis `SaveStringToFile(PwValue, PwFile, False)`, sedangkan
signature resminya

    function SaveStringToFile(const FileName: String;
                              const S: AnsiString; const Append: Boolean): Boolean;

Kedua parameter bertipe String dan isinya variabel yang sama-sama valid,
jadi ISCC tidak keberatan. Digabung dengan `DeleteFile` di baris
sebelumnya, file password dihapus lalu ditulis ke file yang namanya justru
isi password -- dan instalasi keluar exit 0. Smoke test R1 di CI menangkap
akibatnya ("password hilang setelah upgrade senyap"), bukan penyebabnya.

3. Sumber Pascal di luar komentar
----------------------------------
Satu baris penjelasan kehilangan awalan `//`, jadi `%USERPROFILE%\.config
\examvan yang holding jawaban ujian yang belum` duduk di tengah blok
`begin`/`end`. Di Pascal Script `%` adalah operator modulo, jadi `%` di luar
string literal selalu salah.

Kenapa test teks biasa tidak bisa menangkap ketiganya
----------------------------------------------------
`assertIn("FileExists", code)` hanya tahu NAMA fungsi, bukan ARTINYA.
Tidak ada mesin Linux yang bisa menjalankan ISCC, jadi semua ini harus
diperiksa sebagai ATURAN, bukan sebagai teks: identifier harus terdaftar,
jumlah argumen harus cocok, argumen path harus benar-benar path, dan
konstanta harus berada di posisi yang benar.

Test yang tetap perlu CI: `.iss` tidak pernah dikompilasi di repo ini.
Semua yang di bawah adalah pemeriksaan statis — bukan pengganti ISCC.

Referensi: https://jrsoftware.org/ishelp/topic_scriptfunctions.htm
"""

from __future__ import annotations

import re
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
ISS = REPO / "windows/installer/examvan.iss"
REFERENCE_URL = "https://jrsoftware.org/ishelp/topic_scriptfunctions.htm"

# Placeholder untuk isi string literal. NUL tidak mungkin muncul di .iss.
_NUL = "\0"

# --- (1) Support Functions Reference, diambil verbatim dari halaman resmi.
# Dikelompokkan per kategori supaya saat Inno menambah fungsi baru, titik
# perubahan-nya kelihatan.
SUPPORT_FUNCTIONS = set("""
GetCmdTail ParamCount ParamStr ActiveLanguage CustomMessage FmtMessage
SetupMessage WizardDirValue WizardGroupValue WizardNoIcons WizardSetupType
WizardSelectedComponents WizardIsComponentSelected WizardSelectedTasks
WizardIsTaskSelected WizardSilent IsUninstaller UninstallSilent
CurrentFilename CurrentSourceFilename ExpandConstant ExpandConstantEx
GetPreviousData SetPreviousData Terminated Debugging
RegisterExtraCloseApplicationsResource RmSessionStarted GetWizardForm
GetUninstallProgressForm
WizardSelectComponents WizardSelectTasks WizardSetBackImage
Abort RaiseException RaiseLastException GetExceptionMessage
ShowExceptionMessage
IsAdmin IsAdminInstallMode IsWinDark IsDarkInstallMode HighContrastActive
GetWindowsVersion GetWindowsVersionEx GetWindowsVersionString IsWin64
Is64BitInstallMode ProcessorArchitecture IsArm32Compatible IsArm64
IsX64Compatible IsX64OS IsX86Compatible IsX86OS IsCurrentProcess64Bit
InstallOnThisVersion IsDotNetInstalled IsMsiProductInstalled GetEnv
GetUserNameString GetComputerNameString GetUILanguage FontExists
FindWindowByClassName FindWindowByWindowName SendMessage PostMessage
SendNotifyMessage RegisterWindowMessage SendBroadcastMessage
PostBroadcastMessage SendBroadcastNotifyMessage CreateMutex
CheckForMutexes MakePendingFileRenameOperationsChecksum CreateCallback
UnloadDLL DLLGetLastError
Chr Ord Copy Length LowerCase UpperCase AnsiLowerCase AnsiUpperCase
StringOfChar Delete Insert StringChange StringChangeEx Pos RPos AddQuotes
RemoveQuotes ConvertPercentStr AddPeriod CompareText CompareStr SameText
SameStr IsWildcard WildcardMatch Format Trim TrimLeft TrimRight StringJoin
StringSplit StringSplitEx StrToIntDef StrToInt StrToInt64Def StrToInt64
StrToUInt64Def StrToUInt64 StrToFloat StrToColor IntToStr UIntToStr FloatToStr
CharLength AddBackslash RemoveBackslashUnlessRoot RemoveBackslash
PathCombine PathHasInvalidCharacters PathIsRooted PathNormalizeSlashes
PathSame PathStartsWith PathEndsWith ChangeFileExt ExtractFileExt
ExtractFileDir ExtractFilePath ExtractFileName ExtractFileDrive
ExtractRelativePath ExpandFileName ExpandUNCFileName PathConvertNormalToSuper
PathConvertSuperToNormal GetDateTimeString SetLength CharToOemBuff OemToCharBuff
Utf8Encode Utf8Decode GetMD5OfString GetMD5OfUnicodeString GetSHA1OfString
GetSHA1OfUnicodeString GetSHA256OfString GetSHA256OfUnicodeString
SysErrorMessage MinimizePathName
GetArrayLength SetArrayLength GetSHA256OfStream
Null Unassigned VarIsEmpty VarIsClear VarIsNull VarType VarArrayGet
VarArraySet
DirExists FileExists FileOrDirExists FileSize FileSize64 GetSpaceOnDisk
GetSpaceOnDisk64 FileSearch FindFirst FindNext FindClose GetCurrentDir
SetCurrentDir GetWinDir GetSystemDir GetSysWow64Dir GetSysNativeDir
GetTempDir GetShellFolderByCSIDL GetShortName GenerateUniqueName
IsProtectedSystemFile ApplyPathRedirRulesForCurrentProcess ApplyPathRedirRules
EnableFsRedirection
Exec ExecAsOriginalUser ShellExec ShellExecAsOriginalUser
ExecWithNativeSysDir ExtractTemporaryFile ExtractTemporaryFiles
DownloadTemporaryFile DownloadTemporaryFileWithISSigVerify
SetDownloadCredentials DownloadTemporaryFileSize DownloadTemporaryFileDate
ExtractArchive MapArchiveExtensions GetMD5OfFile GetSHA1OfFile
GetSHA256OfFile ISSigVerify RenameFile CopyFile DeleteFile DelayDeleteFile
SetNTFSCompression LoadStringFromFile LoadStringFromLockedFile
LoadStringsFromFile LoadStringsFromLockedFile SaveStringToFile
SaveStringsToFile SaveStringsToUTF8File SaveStringsToUTF8FileWithoutBOM
CreateDir ForceDirectories RemoveDir DelTree CreateShellLink UnpinShellLink
RegisterServer UnregisterServer RegisterTypeLibrary UnregisterTypeLibrary
IncrementSharedCount DecrementSharedCount RestartReplace UnregisterFont
ModifyPifFile
GetVersionNumbers GetVersionComponents GetVersionNumbersString
GetPackedVersion ComparePackedVersion SamePackedVersion UnpackVersionNumbers
UnpackVersionComponents VersionToStr StrToVersion
RegKeyExists RegValueExists RegGetSubkeyNames RegGetValueNames
RegQueryStringValue RegQueryMultiStringValue RegQueryDWordValue
RegQueryBinaryValue RegWriteStringValue RegWriteExpandStringValue
RegWriteMultiStringValue RegWriteDWordValue RegWriteBinaryValue
RegDeleteKeyIncludingSubkeys RegDeleteKeyIfEmpty RegDeleteValue
IniKeyExists IsIniSectionEmpty GetIniBool GetIniInt GetIniString SetIniBool
SetIniInt SetIniString DeleteIniSection DeleteIniEntry
CreateInputQueryPage CreateInputOptionPage CreateInputDirPage
CreateInputFilePage CreateOutputMsgPage CreateOutputMsgMemoPage
CreateOutputProgressPage CreateOutputMarqueeProgressPage CreateDownloadPage
CreateExtractionPage CreateCustomPage CreateCustomForm PageFromID
PageIndexFromID ScaleX ScaleY InitializeBitmapButtonFromIcon
InitializeBitmapImageFromIcon InitializeBitmapButtonFromStockIcon
InitializeBitmapImageFromStockIcon
MsgBox SuppressibleMsgBox TaskDialogMsgBox SuppressibleTaskDialogMsgBox
GetOpenFileName GetOpenFileNameMulti GetSaveFileName BrowseForFolder
ExitSetupMsgBox SelectDisk
CreateOleObject GetActiveOleObject IDispatchInvoke CreateComObject
StringToGUID OleCheck CoFreeUnusedLibraries
Log LogFmt ExecAndCaptureOutput ExecAndLogOutput
ExecAndCaptureOutputWithNativeSysDir ExecAndLogOutputWithNativeSysDir
Sleep Random Beep Abs Round Trunc Int MulDiv Set8087CW Get8087CW Low High
SizeOf Assigned Inc Dec Succ Pred Include Exclude BringToFrontAndRestore
LoadDLL CallDLLProc FreeDLL CastStringToInteger CastIntegerToString
""".split())

# --- Event handler [Code] yang sah (bukan support function).
EVENT_HANDLERS = {
    "InitializeSetup", "InitializeWizard", "InitializeUninstall",
    "CurPageChanged", "CurStepChanged", "CurUninstallStepChanged",
    "NextButtonClick", "BackButtonClick", "PrepareToInstall",
    "RegisterPreviousData", "ShouldSkipPage", "CancelButtonClick",
    "UninstallNeedRestart", "NeedRestart", "ShouldSkip", "DeinitializeSetup",
    "DeinitializeWizard", "RegisterExtraCloseApplicationsResources",
    "PrepareToInstallPages", "WizardFormCreated", "WizardFormDestroyed",
    "CurPageCreate", "CurPageDestroy", "PopupMsg", "SequenceBreak",
    "LanguageChange", "CodeRunnerError",
}

# --- Keyword Pascal + tipe dasar.
PASCAL_KEYWORDS = {
    "if", "then", "else", "begin", "end", "case", "of", "while", "do", "for",
    "to", "downto", "repeat", "until", "with", "var", "const", "type",
    "procedure", "function", "uses", "and", "or", "not", "xor", "div", "mod",
    "shl", "shr", "in", "is", "as", "nil", "true", "false", "self", "out",
    "raise", "try", "except", "finally", "array", "record", "set", "string",
    "integer", "boolean", "cardinal", "byte", "word", "char", "ansistring",
    "widestring", "longint", "int64", "single", "double", "extended",
    "nativeint", "longword", "shortint", "real", "textfile", "pointer",
}

# --- Tipe yang disediakan Inno Setup / RemObjects Pascal Script.
KNOWN_TYPES = {
    "TInputQueryWizardPage", "TInputOptionWizardPage", "TInputDirWizardPage",
    "TInputFileWizardPage", "TOutputMsgWizardPage", "TOutputProgressWizardPage",
    "TDownloadWizardPage", "TExtractionWizardPage", "TWizardPage",
    "TSetupForm", "TSetupStep", "TUninstallStep", "TSetupMessageID",
    "TSetupProcessorArchitecture", "TWindowsVersion", "TFont", "TColor",
    "TMsgBoxType", "TArrayOfString", "TArrayOfGraphic", "TArrayOfInteger",
    "TStrings", "TStringList", "TStream", "TFileStream", "TSetupApp",
    "TBitmapButton", "TBitmapImage", "TWizardForm", "TUninstallProgressForm",
    "TFindRec", "TPathRedirTargetEngine", "TPathRedirTarget", "TExecWait",
    "TExecOutput", "TOnLog", "TOnProgress", "TOnDownloadProgress",
    "TOnExtractionProgress", "TObject", "TClass", "AnyMethod", "AnyString",
    "Variant", "TGUID", "HKEY", "HMODULE", "HWND", "HRESULT", "LPARAM",
    "WPARAM", "LRESULT", "TDateTime",
}

# --- Konstanta MsgBox, TMsgBoxType, dan ShowCmd.
KNOWN_CONSTANTS = {
    "MB_OK", "MB_OKCANCEL", "MB_ABORTRETRYIGNORE", "MB_YESNOCANCEL",
    "MB_YESNO", "MB_RETRYCANCEL", "MB_DEFBUTTON1", "MB_DEFBUTTON2",
    "MB_DEFBUTTON3", "MB_SETFOREGROUND",
    "IDOK", "IDCANCEL", "IDABORT", "IDRETRY", "IDIGNORE", "IDYES", "IDNO",
    "mbError", "mbConfirmation", "mbInformation", "mbCritical",
    "SW_SHOW", "SW_SHOWNORMAL", "SW_SHOWMAXIMIZED", "SW_SHOWMINIMIZED",
    "SW_SHOWMINNOACTIVE", "SW_HIDE",
}

# --- Konstanta wizard page / step.
KNOWN_STEP_CONSTANTS = {
    "ssInstall", "ssPostInstall", "ssDone", "ssPrepareToInstall",
    "usPostUninstall", "usUninstall",
    "wpWelcome", "wpSelectDir", "wpSelectGroup", "wpSelectComponents",
    "wpSelectTasks", "wpSelectProgramGroup", "wpReady", "wpInstalling",
    "wpFinished", "wpInfoBefore", "wpInfoAfter",
}

# --- (2) Signature support function yang bisa tertukar argumennya.
# Hanya fungsi yang parameternya STRING/AnyString semua dan urutannya
# sering tertukar. Bukan daftar lengkap.
#
# Nilai: nama parameter resminya, sesuai Support Functions Reference.
SIGNATURES = {
    "SaveStringToFile": ["filename", "content", "append"],
    "LoadStringFromFile": ["filename", "content"],
    "SaveStringsToFile": ["filename", "content", "append"],
    "LoadStringsFromFile": ["filename", "content"],
    "SaveStringsToUTF8File": ["filename", "content", "append"],
    "CopyFile": ["existingfile", "newfile"],
    "RenameFile": ["oldname", "newname"],
    "PathCombine": ["dir", "filename"],
    "ChangeFileExt": ["filename", "extension"],
    "DeleteFile": ["filename"],
    "FileExists": ["name"],
    "CreateDir": ["dir"],
    "DelTree": ["path", "isdir", "deletefiles", "delsubdirsalso"],
    "ExpandConstant": ["s"],
    "MsgBox": ["text", "type", "buttons"],
    "FileSize": ["name", "size"],
    "ExtractFileName": ["filename"],
    "ExtractFilePath": ["filename"],
}

# --- (3) Jenis tiap parameter, untuk fungsi yang posisinya bisa tertukar
# tanpa mengubah tipe. MsgBox(Text, Typ, Buttons): Typ harus konstanta
# mb*, Buttons harus MB_*/ID*, dan menukarnya tetap compiles.
PARAM_KINDS = {
    "MsgBox": ["free", "mbconst", "buttonsconst"],
    "DelTree": ["free", "bool", "bool", "bool"],
}

_FREE_KINDS = {"free", "value", "path"}

_KIND_RE = {
    "mbconst": re.compile(r"^mb[A-Z]"),
    "buttonsconst": re.compile(r"^(MB_|ID)"),
    "bool": re.compile(r"^(True|False)$", re.IGNORECASE),
}

_BARE_IDENT = re.compile(r"^[A-Za-z_]\w*$")
_PATH_NAME = re.compile(r"(File|Dir|Path)$", re.IGNORECASE)

# Parameter pertama yang harus menerima path.
_PATH_FIRST = {"filename", "path", "dir", "name", "oldname", "existingfile"}

# API Win32 Delphi yang TIDAK ada di Pascal Script Inno Setup. Didaftarkan
# eksplisit supaya pesan kegagalan menyebut API-nya, bukan cuma "identifier
# tidak dikenal".
DELPHI_FILE_API = [
    "FileOpen", "FileOpenExisting", "FileOpenCreate", "FileOpenCreateOrOpen",
    "FileOpenOpenExisting", "FileClose", "FileRead", "FileWrite", "FileSeek",
    "FileEnd", "FileStart", "FileCurrent", "FileShareRead", "FileShareWrite",
    "FileShareReadWrite", "FileShareDelete", "FileShareNone", "THandle",
    "HFILE",
]

# Unit yang tidak pernah ada di distribusi Inno Setup. Percobaan "perbaikan"
# yang menggoda: `uses FileFunc;` -- hanya menghasilkan error lain.
PHANTOM_UNITS = {"filefunc", "sysutils", "classes", "windows"}


def _code_section(iss: str) -> str:
    if "\n[Code]\n" not in iss:
        raise AssertionError("examvan.iss tidak punya [Code]")
    return iss.split("\n[Code]\n", 1)[1]


def _strip_comments(iss: str) -> str:
    """Buang `//`, `;`, `{ }`, dan string literal dalam SATU lintasan.

    Kenapa satu lintasan, bukan regex terpisah: string Inno/Pascal boleh
    memuat `//` maupun `;`. Di examvan.iss ada

        'Perbaiki: buka https://aka.ms/vs/17/release/vc_redist.x64.exe'

    Kalau `//` dihapus lebih dulu, sisa `'Perbaiki: buka https:` membuat
    kutip string tidak tertutup -- dan setelah itu setiap hitungan kurung
    jadi salah, sehingga pemanggilan yang sah ikut dilaporkan bermasalah.

    String literal diganti placeholder `''` (bukan dihapus) supaya jumlah
    argumen tetap benar: `ExpandConstant('{sys}')` tetap satu argumen.
    """
    out = []
    in_str = False
    in_brace = False
    for ch in iss:
        if in_brace:
            if ch == "}":
                in_brace = False
            continue
        if in_str:
            if ch == "\n":
                out.append("\n")
            elif ch == "'":
                in_str = False
            else:
                out.append(_NUL)
            continue
        if ch == "'":
            in_str = True
            out.append(_NUL)
            continue
        if ch == "{":
            in_brace = True
            continue
        out.append(ch)
    text = re.sub(_NUL + "+", "''", "".join(out))
    # Baru sekarang `//` dan `;` boleh dihapus: kita sudah tahu mana yang
    # benar-benar di luar string.
    return "\n".join(
        re.sub(r";.*$", "", re.sub(r"//.*$", "", line))
        for line in text.splitlines()
    )


def _self_declared(iss: str) -> set[str]:
    """Nama fungsi/procedure yang dideklarasikan sendiri di file ini."""
    return set(
        re.findall(
            r"^\s*(?:function|procedure)\s+([A-Za-z_]\w*)", iss, re.MULTILINE
        )
    )


def _receivers(code: str) -> set[str]:
    """Objek yang punya method: `X.Yyy(` -> `Yyy` sah sebagai method call."""
    return set(re.findall(r"\.\s*([A-Za-z_]\w*)\s*\(", code))


def _called(code: str) -> set[str]:
    return set(re.findall(r"(?<![.\w])([A-Za-z_]\w*)\s*\(", code))


def _is_pathish(arg: str) -> bool:
    """Apakah argumen ini aset path.

    Hanya bermakna untuk identifier telanjang. Kalau sudah berupa
    pemanggilan fungsi atau literal, nilainya sudah jelas dan tidak bisa
    tertukar diam-diam.
    """
    if not _BARE_IDENT.match(arg):
        return True
    return bool(_PATH_NAME.search(arg))


def _split_args(args: str) -> list[str]:
    """Pecah argumen pemanggilan pada koma di level terluar."""
    out: list[str] = []
    depth = 0
    current = ""
    for ch in args:
        if ch in "([":
            depth += 1
        elif ch in ")]":
            depth -= 1
        elif ch == "," and depth == 0:
            out.append(current.strip())
            current = ""
            continue
        current += ch
    if current.strip():
        out.append(current.strip())
    return out


def _call_args(code: str, name: str) -> list[list[str]]:
    """Semua daftar argumen untuk pemanggilan `name(...)`."""
    found = []
    for m in re.finditer(rf"(?<![.\w]){re.escape(name)}\s*\(", code):
        depth = 0
        for j in range(m.end() - 1, len(code)):
            ch = code[j]
            if ch == "(":
                depth += 1
            elif ch == ")":
                depth -= 1
                if depth == 0:
                    found.append(_split_args(code[m.end():j]))
                    break
        else:
            raise AssertionError(f"{name}( tidak pernah ditutup")
    return found


def _procedure_body(code: str, name: str) -> str:
    """Body lengkap satu function/procedure, dari deklarasi sampai `end`
    yang mengimbangnya.

    Regex `.*?\\nend;` tidak bisa dipakai: setelah `_strip_comments`, titik
    koma sebagai akhir pernyataan ikut hilang, dan `end` yang terindentasi
    milik blok `if` di dalam akan dipotong lebih dulu -- persis di atas
    baris yang mau diperiksa.
    """
    m = re.search(
        rf"^\s*(?:function|procedure)\s+{re.escape(name)}\b", code, re.MULTILINE
    )
    if not m:
        return ""
    depth = 0
    started = False
    out = []
    for line in code[m.start():].splitlines():
        out.append(line)
        if re.search(r"\bbegin\b", line):
            depth += 1
            started = True
        elif re.search(r"\bend\b", line):
            depth -= 1
            if started and depth == 0:
                break
    return "\n".join(out)


def _strip_comments_keep_strings(iss: str) -> str:
    """Buang komentar, TAPI pertahankan isi string literal.

    Dipakai untuk memeriksa nilai argument -- `ExpandConstant('{sys}\\foo')`
    tidak bisa dinilai kalau string-nya ikut dihapus. Tetap satu lintasan
    supaya `https://` di dalam string tidak merusak hitungan kurung.
    """
    out = []
    in_str = False
    in_brace = False
    for ch in iss:
        if in_brace:
            if ch == "}":
                in_brace = False
            continue
        if in_str:
            out.append(ch)
            if ch == "'":
                in_str = False
            continue
        if ch == "'":
            in_str = True
            out.append(ch)
            continue
        if ch == "{":
            in_brace = True
            continue
        out.append(ch)
    return "\n".join(
        re.sub(r";.*$", "", re.sub(r"//.*$", "", line))
        for line in "".join(out).splitlines()
    )


def _brace(text: str) -> str:
    """Bungkus teks jadi {..} supaya bisa dibandingkan dengan _ENV_CONSTANT."""
    return "{" + text.strip("{}") + "}"


class InstallerPascalIdentifierTest(unittest.TestCase):
    ISS_TEXT = ISS.read_text(encoding="utf-8")
    CODE = _strip_comments(_code_section(ISS_TEXT))
    SELF = _self_declared(ISS_TEXT)
    METHODS = _receivers(CODE)
    CALLED = _called(CODE)

    KNOWN = (
        SUPPORT_FUNCTIONS
        | EVENT_HANDLERS
        | PASCAL_KEYWORDS
        | KNOWN_TYPES
        | KNOWN_CONSTANTS
        | KNOWN_STEP_CONSTANTS
        | SELF
        | METHODS
    )

    def test_the_code_section_was_actually_parsed(self):
        # Kalau parser gagal, semua test lain lolos kosong.
        self.assertGreater(len(self.CALLED), 10, "hanya sedikit identifier")
        self.assertIn("CurStepChanged", self.CALLED)
        self.assertIn("ReadPasswordFromFile", self.SELF)

    def test_every_called_identifier_exists(self):
        unknown = sorted(self.CALLED - self.KNOWN)
        self.assertEqual(
            unknown,
            [],
            f"Identifier tidak dikenal di [Code]: {unknown}\n"
            f"Inno Pascal Script tidak menyediakannya. Cek {REFERENCE_URL}\n"
            f"Catatan: `uses FileFunc;` TIDAK menolong -- unit itu tidak "
            f"ada di Inno Setup. Yang tersedia hanya support function di "
            f"daftar referensi.",
        )

    def test_no_delphi_win32_file_api_survives(self):
        present = sorted(
            name for name in DELPHI_FILE_API
            if re.search(rf"(?<![.\w]){re.escape(name)}\b", self.CODE)
        )
        self.assertEqual(
            present, [],
            f"API Win32 Delphi yang tidak ada di Pascal Script: {present}. "
            f"Pakai LoadStringFromFile / LoadStringsFromFile / FileExists.",
        )

    def test_no_phantom_unit_is_imported(self):
        # WAJIB memakai teks mentah: `_strip_comments` membuang ';' sebagai
        # penanda komentar, jadi `uses FileFunc;` akan hilang sebelum
        # pencarian dan test ini diam-diam tidak memeriksa apa pun.
        uses_block = re.search(
            r"\buses\b(.*?);", _code_section(self.ISS_TEXT),
            re.DOTALL | re.IGNORECASE,
        )
        units = (
            {u.lower() for u in re.findall(r"[A-Za-z_]\w*", uses_block.group(1))}
            if uses_block else set()
        )
        phantom = units & PHANTOM_UNITS
        self.assertEqual(
            phantom, set(),
            f"Unit {phantom} tidak ada di Inno Setup. Hapus baris `uses` "
            f"ini dan pakai support function yang terdaftar di "
            f"{REFERENCE_URL}",
        )

    def test_every_type_used_is_a_known_type(self):
        types = set(re.findall(r"\b(T[A-Z]\w*)\b", self.CODE))
        unknown = sorted(types - KNOWN_TYPES - self.METHODS - self.SELF)
        self.assertEqual(
            unknown, [],
            f"Tipe tidak dikenal: {unknown}. Kalau ini `THandle`, itu API "
            f"Delphi -- Inno tidak menyediakannya.",
        )

    def test_known_types_table_is_not_lying_about_these(self):
        # Sanity: `THandle` sengaja TIDAK ada di KNOWN_TYPES. Kalau suatu
        # hari ada yang menambahkannya "supaya test hijau", test ini
        # menangkapnya.
        self.assertNotIn("THandle", KNOWN_TYPES)
        for name in ("FileOpen", "FileRead", "FileClose", "FileSeek"):
            self.assertNotIn(name, SUPPORT_FUNCTIONS)

    def test_percent_sign_only_appears_inside_string_literals(self):
        # Menangkap kelas bug kedua: satu baris penjelasan kehilangan
        # awalan `//`, sehingga `%USERPROFILE%\.config\examvan yang holding
        # jawaban` duduk di tengah blok `begin`/`end` sebagai sumber
        # Pascal. Di Pascal Script `%` adalah operator modulo, jadi `%` di
        # luar string literal selalu salah.
        raw = _code_section(self.ISS_TEXT)
        offenders = []
        for number, line in enumerate(raw.splitlines(), 1):
            if line.lstrip().startswith(("//", ";")):
                continue
            without = re.sub(r"\{[^}]*\}", "", line)
            without = re.sub(r"'(?:[^']|'')*'", "", without)
            if "%" in without:
                offenders.append((number, line.strip()))
        self.assertEqual(
            offenders, [],
            f"Karakter `%` di luar string literal (kemungkinan baris "
            f"komentar yang kehilangan `//`): {offenders}",
        )

    def test_the_password_reader_uses_a_documented_function(self):
        text = _procedure_body(self.CODE, "ReadPasswordFromFile")
        self.assertNotEqual(text, "", "ReadPasswordFromFile tidak ditemukan")
        self.assertIn("LoadStringFromFile", text)
        self.assertIn("Trim", text)
        # Ketiga kasus di docstring harus tetap ditangani.
        self.assertIn("FileExists", text)
        self.assertIn("not LoadStringFromFile", text)


class SupportFunctionSignatureTest(unittest.TestCase):
    """Urutan argumen, bukan hanya keberadaan identifier."""

    ISS_TEXT = ISS.read_text(encoding="utf-8")
    CODE = _strip_comments(_code_section(ISS_TEXT))

    def test_arity_matches_the_documented_signature(self):
        wrong = []
        for name, params in SIGNATURES.items():
            for args in _call_args(self.CODE, name):
                if len(args) != len(params):
                    wrong.append((name, len(args), len(params)))
        self.assertEqual(
            wrong, [],
            f"Jumlah argumen tidak cocok dengan signature resmi: {wrong}. "
            f"Lihat {REFERENCE_URL}",
        )

    def test_single_path_argument_is_really_a_path(self):
        # FileExists(PwValue) compiles, dan kompilasi tidak bisa menolong:
        # dua-duanya identifier String. Yang membedakan hanya namanya.
        offenders = []
        for name, params in SIGNATURES.items():
            if len(params) != 1 or params[0] not in _PATH_FIRST:
                continue
            for args in _call_args(self.CODE, name):
                if len(args) == 1 and not _is_pathish(args[0]):
                    offenders.append((name, args[0]))
        self.assertEqual(
            offenders, [],
            f"Argumen ini classifier sebagai bukan variabel path: {offenders}",
        )

    def test_positional_constant_parameters_keep_their_kind(self):
        offenders = []
        for name, kinds in PARAM_KINDS.items():
            for args in _call_args(self.CODE, name):
                if len(args) != len(kinds):
                    continue
                for got, kind in zip(args, kinds):
                    if kind in _FREE_KINDS:
                        continue
                    if not _KIND_RE[kind].match(got):
                        offenders.append((name, kind, got))
        self.assertEqual(
            offenders, [],
            f"Parameter konstanta tidak sesuai posisinya: {offenders}",
        )

    def test_filename_argument_comes_first(self):
        # Parameter pertama yang namanya 'filename'/'path'/'dir'/'name'
        # harus menerima identifier yang benar-benar path. Konten password
        # (nilai bebas) tidak boleh kebetulan bernama seperti file, dan
        # kalau sampai tertukar keduanya tetap String sehingga ISCC tidak
        # keberatan.
        offenders = []
        for name, params in SIGNATURES.items():
            if not params or params[0] not in _PATH_FIRST:
                continue
            for args in _call_args(self.CODE, name):
                if len(args) != len(params):
                    continue
                first = args[0]
                if not _BARE_IDENT.match(first):
                    continue
                if not _is_pathish(first):
                    offenders.append((name, first, args[1:2]))
                    continue
                if len(args) > 1 and _BARE_IDENT.match(args[1]):
                    if _is_pathish(args[1]):
                        offenders.append((name, first, args[1]))
        self.assertEqual(
            offenders, [],
            f"Argumen path dan konten tertukar: {offenders}. "
            f"Signature resmi lihat {REFERENCE_URL}",
        )

    def test_password_write_uses_the_documented_order(self):
        text = _procedure_body(self.CODE, "CurStepChanged")
        self.assertNotEqual(text, "", "CurStepChanged tidak ditemukan")
        self.assertIn("SaveStringToFile(PwFile, PwValue, False)", text)
        self.assertNotIn("SaveStringToFile(PwValue, PwFile", text)

    def test_password_write_checks_its_result(self):
        # Menulis tanpa memeriksa hasil = kehilangan data senyap dengan
        # exit 0. Kalau writer gagal, instalasi HARUS gagal.
        text = _procedure_body(self.CODE, "CurStepChanged")
        self.assertIn("if not SaveStringToFile(", text)
        self.assertIn("RaiseException(", text)


class PasswordRoundTripTest(unittest.TestCase):
    """Simulasi algoritma CurStepChanged untuk semua kombinasi isian.

    Ini tidak menjalankan Pascal -- tujuannya membuktikan bahwa logika yang
    ditulis di .iss menghasilkan keputusan yang benar, sehingga test
    struktural di atas punya sesuatu untuk diverifikasi.

    Semantik yang disimulasikan (lihat .iss):
      - Halaman hanya tampil kalau bukan WizardSilent. Kalau senyap,
        Values[] kosong BUKAN pilihan pengguna.
      - Password lama dimuat hanya untuk instalasi senyap.
      - Dua kolom tidak sama -> password LAMA dikembalikan, bukan string
        kosong (string kosong akan jatuh ke cabang hapus).
      - Terisi -> tulis. Kosong + interaktif -> hapus.
    """

    @staticmethod
    def _run(values0: str, values1: str, silent: bool, existing: str | None):
        stored = existing

        def read() -> str:
            return existing.strip() if existing is not None else ""

        # muat password lama HANYA kalau halamannya tidak pernah tampil
        if silent and values0 == "" and existing is not None:
            values0 = read()

        pw_value = values0
        pw_repeat = values1
        if pw_repeat == "":
            pw_repeat = pw_value
        if pw_value != "" and pw_value != pw_repeat:
            pw_value = read()          # ditolak -> password lama, bukan ""

        if pw_value != "":
            stored = pw_value
        elif not silent and stored is not None:
            stored = None               # DeleteFile
        return stored

    def test_silent_upgrade_preserves_the_password(self):
        # Skenario persis yang gagal di CI: install 1, tulis password,
        # lalu install 2 senyap di atasnya.
        self.assertEqual(self._run("", "", True, "rahasia-smoke-123"),
                         "rahasia-smoke-123")

    def test_silent_install_without_existing_password_stays_empty(self):
        self.assertIsNone(self._run("", "", True, None))

    def test_silent_install_never_loses_an_existing_password(self):
        # R1 untuk semua kombinasi isian. Password yang sudah ada hanya
        # boleh hilang kalau Values[0] kosong -- kalau tidak, yang ditulis
        # adalah nilai itu sendiri (dan pada instalasi senyap_values itu
        # memang tidak pernah terisi karena halamannya tidak tampil).
        for values0 in ("", "a", "b"):
            for values1 in ("", "a", "b"):
                with self.subTest(v=values0, r=values1):
                    got = self._run(values0, values1, True, "lama-123")
                    # Nilai baru hanya tersimpan kalau ada isian yang
                    # benar-benar terisi DAN kedua kolom cocok. Selain itu
                    # yang berlaku password lama.
                    accepted = values0 != "" and (
                        values1 == "" or values1 == values0
                    )
                    self.assertEqual(
                        got, values0 if accepted else "lama-123",
                    )
                    self.assertIsNotNone(got)

    def test_interactive_upgrade_with_empty_field_deletes(self):
        # Supervisor yang sengaja mengosongkan kolom harus benar-benar
        # menonaktifkan password exit -- kalau tidak, satu-satunya cara
        # "mematikan" password adalah reinstall lalu mengetik yang lupa.
        self.assertIsNone(self._run("", "", False, "lama-123"))

    def test_interactive_upgrade_keeping_the_field_rewrites_same_value(self):
        self.assertEqual(
            self._run("lama-123", "lama-123", False, "lama-123"), "lama-123"
        )

    def test_interactive_mismatch_keeps_the_old_password(self):
        # Salah ketik tidak boleh menghapus password yang masih working.
        # Nilai yang tersimpan adalah Values[0], jadi yang dikembalikan
        # harus isi lama.
        self.assertEqual(
            self._run("baru-999", "lama-123", False, "lama-123"), "lama-123"
        )

    def test_interactive_never_empties_unless_the_user_cleared_it(self):
        for values0 in ("a", "b"):
            for values1 in ("a", "b"):
                with self.subTest(v=values0, r=values1):
                    self.assertIsNotNone(
                        self._run(values0, values1, False, "lama-123")
                    )

    def test_mismatch_with_no_previous_password_writes_nothing(self):
        # Tidak ada password lama untuk dikembalikan; yang penting tidak
        # ada file yang salah tulis.
        self.assertIsNone(self._run("baru-999", "lama-123", False, None))


class GuardTest(unittest.TestCase):
    """Guard struktural untuk tiga keputusan yang mudah dibalik diam-diam."""

    ISS_TEXT = ISS.read_text(encoding="utf-8")
    CODE = _strip_comments(_code_section(ISS_TEXT))

    def test_silent_install_never_deletes_the_password(self):
        # R1. Cabang "hapus kalau dikosongkan" WAJIB dijaga WizardSilent.
        # Tanpa itu, /VERYSILENT selalu punya Values[0] kosong dan setiap
        # upgrade senyap menghapus password seluruh lab.
        text = _procedure_body(self.CODE, "CurStepChanged")
        self.assertIn("WizardSilent", SUPPORT_FUNCTIONS)
        idx_guard = text.index("else if not WizardSilent then")
        idx_delete = text.index("DeleteFile(PwFile)", idx_guard)
        self.assertLess(
            idx_guard, idx_delete,
            "penghapusan password harus berada DI DALAM cabang WizardSilent",
        )

    def test_existing_password_is_prefilled_only_when_silent(self):
        # Kalau pemuatan terjadi juga saat halaman tampil, Values[0] yang
        # baru saja diketik pengguna ditimpa password lama -- dan
        # mengosongkan kolom tidak akan pernah berhasil.
        text = _procedure_body(self.CODE, "CurStepChanged")
        idx_silent = text.index("if WizardSilent then")
        idx_prefill = text.index("ReadPasswordFromFile(PwFile)")
        self.assertLess(
            idx_silent, idx_prefill,
            "password lama hanya boleh dimuat untuk instalasi SENYAP",
        )

    def test_mismatch_keeps_the_old_password_instead_of_blanking(self):
        # `PwValue := ''` di cabang mismatch akan jatuh ke cabang hapus dan
        # MENGHAPUS password yang masih working -- kebalikan dari niatnya.
        text = _procedure_body(self.CODE, "CurStepChanged")
        # cabang mismatch berada di antara pengecekan kolom dan cabang tulis
        start = text.index("if (PwValue <> '')")
        end = text.index("if PwValue <> ''", start)
        mismatch = text[start:end]
        self.assertIn("ReadPasswordFromFile(PwFile)", mismatch)
        # tidak boleh ada reset ke string kosong di seluruh body: itu yang
        # membuat salah ketik jatuh ke cabang hapus
        self.assertNotIn("PwValue := ''", text)

    def test_signature_tables_are_not_empty(self):
        # Kalau tabel ini dikosongkan, test di atas jadi tidak berguna.
        self.assertIn("SaveStringToFile", SIGNATURES)
        self.assertEqual(SIGNATURES["SaveStringToFile"][0], "filename")
        self.assertEqual(SIGNATURES["SaveStringToFile"][1], "content")
        self.assertEqual(SIGNATURES["LoadStringFromFile"][0], "filename")
        self.assertIn("MsgBox", PARAM_KINDS)
        self.assertTrue(DELPHI_FILE_API)
        self.assertIn("filefunc", PHANTOM_UNITS)


if __name__ == "__main__":
    unittest.main()


# --- (4) Constant Inno Setup, dari halaman Constants resmi.
# https://jrsoftware.org/ishelp/topic_consts.htm
#
# Constant ini dievaluasi INSTALL/UNINSTALL, bukan compiler -- jadi constant
# yang salah di dalam string Pascal TIDAK menggagalkan build. Baru meledak
# saat runtime, di jalur yang tidak selalu dijalankan.
INNO_CONSTANTS = set("""
app win sys sysnative syswow64 src sd commonpf commonpf32 commonpf64
commoncf commoncf32 commoncf64 tmp commonfonts dao dotnet11 dotnet20
dotnet2032 dotnet2064 dotnet40 dotnet4032 dotnet4064
group localappdata userappdata commonappdata usercf userdesktop
commondesktop userdocs commondocs userfavorites userfonts userpf
userprograms commonprograms usersavedgames usersendto userstartmenu
commonstartmenu userstartup commonstartup usertemplates commontemplates
autoappdata autocf autocf32 autocf64 autodesktop autodocs autofonts
autopf autopf32 autopf64 autoprograms autostartmenu autostartup
autotemplates
cf cf32 cf64 fonts pf pf32 pf64 sendto
cmd computername groupname hwnd wizardhwnd srcexe uninstallexe
sysuserinfoname sysuserinfoorg userinfoname userinfoorg userinfoserial
username log language
""".split())

# Constant berparameter: isinya bukan nama constant, tapi prefiks + argumen.
INNO_PARAMETRIC_PREFIXES = ("ini:", "cm:", "reg:", "param:", "drive:", "code:")

# `{%NAME|Default}` = environment variable. Bentuk inilah yang benar untuk
# USERPROFILE -- bukan `{userprofile}`.
_ENV_CONSTANT = re.compile(r"\{%[A-Za-z_][A-Za-z0-9_]*(?:\|[^}]*)?\}")

# Constant yang penyebutnya mirip constant tapi TIDAK ada di Inno Setup.
# Dipisah supaya pesan kegagalan menyebut替代 yang benar.
NOT_INNO_CONSTANTS = {
    "userprofile": "{%USERPROFILE} (environment variable)",
    "userhome": "{%USERPROFILE} (environment variable)",
    "home": "{%USERPROFILE} (environment variable)",
    "appdata": "{userappdata} atau {localappdata}",
    "pf": "{commonpf} (nama lama 'pf' deprecated)",
    "cf": "{commoncf} (nama lama 'cf' deprecated)",
    "fonts": "{commonfonts} (nama lama 'fonts' deprecated)",
}


def _constants_in(text: str) -> list[str]:
    return re.findall(r"\{([^{}]*)\}", text)


class InnoConstantTest(unittest.TestCase):
    r"""Setiap constant di ExpandConstant harus benar-benar ADA.

    Bug yang menutup kelas ini: `ExpandConstant('{userprofile}\.config
    \examvan')`. `{userprofile}` bukan constant Inno Setup -- yang ada
    `{userappdata}`, `{userdocs}`, `{userdesktop}`, dan seterusnya, tapi
    tidak ada constant untuk user profile. Environment variable ditulis
    dengan bentuk `{%NAME}`. Hasilnya runtime error:

        Cannot find 'userprofile'

    Yang membuat bug ini lolos: user asli yang menemukannya, saat uninstall interaktif.
    """

    ISS_TEXT = ISS.read_text(encoding="utf-8")
    # String literal harus UTUH: nama constant justru ADA di dalam string.
    CODE = _strip_comments_keep_strings(_code_section(ISS_TEXT))

    def _expand_constant_args(self) -> list[str]:
        args = []
        for a in _call_args(self.CODE, "ExpandConstant"):
            self.assertEqual(len(a), 1, f"ExpandConstant perlu 1 argumen: {a}")
            args.append(a[0].strip("'"))
        return args

    def test_every_expand_constant_name_exists(self):
        bad = []
        for arg in self._expand_constant_args():
            if _ENV_CONSTANT.fullmatch(_brace(arg)) or _ENV_CONSTANT.search(arg):
                continue
            for raw in _constants_in(arg):
                if raw in INNO_CONSTANTS:
                    continue
                if raw.startswith(INNO_PARAMETRIC_PREFIXES):
                    continue
                if raw == "\\":          # {\} = backslash
                    continue
                bad.append(raw)
        self.assertEqual(
            bad, [],
            f"Constant yang tidak ada di Inno Setup: {bad}. "
            f"Lihat https://jrsoftware.org/ishelp/topic_consts.htm",
        )

    def test_user_profile_uses_the_environment_variable_form(self):
        # {userprofile} tidak pernah ada. Yang benar {%USERPROFILE}.
        for arg in self._expand_constant_args():
            for raw in _constants_in(arg):
                self.assertNotIn(
                    raw.lower(), NOT_INNO_CONSTANTS,
                    f"{{{raw}}} bukan constant Inno Setup. Untuk yang itu "
                    f"pakai {NOT_INNO_CONSTANTS.get(raw.lower(), '?')}",
                )

    def test_the_data_folder_matches_what_the_app_actually_uses(self):
        # config.py: `Path.home() / ".config" / "examvan"`. Di Windows
        # Path.home() == %USERPROFILE%. Kalau installer dan app tidak
        # sengaja, uninstall tidak akan pernah menemukan folder jawaban.
        args = [a.lower() for a in self._expand_constant_args()]
        data = [a for a in args if ".config" in a]
        self.assertEqual(
            len(data), 1,
            f"harus ada tepat satu path data, dapat {data}",
        )
        self.assertIn("{%userprofile}", data[0])
        self.assertIn(r".config\examvan", data[0])

    def test_expand_constant_is_not_hidden_behind_the_silent_guard(self):
        # Smoke test CI meng-uninstall dengan /VERYSILENT. Kalau semua
        # ExpandConstant berada di dalam `and (not UninstallSilent)`,
        # jalur itu tidak pernah dievaluasi di CI dan constant salah
        # lolos ke rilis tanpa pernah meledak.
        text = _procedure_body(self.CODE, "CurUninstallStepChanged")
        guard = text.index("not UninstallSilent")
        for arg in self._expand_constant_args():
            needle = "ExpandConstant("
            idx = 0
            while True:
                idx = text.find(needle, idx)
                if idx == -1:
                    break
                self.assertLess(
                    idx, guard,
                    f"ExpandConstant('{arg}') berada SETELAH "
                    f"`not UninstallSilent`, jadi tidak pernah dievaluasi "
                    f"oleh uninstall senyap di CI",
                )
                idx += 1

    def test_inno_constants_table_is_not_empty(self):
        self.assertIn("localappdata", INNO_CONSTANTS)
        self.assertIn("userappdata", INNO_CONSTANTS)
        self.assertIn("app", INNO_CONSTANTS)
        # {userprofile} sengaja tidak boleh masuk tabel ini.
        self.assertNotIn("userprofile", INNO_CONSTANTS)
        self.assertIn("userprofile", NOT_INNO_CONSTANTS)
