"""R6: setiap identifier Pascal Script di [Code] harus benar-benar ADA.

Bug yang menutup kelas ini
-------------------------
`ReadPasswordFromFile` memakai `FileOpen`, `FileSeek`, `FileRead`,
`FileClose`, `FileEnd`, `FileOpenExisting`, `FileShareReadWrite`,
`FileShareDelete`, dan `THandle`. Semuanya API Win32 Delphi, bukan
Pascal Script Inno Setup — dan tidak ada unit `FileFunc` yang bisa
mengimpornya. Build Windows di CI berhenti dengan:

    Error on line 261 in ...examvan.iss: Column 13:
    Unknown identifier 'FileOpen'

Yang membuat ini lolos dari review lokal: tidak ada mesin Linux yang
bisa menjalankan ISCC, dan test yang ada hanya membaca `.iss` sebagai
TEKS. Regex seperti `assertIn("FileExists", code)` tidak bisa tahu
bahwa `FileOpen` tidak punya unit yang mengimpornya -- keduanya cuma
huruf.

Trik: `uses FileFunc;` TIDAK akan memperbaiki ini. Unit `FileFunc` tidak
ada di distribusi Inno Setup sama sekali. Satu-satunya jalan adalah pakai
support function yang benar-benar terdaftar.

Test ini karena itu memeriksa ATURAN, bukan teks: setiap identifier yang
dipanggil di [Code] harus salah satu dari
  1. fungsi/procedure yang dideklarasikan sendiri di file itu,
  2. event handler Inno yang sah,
  3. keyword Pascal,
  4. support function dari daftar resmi Support Functions Reference,
  5. method milik objek yang sudah dipakai (mis. `Page.Add`).

Kalau (4) gagal, test menyebut identifier yang tidak dikenal dan memberi
URL rujukan, sehingga perbaikan berikutnya tidak perlu mengulang riset dari
nol.

Referensi: https://jrsoftware.org/ishelp/topic_scriptfunctions.htm
"""

from __future__ import annotations

import re
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
ISS = REPO / "windows/installer/examvan.iss"
REFERENCE_URL = "https://jrsoftware.org/ishelp/topic_scriptfunctions.htm"

# --- (4) Support Functions Reference, diambil verbatim dari halaman resmi.
# Diedarkan per kategori supaya saat Inno menambah fungsi baru, titik
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
    "CurPageCreate", "CurPageDestroy", "InputQueryWizardPageAdd",
    "InputQueryWizardPageRemove", "InputDirWizardPageAdd",
    "InputDirWizardPageRemove", "InputOptionWizardPageAdd",
    "InputOptionWizardPageRemove", "InputFileWizardPageAdd",
    "OutputMsgWizardPageAdd", "OutputMsgWizardPageRemove",
    "PopupMsg", "SequenceBreak", "LanguageChange", "CodeRunnerError",
}

# --- Keyword Pascal + tipe bawaan yang boleh muncul.
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
    "TOnExtractionProgress", "TInstalledWithProgressBar", "TObject",
    "TComponent", "TClass", "TMethod", "AnyMethod", "AnyString", "Variant",
    "TGUID", "HKEY", "HMODULE", "HANDLE_", "HWND", "HRESULT", "LPARAM",
    "WPARAM", "LRESULT", "Cardinal_", "Double_", "Boolean_", "Variant_",
    "None", "OleVariant", "IDispatch", "IUnknown", "TDateTime",
}

# --- Konstanta yang sah (MsgBox buttons/return, TMsgBoxType, wizard page).
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
    "wpWelcome", "wpSelectDir", "wpSelectGroup", "wpSelectComponents",
    "wpSelectTasks", "wpSelectProgramGroup", "wpReady", "wpInstalling",
    "wpFinished", "wpInfoBefore", "wpInfoAfter",
}


def _code_section(iss: str) -> str:
    if "\n[Code]\n" not in iss:
        raise AssertionError("examvan.iss tidak punya [Code]")
    return iss.split("\n[Code]\n", 1)[1]


def _strip_comments(iss: str) -> str:
    """Buang `//`, `;`, `{ }`, dan string literal.

    Komentar dipakai untuk menjelaskan keputusan, jadi isinya penuh nama
    fungsi -- kalau tidak dibuang, `FileOpen` di dalam penjelasan akan
    terbaca sebagai pemanggilan. String literal ('%LOCALAPPDATA%', #13#10)
    juga bukan kode.
    """
    out = []
    for line in iss.splitlines():
        line = re.sub(r"//.*$", "", line)
        line = re.sub(r"\s;.*$", "", line)
        line = re.sub(r"\{[^}]*\}", "", line)
        line = re.sub(r"'(?:[^']|'')*'", "''", line)
        line = re.sub(r'#\d+', "", line)
        out.append(line)
    return "\n".join(out)


def _self_declared(iss: str) -> set[str]:
    """Nama fungsi/procedure yang dideklarasikan sendiri di file ini."""
    return set(
        re.findall(
            r"^\s*(?:function|procedure)\s+([A-Za-z_]\w*)",
            iss,
            re.MULTILINE,
        )
    )


def _receivers(code: str) -> set[str]:
    """Objek yang punya method: `X.Yyy(` -> `Yyy` sah sebagai method call."""
    return set(re.findall(r"\.\s*([A-Za-z_]\w*)\s*\(", code))


def _called(code: str) -> set[str]:
    return set(re.findall(r"(?<![.\w])([A-Za-z_]\w*)\s*\(", code))


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
            f"Inno Pascal Script tidak menyediakannya. Cek "
            f"{REFERENCE_URL}\n"
            f"Catatan: `uses FileFunc;` TIDAK menolong — unit itu tidak "
            f"ada di Inno Setup. Yang tersedia hanya support function di "
            f"daftar referensi.",
        )

    def test_no_delphi_win32_file_api_survives(self):
        # Guards spesifik untuk kelas bug yang memblokir build ini.
        # Helper ini ada supaya pesan kegagalan menyebut API-nya, bukan
        # cuma "identifier tidak dikenal".
        forbidden = [
            "FileOpen", "FileOpenExisting", "FileOpenCreate",
            "FileOpenCreateOrOpen", "FileOpenOpenExisting", "FileClose",
            "FileRead", "FileWrite", "FileSeek", "FileEnd", "FileStart",
            "FileCurrent", "FileShareRead", "FileShareWrite",
            "FileShareReadWrite", "FileShareDelete", "FileShareNone",
            "THandle", "HFILE",
        ]
        present = sorted(
            name for name in forbidden
            if re.search(rf"(?<![.\w]){re.escape(name)}\b", self.CODE)
        )
        self.assertEqual(
            present, [],
            f"API Win32 Delphi yang tidak ada di Pascal Script: {present}. "
            f"Pakai LoadStringFromFile / LoadStringsFromFile / FileExists.",
        )

    def test_no_phantom_filefunc_unit_is_imported(self):
        # Percobaan "perbaikan" yang menggoda: menambahkan
        # `uses FileFunc;`. Unit itu tidak ada, jadi ISCC akan gagal
        # dengan error lain yang lebih membingungkan.
        uses_block = re.search(
            r"\buses\b(.*?);", self.CODE, re.DOTALL | re.IGNORECASE
        )
        units = (
            set(re.findall(r"[A-Za-z_]\w*", uses_block.group(1)))
            if uses_block else set()
        )
        phantom = {
            u for u in units
            if u.lower() in {"filefunc", "sysutils", "classes", "windows"}
        }
        self.assertEqual(
            phantom, set(),
            f"Unit {phantom} tidak ada di Inno Setup. Hapus baris `uses` "
            f"ini dan pakai support function.",
        )

    def test_every_type_used_is_a_known_type(self):
        types = set(re.findall(r"\b(T[A-Z]\w*)\b", self.CODE))
        unknown = sorted(types - KNOWN_TYPES - self.METHODS - self.SELF)
        self.assertEqual(
            unknown, [],
            f"Tipe tidak dikenal: {unknown}. Kalau ini `THandle`, itu API "
            f"Delphi — Inno tidak menyediakannya.",
        )

    def test_known_types_table_is_not_lying_about_these(self):
        # Sanity: `THandle` sengaja TIDAK ada di KNOWN_TYPES. Kalau suatu
        # hari ada yang menambahkannya "supaya test hijau", test ini
        # menangkapnya.
        self.assertNotIn("THandle", KNOWN_TYPES)
        self.assertNotIn("FileOpen", SUPPORT_FUNCTIONS)
        self.assertNotIn("FileRead", SUPPORT_FUNCTIONS)
        self.assertNotIn("FileClose", SUPPORT_FUNCTIONS)
        self.assertNotIn("FileSeek", SUPPORT_FUNCTIONS)

    def test_percent_sign_only_appears_inside_string_literals(self):
        # Menangkap bug kedua di file yang sama: satu baris penjelasan
        # kehilangan awalan `//`, sehingga `%USERPROFILE%\.config\examvan
        # yang holding jawaban` duduk di tengah blok `begin`/`end` sebagai
        # sumber Pascal. ISCC baru akan menj complains setelah error
        # `FileOpen` diperbaiki, karena kompilasi berhenti di error
        # pertama. Di Pascal Script `%` adalah operator modulo, jadi `%`
        # di luar string literal selalu salah.
        raw = _code_section(self.ISS_TEXT)
        offenders = []
        for number, line in enumerate(raw.splitlines(), 1):
            if line.lstrip().startswith(("//", ";")):
                continue
            without_literals = re.sub(r"'(?:[^']|'')*'", "", line)
            without_literals = re.sub(r"\{[^}]*\}", "", without_literals)
            if "%" in without_literals:
                offenders.append((number, line.strip()))
        self.assertEqual(
            offenders, [],
            f"Karakter `%` di luar string literal (kemungkinan baris "
            f"komentar yang kehilangan `//`): {offenders}",
        )

    def test_the_password_reader_uses_a_documented_function(self):
        # Kontrak langsung untuk bug yang memblokir build.
        body = re.search(
            r"function\s+ReadPasswordFromFile\b.*?\bend;", self.CODE,
            re.DOTALL,
        )
        self.assertIsNotNone(body, "ReadPasswordFromFile tidak ditemukan")
        text = body.group(0)
        self.assertIn("LoadStringFromFile", text)
        self.assertIn("Trim", text)
        # Ketiga kasus di docstring harus tetap ditangani.
        self.assertIn("FileExists", text)
        self.assertIn("not LoadStringFromFile", text)


if __name__ == "__main__":
    unittest.main()
