#!/usr/bin/env bash
# EXAMVAN Linux Client — Installer
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/amnahwaida/EXAMVAN/main/desktop/install.sh | sudo bash
#   sudo ./install.sh              Install from local repo
#   sudo ./install.sh --uninstall  Remove EXAMVAN from system

set -euo pipefail

REPO_URL="https://github.com/amnahwaida/EXAMVAN"
BRANCH="main"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-.}")" && pwd)"
INSTALL_DIR="/opt/examvan"
VENV_DIR="$INSTALL_DIR/.venv"
BIN_LINK="/usr/bin/examvan"

# ── Colors ──────────────────────────────────────────────────────────────
RED='\033[1;31m'
GRN='\033[1;32m'
YLW='\033[1;33m'
BLU='\033[1;34m'
NC='\033[0m'

info()  { printf "${BLU}[INFO]${NC}  %s\n" "$*"; }
ok()    { printf "${GRN}[OK]${NC}    %s\n" "$*"; }
warn()  { printf "${YLW}[WARN]${NC}  %s\n" "$*"; }
err()   { printf "${RED}[ERR]${NC}   %s\n" "$*"; }

# ── Root check ────────────────────────────────────────────────────────
if [ "$(id -u)" -ne 0 ]; then
    err "Jalankan dengan sudo:"
    err "  curl -fsSL https://raw.githubusercontent.com/amnahwaida/EXAMVAN/main/desktop/install.sh | sudo bash"
    err "  # atau"
    err "  sudo ./install.sh"
    exit 1
fi

# ── Uninstall ──────────────────────────────────────────────────────────
if [ "${1:-}" = "--uninstall" ]; then
    info "Menghapus EXAMVAN dari sistem..."
    rm -f "$BIN_LINK"
    rm -f /usr/share/applications/examvan.desktop
    rm -f /usr/share/xsessions/examvan-kiosk.desktop
    rm -rf "$INSTALL_DIR"
    ok "EXAMVAN berhasil dihapus"
    exit 0
fi

# ── Banner ────────────────────────────────────────────────────────────
echo ""
echo "  ╔═══════════════════════════════════════╗"
echo "  ║     EXAMVAN Linux Client Installer     ║"
echo "  ╚═══════════════════════════════════════╝"
echo ""

# ── OS detection ──────────────────────────────────────────────────────
info "Mendeteksi sistem operasi..."
OS_ID=""
if [ -f /etc/os-release ]; then
    OS_ID=$(grep ^ID= /etc/os-release | cut -d= -f2 | tr -d '"')
fi
ok "Terdeteksi: ${OS_ID:-unknown}"

# ── Download source (curl mode) ─────────────────────────────────────────
SOURCE_DIR=""  # where examvan/ source live

resolve_source() {
    # Check if source files exist locally (local install mode)
    if [ -f "$SCRIPT_DIR/examvan/__main__.py" ] && [ -f "$SCRIPT_DIR/requirements.txt" ]; then
        SOURCE_DIR="$SCRIPT_DIR"
        info "Mode: install lokal ($SOURCE_DIR)"
        return
    fi

    # Check if running from repo root (git clone without cd desktop)
    if [ -f "$SCRIPT_DIR/desktop/examvan/__main__.py" ] && [ -f "$SCRIPT_DIR/desktop/requirements.txt" ]; then
        SOURCE_DIR="$SCRIPT_DIR/desktop"
        info "Mode: install lokal ($SOURCE_DIR)"
        return
    fi

    # No local source — download from GitHub
    info "Mode: install via curl ($REPO_URL)"
    info "Mengunduh sumber dari GitHub..."

    local tmpdir
    tmpdir=$(mktemp -d)

    # Prefer git clone (preserves exact file structure)
    if command -v git &>/dev/null; then
        git clone --depth 1 -b "$BRANCH" "$REPO_URL" "$tmpdir/repo" --quiet 2>/dev/null || true
        if [ -f "$tmpdir/repo/desktop/install.sh" ]; then
            SOURCE_DIR="$tmpdir/repo/desktop"
            ok "Sumber diunduh via git"
            return
        fi
        if [ -f "$tmpdir/repo/linux/install.sh" ]; then  # backward compat
            SOURCE_DIR="$tmpdir/repo/linux"
            ok "Sumber diunduh via git (legacy path)"
            return
        fi
    fi

    # Fallback: tarball
    if command -v curl &>/dev/null; then
        curl -fsSL "$REPO_URL/archive/refs/heads/$BRANCH.tar.gz" -o "$tmpdir/repo.tar.gz"
        if command -v tar &>/dev/null; then
            tar -xzf "$tmpdir/repo.tar.gz" -C "$tmpdir"
            local dirname="EXAMVAN-$BRANCH"
            if [ ! -d "$tmpdir/$dirname" ]; then
                # Try to detect extracted dir name
                dirname=$(ls "$tmpdir" | grep -v "repo.tar.gz" | head -1)
            fi
            if [ -f "$tmpdir/$dirname/desktop/install.sh" ]; then
                SOURCE_DIR="$tmpdir/$dirname/desktop"
                ok "Sumber diunduh via tarball"
                return
            fi
            if [ -f "$tmpdir/$dirname/linux/install.sh" ]; then  # backward compat
                SOURCE_DIR="$tmpdir/$dirname/linux"
                ok "Sumber diunduh via tarball (legacy path)"
                return
            fi
        fi
    fi

    err "Gagal mengunduh sumber dari GitHub."
    err "Pastikan git atau curl + tar tersedia."
    err "Atau clone repo secara manual:"
    err "  git clone $REPO_URL && cd EXAMVAN/linux && sudo ./install.sh"
    exit 1
}
APT_DEPS=(python3 python3-venv python3-pip xsel xdg-utils x11-utils)
DNF_DEPS=(python3 python3-pip python3-devel xsel xdg-utils libX11)
PACMAN_DEPS=(python python-pip xsel xdg-utils libx11)
ZAPPER_DEPS=(python3 python3-venv python3-pip xsel xdg-utils x11-utils)
RHEL_DEPS=(python3 python3-pip python3-devel xsel xdg-utils libX11)
# Build deps for PyMuPDF compilation (when system package unavailable)
APT_BUILD_DEPS=(python3-dev build-essential)
DNF_BUILD_DEPS=(python3-devel gcc gcc-c++)
PACMAN_BUILD_DEPS=(python python-devel gcc)
RHEL_BUILD_DEPS=(python3-devel gcc gcc-c++)

install_sys_deps() {
    case "$OS_ID" in
        ubuntu|debian|linuxmint|pop|elementary|zorin)
            info "Menginstall dependensi sistem (apt)..."
            apt-get update -qq
            DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "${APT_DEPS[@]}" 2>/dev/null || {
                warn "Beberapa paket gagal diinstall, melanjutkan..."
            }
            DEBIAN_FRONTEND=noninteractive apt-get install -y -qq tzdata 2>/dev/null || true
            ;;
        fedora)
            info "Menginstall dependensi sistem (dnf)..."
            dnf install -y "${DNF_DEPS[@]}" 2>/dev/null || warn "Beberapa paket gagal diinstall"
            ;;
        rhel|centos|rocky|alma)
            info "Menginstall dependensi sistem (dnf)..."
            dnf install -y "${RHEL_DEPS[@]}" 2>/dev/null || warn "Beberapa paket gagal diinstall"
            if ! command -v pip3 &>/dev/null; then
                dnf install -y epel-release 2>/dev/null || true
                dnf install -y python3-pip 2>/dev/null || true
            fi
            ;;
        arch|manjaro|endeavouros)
            info "Menginstall dependensi sistem (pacman)..."
            pacman -Syu --noconfirm "${PACMAN_DEPS[@]}" 2>/dev/null || warn "Beberapa paket gagal diinstall"
            ;;
        opensuse*|suse)
            info "Menginstall dependensi sistem (zypper)..."
            zypper install -y "${ZAPPER_DEPS[@]}" 2>/dev/null || warn "Beberapa paket gagal diinstall"
            ;;
        *)
            warn "Sistem operasi '${OS_ID}' tidak dikenali."
            warn "Mencoba install dependensi via paket manager default..."
            command -v apt-get && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "${APT_DEPS[@]}" 2>/dev/null && return
            command -v dnf && dnf install -y "${DNF_DEPS[@]}" 2>/dev/null && return
            command -v zypper && zypper install -y "${ZAPPER_DEPS[@]}" 2>/dev/null && return
            warn "Tidak bisa install otomatis. Pastikan Python 3.8+, python3-venv, python3-pip."
            ;;
    esac
}

# ── Check python version ──────────────────────────────────────────────
check_python() {
    info "Memeriksa Python..."
    if ! command -v python3 &>/dev/null; then
        err "python3 tidak ditemukan. Install Python 3.8+ terlebih dahulu."
        exit 1
    fi
    PY_VER=$(python3 -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')")
    PY_MAJOR=$(echo "$PY_VER" | cut -d. -f1)
    PY_MINOR=$(echo "$PY_VER" | cut -d. -f2)
    if [ "$PY_MAJOR" -lt 3 ] || { [ "$PY_MAJOR" -eq 3 ] && [ "$PY_MINOR" -lt 8 ]; }; then
        err "Python 3.8+ diperlukan, ditemukan Python $PY_VER"
        exit 1
    fi
    ok "Python $PY_VER"
}

# ── Install PyQt5 (system or pip) ─────────────────────────────────────
install_pyqt5() {
    info "Memeriksa PyQt5..."
    # Try system package first (faster, smaller)
    local pyqt_ok=false
    case "$OS_ID" in
        ubuntu|debian|linuxmint|pop|elementary|zorin)
            if apt-get install -y -qq python3-pyqt5 2>/dev/null; then
                pyqt_ok=true
            fi
            ;;
        fedora)
            if dnf install -y python3-qt5 2>/dev/null; then
                pyqt_ok=true
            fi
            ;;
        arch|manjaro|endeavouros)
            if pacman -S --noconfirm python-pyqt5 2>/dev/null; then
                pyqt_ok=true
            fi
            ;;
        opensuse*|suse)
            if zypper install -y python3-qt5 2>/dev/null; then
                pyqt_ok=true
            fi
            ;;
    esac

    if [ "$pyqt_ok" = true ]; then
        ok "PyQt5 (sistem)"
    else
        warn "PyQt5 sistem tidak tersedia, menginstall via pip..."
        "$VENV_DIR/bin/pip" install PyQt5 --quiet
        ok "PyQt5 (pip)"
    fi
}

# ── Install PyMuPDF (system or pip) ───────────────────────────────────
install_pymupdf() {
    info "Memeriksa PyMuPDF..."
    local mupdf_ok=false
    case "$OS_ID" in
        ubuntu|debian|linuxmint|pop|elementary|zorin)
            if apt-get install -y -qq python3-pymupdf 2>/dev/null; then
                mupdf_ok=true
            fi
            ;;
    esac

    if [ "$mupdf_ok" = true ]; then
        ok "PyMuPDF (sistem)"
    else
        warn "PyMuPDF tidak tersedia, menginstall via pip..."
        case "$OS_ID" in
            ubuntu|debian|linuxmint|pop|elementary|zorin)
                apt-get install -y -qq "${APT_BUILD_DEPS[@]}" 2>/dev/null || true
                ;;
            fedora)
                dnf install -y "${DNF_BUILD_DEPS[@]}" 2>/dev/null || true
                ;;
            rhel|centos|rocky|alma)
                dnf install -y "${RHEL_BUILD_DEPS[@]}" 2>/dev/null || true
                ;;
            arch|manjaro|endeavouros)
                pacman -S --noconfirm "${PACMAN_BUILD_DEPS[@]}" 2>/dev/null || true
                ;;
            *)
                command -v apt-get && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "${APT_BUILD_DEPS[@]}" 2>/dev/null || true
                command -v dnf && dnf install -y "${DNF_BUILD_DEPS[@]}" 2>/dev/null || true
                ;;
        esac
        "$VENV_DIR/bin/pip" install PyMuPDF --quiet
        ok "PyMuPDF (pip)"
    fi
}

# ── Install kiosk deps ────────────────────────────────────────────────
install_kiosk_deps() {
    info "Memeriksa dependensi kiosk..."
    local missing=()
    for cmd in Xephyr openbox; do
        if ! command -v "$cmd" &>/dev/null; then
            missing+=("$cmd")
        fi
    done

    if [ ${#missing[@]} -eq 0 ]; then
        ok "Kiosk dependencies ready"
        return
    fi

    warn "Mode kiosk membutuhkan: ${missing[*]}"
    warn "Install secara manual jika diperlukan:"
    case "$OS_ID" in
        ubuntu|debian|linuxmint|pop|elementary|zorin)
            echo "  sudo apt install xserver-xephyr openbox"
            ;;
        fedora)
            echo "  sudo dnf install xorg-x11-server-Xephyr openbox"
            ;;
        arch|manjaro|endeavouros)
            echo "  sudo pacman -S xorg-server-xephyr openbox"
            ;;
    esac
}

# ── Copy files ────────────────────────────────────────────────────────
copy_files() {
    info "Menyalin file ke $INSTALL_DIR..."

    # Create target directories
    mkdir -p "$INSTALL_DIR"
    mkdir -p /usr/share/applications
    mkdir -p /usr/share/xsessions
    mkdir -p /usr/share/icons/hicolor/256x256/apps

    # Remove old installation if exists
    rm -rf "$INSTALL_DIR/examvan"
    rm -f "$INSTALL_DIR/run.sh"
    rm -f "$INSTALL_DIR/requirements.txt"

    # Copy source code (exclude __pycache__ and .venv)
    cp -r "$SOURCE_DIR/examvan" "$INSTALL_DIR/examvan"
    # Clean pycache from copied source
    find "$INSTALL_DIR/examvan" -name "__pycache__" -type d -exec rm -rf {} + 2>/dev/null || true
    find "$INSTALL_DIR/examvan" -name "*.pyc" -delete 2>/dev/null || true

    # Copy support files
    cp "$SOURCE_DIR/run.sh" "$INSTALL_DIR/run.sh"
    cp "$SOURCE_DIR/requirements.txt" "$INSTALL_DIR/requirements.txt"
    chmod +x "$INSTALL_DIR/run.sh"

    # Desktop file
    if [ -f "$SOURCE_DIR/pkg-build/usr/share/applications/examvan.desktop" ]; then
        cp "$SOURCE_DIR/pkg-build/usr/share/applications/examvan.desktop" /usr/share/applications/examvan.desktop
    fi

    # Kiosk session
    # Hanya dari pkg-build. Sebelumnya ada dua file .desktop yang berbeda
    # (examvan_kiosk.desktop di root, dan pkg-build/.../xsessions/) sehingga
    # jalur install.sh dan jalur .deb bisa memasang Exec yang berbeda. Root
    # file-nya sudah dihapus; pkg-build adalah satu-satunya sumber.
    XSESSIONS_SRC="$SOURCE_DIR/pkg-build/usr/share/xsessions/examvan-kiosk.desktop"
    if [ -f "$XSESSIONS_SRC" ]; then
        cp "$XSESSIONS_SRC" /usr/share/xsessions/examvan-kiosk.desktop
        chmod 644 /usr/share/xsessions/examvan-kiosk.desktop
    fi

    # Icon
    if [ -f "$SOURCE_DIR/pkg-build/usr/share/icons/hicolor/256x256/apps/examvan.png" ]; then
        cp "$SOURCE_DIR/pkg-build/usr/share/icons/hicolor/256x256/apps/examvan.png" \
           /usr/share/icons/hicolor/256x256/apps/examvan.png
    fi

    # Launcher
    # Salin pkg-build/usr/bin/examvan apa adanya, JANGAN tulis ulang inline.
    # Versi lama meng-hardcode `exec python -m examvan "$@"`, yang membuat
    # /usr/bin/examvan --kiosk tidak melakukan apa-apa: flag itu hanya
    # menyalakan boolean yang dibaca di dalam _activate_strict(). Seluruh
    # logika (venv, theme, Xephyr) sudah ada di /opt/examvan/run.sh.
    LAUNCHER_SRC="$SOURCE_DIR/pkg-build/usr/bin/examvan"
    if [ -f "$LAUNCHER_SRC" ]; then
        cp "$LAUNCHER_SRC" "$BIN_LINK"
        chmod +x "$BIN_LINK"
    else
        error "Launcher tidak ditemukan: $LAUNCHER_SRC"
    fi

    ok "File tersalin"
}

# ── Create virtual environment ────────────────────────────────────────
setup_venv() {
    info "Membuat virtual environment..."

    # Remove old venv if exists
    rm -rf "$VENV_DIR"

    python3 -m venv --system-site-packages "$VENV_DIR"
    "$VENV_DIR/bin/pip" install --upgrade pip --quiet

    ok "Virtual environment siap"
}

# ── Main ─────────────────────────────────────────────────────────────
main() {
    resolve_source
    install_sys_deps
    check_python
    copy_files
    setup_venv
    install_pyqt5
    install_pymupdf
    install_kiosk_deps

    # Update icon cache
    if command -v gtk-update-icon-cache &>/dev/null; then
        gtk-update-icon-cache /usr/share/icons/hicolor/ 2>/dev/null || true
    fi
    if command -v update-desktop-database &>/dev/null; then
        update-desktop-database 2>/dev/null || true
    fi

    echo ""
    echo "  ╔═══════════════════════════════════════╗"
    echo "  ║        INSTALASI SELESAI!              ║"
    echo "  ╚═══════════════════════════════════════╝"
    echo ""
    echo "  Jalankan:  examvan"
    echo "  Atau cari 'EXAMVAN' di menu aplikasi."
    echo ""
    echo "  Mode kiosk:  examvan --kiosk"
    echo "  Atau pilih sesi 'EXAMVAN Kiosk' dari login manager."
    echo ""
    echo "  Uninstall:  curl -fsSL https://raw.githubusercontent.com/amnahwaida/EXAMVAN/main/desktop/install.sh | sudo bash -s -- --uninstall"
    echo ""
}

main
