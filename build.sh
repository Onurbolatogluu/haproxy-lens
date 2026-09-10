#!/bin/bash
# Kaynaktan derleme. Gerekenler: Go 1.22+ ve Node 18+.
# Çıktı: ./haproxy-lens (tek dosya, bağımlılıksız, linux/amd64)
#   GOARCH=arm64 ./build.sh   ARM sunucular için
set -euo pipefail
cd "$(dirname "$0")"
VERSION="$(git describe --tags --always 2>/dev/null || echo dev)"
VERSION="${VERSION#v}"
(cd webapp && npm ci && npm run build)
CGO_ENABLED=0 GOOS=linux GOARCH="${GOARCH:-amd64}" \
  go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o haproxy-lens .
echo "Hazır: ./haproxy-lens ($VERSION)"
