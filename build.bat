@echo off
chcp 65001 >nul
setlocal
rem ============================================================
rem  DocDiff — сборка Windows-приложения (сравнение Excel/Word)
rem  Требуется установленный Go 1.20 или новее: https://go.dev/dl/
rem ============================================================

echo [1/3] Проверка Go...
where go >nul 2>nul
if errorlevel 1 (
    echo ОШИБКА: Go не найден. Установите Go с https://go.dev/dl/ и повторите.
    exit /b 1
)
go version

echo [2/3] Сборка docdiff.exe ...
set CGO_ENABLED=0
set GOOS=windows
go build -trimpath -ldflags "-s -w" -o dist\docdiff.exe .\cmd\docdiff
if errorlevel 1 (
    echo ОШИБКА сборки.
    exit /b 1
)

echo [3/3] Готово: dist\docdiff.exe
echo.
echo Запуск:  dist\docdiff.exe
echo Или из консоли: dist\docdiff.exe old.xlsx new.xlsx
endlocal
