"""EXAMVAN desktop client."""

# SATU-SATUNYA sumber nomor versi di repo ini.
#
# Sebelumnya ada dua literal: `__version__ = "1.0.0"` dan
# `APP_VERSION = "2.5.0"`. Yang dibaca guru di layar adalah `__version__`
# (label di dialog server pernah menulis "v1.0.0 (API 2.5.0)"), sementara
# installer melaporkan 2.5.0 dan resource exe melaporkan 1.0.0.0. Guru
# melapor "v1.0.0", installer bilang 2.5.0, tidak ada yang bisa dicocokkan
# ke build.
#
# Arah dependensinya penting: APP_VERSION yang literal, __version__ yang
# diturunkan. Alasannya, build_info.py, build-deb.sh, dan langkah "Verify
# package contents" di ci.yml membaca `APP_VERSION = "..."` dengan regex
# tanpa mengeksekusi source. Kalau arahnya dibalik, ketiganya ikut gagal
# diam-diam dan paket .deb/${dist} terlabel versi fallback yang salah.
APP_VERSION = "2.5.0"

# Yang ditampilkan di layar. Diturunkan, bukan literal kedua.
__version__ = APP_VERSION
