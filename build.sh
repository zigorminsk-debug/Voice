#!/bin/sh
# DocDiff — сборка приложения.
#   ./build.sh          — сборка под текущую ОС
#   ./build.sh windows  — кросс-сборка Windows (docdiff.exe)
set -e
cd "$(dirname "$0")"

TARGET="${1:-}"
mkdir -p dist

CGO_ENABLED=0
export CGO_ENABLED

if [ "$TARGET" = "windows" ]; then
    echo "Сборка dist/docdiff.exe (windows/amd64)..."
    GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/docdiff.exe ./cmd/docdiff
    echo "Готово: dist/docdiff.exe"
else
    OUT=dist/docdiff
    echo "Сборка $OUT ..."
    go build -trimpath -ldflags "-s -w" -o "$OUT" ./cmd/docdiff
    echo "Готово: $OUT"
fi
