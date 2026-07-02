#!/bin/bash
# Script untuk menjalankan aplikasi secara lokal untuk Development

echo "=> Menghentikan webui-server di docker jika berjalan..."
docker compose stop webui-server nginx-lb

echo "=> Menjalankan database dan redis (menggunakan docker-compose.override.yml untuk expose port)..."
docker compose up -d db redis

echo "=> Mengekspor variabel environment dari .env..."
set -a
source .env
set +a

# Pastikan REDIS_URL diarahkan ke localhost jika ingin dipakai
export REDIS_URL="redis://localhost:6379/0"

echo "=> Menjalankan server Go secara native (Tekan Ctrl+C untuk berhenti)..."
echo "=> Akses aplikasi di http://localhost:$PORT"

# Cek apakah `air` terinstall untuk hot-reload
if command -v air &> /dev/null; then
    echo "=> [Air terdeteksi] Menjalankan dengan live-reload..."
    air
elif [ -x "$(command -v ~/go/bin/air)" ]; then
    echo "=> [Air terdeteksi] Menjalankan dengan live-reload..."
    ~/go/bin/air
else
    echo "=> [Tip] Install 'air' untuk auto-rebuild setiap simpan file:"
    echo "   go install github.com/air-verse/air@latest"
    echo "=> Menjalankan dengan 'go run' (butuh restart manual saat ada perubahan)..."
    go run cmd/server/main.go
fi
