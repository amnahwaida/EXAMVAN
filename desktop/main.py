"""Entry point build Windows (.exe) — python main.py / EXAMVAN.exe.

Mengapa file ini ada: PyInstaller menjalankan file entry sebagai SCRIPT
LEPAS (bukan sebagai bagian dari package). File examvan/__main__.py memakai
relative import (`from .ui.styles import ...`) yang gagal bila dijalankan
langsung: "ImportError: attempted relative import with no known parent
package". Stub ini mengimpor package examvan secara normal sehingga
PyInstaller menganalisis seluruh graf import package dan aplikasi benar-benar
menyala saat EXAMVAN.exe dijalankan.

Entry alternatif: `python -m examvan` (dari folder desktop/ — dipakai
run.ps1 / install.ps1).
"""

from examvan.__main__ import main

if __name__ == "__main__":
    main()
