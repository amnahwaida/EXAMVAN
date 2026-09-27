#!/usr/bin/env bash
# =============================================================================
# Build EXAMVAN .deb
#
# PRINSIP: desktop/examvan/ adalah SATU-SATUNYA sumber kebenaran. Isi paket
# selalu di-stage ulang dari sana setiap build.
#
# Kenapa tidak lagi mengemas pkg-build/ apa adanya:
#   pkg-build/opt/examvan/examvan/ pernah ikut di-commit sebagai salinan kode
#   aplikasi. Salinan itu tidak pernah di-sync, jadi .deb yang terbit
#   pre-WebSocket, pre-presence, pre-approval-gate, dan kehilangan
#   notify.py / ws.py /
#   security/base.py / linux_backend.py / waiting_approval.py, dan melaporkan
#   APP_VERSION 2.2.0 padahal source sudah 2.5.0. Diff-nyarrrr terlihat
#   sekarang karena build ini selalu menyalin source, bukan salinan.
#
# Yang TETAP di-commit di pkg-build/: template paket (DEBIAN/*, usr/*).
#.opt/examvan/ TIDAK di-commit — dibuat ulang di sini tiap build.
#
# Usage:
#   ./build-deb.sh              build
#   ./build-deb.sh --clean      hapus .deb lama lebih dulu
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TEMPLATE_DIR="$SCRIPT_DIR/pkg-build"
OUTPUT_DIR="$SCRIPT_DIR/dist"
SOURCE_PKG="$SCRIPT_DIR/examvan"

info()  { printf '\033[1;34m[INFO]\033[0m  %s\n' "$*"; }
ok()    { printf '\033[1;32m[OK]\033[0m    %s\n' "$*"; }
error() { printf '\033[1;31m[ERR]\033[0m   %s\n' "$*" >&2; }
die()   { error "$@"; exit 1; }

CLEAN=0
[ "${1:-}" = "--clean" ] && CLEAN=1

# ---- Preflight ---------------------------------------------------------------

command -v dpkg-deb &>/dev/null || die "dpkg-deb tidak ditemukan. Install: sudo apt install dpkg-dev"
command -v python3  &>/dev/null || die "python3 tidak ditemukan."
[ -d "$SOURCE_PKG" ] || die "Source package tidak ditemukan: $SOURCE_PKG"
[ -d "$TEMPLATE_DIR/DEBIAN" ] || die "Template paket tidak ditemukan: $TEMPLATE_DIR/DEBIAN"
[ -f "$SCRIPT_DIR/requirements.txt" ] || die "requirements.txt tidak ditemukan."

# ---- Versi: satu sumber, sama dengan installer Windows -----------------------
# windows/installer/build_info.py sudah jadi acuan APP_VERSION (bukan git tag)
# supaya .deb dan EXAMVAN-Setup.exe tidak pernah berbeda label. Dipakai ulang,
# bukan dihitung ulang di sini.
BUILD_INFO="$REPO_ROOT/windows/installer/build_info.py"
if [ -f "$BUILD_INFO" ]; then
    DEFINES="$(python3 "$BUILD_INFO")"
else
    die "windows/installer/build_info.py tidak ditemukan — itu acuan versi."
fi
get_define() { # get_define AppVersion
    local key="/D$1="
    for d in $DEFINES; do
        case "$d" in
            "$key"*) printf '%s' "${d#"$key"}"; return 0 ;;
        esac
    done
    return 1
}
VERSION="$(get_define AppVersion)" || die "AppVersion tidak ada di output build_info.py: $DEFINES"
BUILD_NUM="$(get_define AppBuild)"    || BUILD_NUM="0"
COMMIT_SHA="$(get_define AppCommit)" || COMMIT_SHA="local"

# Debian versi 2.0 format: digit dengan titik, tanpa 'v'. build_info.py
# sudah menjamin numerik, tapi kita cek lagi di sini — yang dirsakit kalau
# bocor ke sini adalah dpkg-deb, bukan cuma tampilan.
echo "$VERSION" | grep -Eq '^[0-9]+(\.[0-9]+)*$' \
    || die "Versi tidak valid untuk Debian: '$VERSION' (dari build_info.py)"

mkdir -p "$OUTPUT_DIR"
[ "$CLEAN" -eq 1 ] && rm -f "$OUTPUT_DIR"/examvan-linux_*.deb

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
info "Staging di $STAGE"

# ---- Susun paket -------------------------------------------------------------

# Template (DEBIAN/*, usr/*) disalin apa adanya.
cp -a "$TEMPLATE_DIR/DEBIAN" "$STAGE/"
cp -a "$TEMPLATE_DIR/usr"     "$STAGE/"

# Payload aplikasi — dari source, BUKAN dari salinan.
mkdir -p "$STAGE/opt/examvan"
cp -a "$SOURCE_PKG"                  "$STAGE/opt/examvan/examvan"
cp -a "$SCRIPT_DIR/requirements.txt" "$STAGE/opt/examvan/"
cp -a "$SCRIPT_DIR/run.sh"           "$STAGE/opt/examvan/"

# Bytecode TIDAK boleh ikut paket: .pyc bawaan mesin build bisa lebih lama
# dari .py-nya, dan Python lebih suka memakainya daripada recompile.
find "$STAGE/opt/examvan" -name '__pycache__' -type d -prune -exec rm -rf {} + 2>/dev/null || true
find "$STAGE/opt/examvan" -name '*.pyc' -delete 2>/dev/null || true

# ---- Guard: paket harus berisi persis source -------------------------------
# Ini yang menangkap kelas bug "modul hilang dari paket". Kalau suatu modul
# ada di source tapi tidak di stage, build DIHENTIKAN, bukan diteruskan
# dengan .deb yang lebih sunyi dari biasanya.
info "Verifikasi stage vs source ..."
DIFF_OUT="$(diff -rq --exclude='__pycache__' --exclude='*.pyc' \
    "$SOURCE_PKG" "$STAGE/opt/examvan/examvan" 2>&1 || true)"
if [ -n "$DIFF_OUT" ]; then
    error "Stage tidak identik dengan source:"
    printf '%s\n' "$DIFF_OUT" >&2
    die "Build dibatalkan."
fi

# Modul yang WAJIB ada — kalau hilang, .deb tidak akan bisa start.
for required in \
    examvan/__main__.py \
    examvan/api.py \
    examvan/config.py \
    examvan/notify.py \
    examvan/ws.py \
    examvan/security/__init__.py \
    examvan/security/base.py \
    examvan/security/enforcer.py \
    examvan/security/linux_backend.py \
    examvan/security/windows_backend.py \
    examvan/security/kiosk.py \
    examvan/security/x11.py \
    examvan/ui/exam_viewer.py \
    examvan/ui/server_config.py \
    examvan/ui/waiting_approval.py
do
    [ -f "$STAGE/opt/examvan/$required" ] || die "Modul wajib hilang dari paket: $required"
done
ok "Stage identik dengan source, semua modul wajib ada."

# ---- Sanity: bytecode harus bisa dikompilasi --------------------------------
# Menangkap file .py yang terpotong / rusak di tengah copy.
python3 -m compileall -q "$STAGE/opt/examvan/examvan" >/dev/null \
    || die "compileall gagal pada source yang di-stage."
find "$STAGE/opt/examvan" -name '__pycache__' -type d -prune -exec rm -rf {} + 2>/dev/null || true
find "$STAGE/opt/examvan" -name '*.pyc' -delete 2>/dev/null || true
ok "Syntax source OK."

# ---- DEBIAN/control: versi --------------------------------------------------
# Dipatch dari template, bukan ditulis ulang, supaya deskripsi & dependency
# tetap di-review di git.
control_file="$STAGE/DEBIAN/control"
[ -f "$control_file" ] || die "Template DEBIAN/control tidak ditemukan."
sed -i -E "s|^Version:.*|Version: $VERSION|" "$control_file"
grep -qE "^Version: $VERSION\$" "$control_file" || die "Gagal menulis Version ke DEBIAN/control."
ok "Version: $VERSION (build $BUILD_NUM, commit $COMMIT_SHA)"

# ---- DEBIAN: izin eksekusi --------------------------------------------------
# Tanpa ini dpkg-deb tidak menandai skrip executable dan postinst gagal diam-diam.
# Ketiganya WAJIB ada: postinst membangun venv, prerm menghentikan app + memulihkan
# setting GNOME, postrm membuang venv yang tidak dimiliki dpkg (kembalikannya ~200 MB
# dan membuat dpkg -r lalu dpkg -i diam-diam memakai kode versi lama). Melewatkan
# salah satunya berarti uninstall rusak lagi — persis bug yang sedang diperbaiki.
for script in postinst prerm postrm; do
    [ -f "$STAGE/DEBIAN/$script" ] || die "DEBIAN/$script hilang — uninstall akan rusak."
    chmod 755 "$STAGE/DEBIAN/$script"
done
chmod 644 "$STAGE/DEBIAN/control"

chmod 755 "$STAGE/usr/bin/examvan"
chmod 755 "$STAGE/opt/examvan/run.sh"
chmod 644 "$STAGE/usr/share/applications/examvan.desktop"
chmod 644 "$STAGE/usr/share/xsessions/examvan-kiosk.desktop"

# md5sum wajib ada untuk paket yang Qualifies untuk Things
( cd "$STAGE" && md5sum $(find DEBIAN -type f) > DEBIAN/md5sums )
chmod 644 "$STAGE/DEBIAN/md5sums"

# ---- Build -------------------------------------------------------------------

DEB_PATH="$OUTPUT_DIR/examvan-linux_${VERSION}.deb"
info "Build .deb (dpkg-deb --build) ..."
rm -f "$DEB_PATH"
dpkg-deb --build "$STAGE" "$DEB_PATH" >/dev/null

[ -f "$DEB_PATH" ] || die "dpkg-deb selesai tapi .deb tidak ditemukan: $DEB_PATH"

# ---- Verifikasi hasil -------------------------------------------------------
# Cek isi .deb yang BENAR-BENAR terbit, bukan isi staging. Tanpa ini, guard di
# atas hanya membuktikan stage benar — bukan bahwa dpkg-deb mengemasnya.
info "Verifikasi isi .deb ..."
# Listing diambil SEKALI ke variabel. Kalau dipipe per-file ke `grep -q`,
# grep keluar begitu menemukan match → dpkg-deb kena SIGPIPE → pipefail
# membuat seluruh build gagal dengan pesan "Broken pipe" yang menyesatkan.
DEB_LISTING="$(dpkg-deb -c "$DEB_PATH")"

for required in \
    ./opt/examvan/examvan/notify.py \
    ./opt/examvan/examvan/ws.py \
    ./opt/examvan/examvan/security/base.py \
    ./opt/examvan/examvan/security/linux_backend.py \
    ./opt/examvan/examvan/security/windows_backend.py \
    ./opt/examvan/examvan/ui/waiting_approval.py \
    ./opt/examvan/run.sh \
    ./opt/examvan/requirements.txt \
    ./usr/bin/examvan \
    ./usr/share/xsessions/examvan-kiosk.desktop
do
    printf '%s\n' "$DEB_LISTING" | grep -qF " $required" \
        || die "Isi .deb tidak lengkap: $required tidak ada di dalam paket."
done

# Maintainer scripts TIDAK muncul di `dpkg-deb -c` (itu hanya data archive),
# jadi harus dicek lewat control tarball. Mode 755 wajib: tanpa itu dpkg
# tidak menandai skrip executable dan postinst gagal diam-diam.
CTRL_LISTING="$(dpkg-deb --ctrl-tarfile "$DEB_PATH" | tar -tv)"
for script in postinst prerm postrm; do
    printf '%s\n' "$CTRL_LISTING" | grep -qE "^-rwxr-xr-x.*\./$script\$" \
        || die "Maintainer script $script tidak ada atau tidak executable (butuh mode 755)."
done
printf '%s\n' "$CTRL_LISTING" | grep -qE '^-rw-r--r--.*\./control$' \
    || die "DEBIAN/control tidak ada di paket."

# Bytecode lokal tidak boleh bocor ke paket.
if printf '%s\n' "$DEB_LISTING" | grep -q '__pycache__\|\.pyc$'; then
    die "Bytecode build bocor ke dalam .deb (harus dibersihkan)."
fi

# Modul backend Windows boleh TIDAK berguna di Linux, tapi file-nya harus
# ada di paket supaya tidak ada fork lagi: kita sudah menyalin seluruh
# examvan/, jadi satu-satunya cara hilang adalah filter yang salah.
printf '%s\n' "$DEB_LISTING" | grep -qF "./opt/examvan/examvan/security/windows_backend.py" \
    || die "windows_backend.py hilang — berarti stage bukan hasil copy penuh."

# Launcher sistem harus executable juga, kalau tidak "examvan" di PATH
# gagal dengan "Permission denied".
printf '%s\n' "$DEB_LISTING" | grep -qE '^-rwxr-xr-x.*\./usr/bin/examvan$' \
    || die "/usr/bin/examvan tidak executable (butuh mode 755)."
ok "Isi .deb lengkap, maintainer scripts executable, bytecode bersih."

DEB_VER="$(dpkg-deb -f "$DEB_PATH" Version)"
[ "$DEB_VER" = "$VERSION" ] || die "Version di dalam paket ($DEB_VER) != yang diminta ($VERSION)."

DEB_SIZE="$(du -h "$DEB_PATH" | cut -f1)"

echo
ok "BUILD SUCCESS"
echo
echo "  Paket : $DEB_PATH  ($DEB_SIZE)"
echo "  Versi : $DEB_VER"
echo "  Build : $BUILD_NUM (commit $COMMIT_SHA)"
echo
echo "  Install : sudo dpkg -i $DEB_PATH"
echo "  Remove  : sudo dpkg -r examvan-linux   (prerm/PR/postrm membersihkan venv)"
echo "  Catatan : .deb tidak di-commit — build ulang dengan ./build-deb.sh"
echo
