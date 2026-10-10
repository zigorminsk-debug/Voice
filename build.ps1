$ErrorActionPreference = "Stop"
$env:GOOS = "windows"
$env:GOARCH = "amd64"
go test ./...
go build -trimpath -ldflags="-s -w -H windowsgui" -o "ЦОД.exe" .
Write-Host "Готово: ЦОД.exe" -ForegroundColor Green
