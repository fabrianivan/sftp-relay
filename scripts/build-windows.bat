@echo off
REM Builds dist\sftp-relay.exe with icon, manifest and version info.
REM Requires Go, a C compiler for CGO (e.g. MSYS2 mingw-w64) and goversioninfo:
REM   go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.4.1
setlocal
cd /d "%~dp0\.."

set VERSION=%1
if "%VERSION%"=="" set VERSION=dev

where goversioninfo >nul 2>&1 || (
  echo [!] goversioninfo not found. Install it with:
  echo     go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.4.1
  exit /b 1
)

if not exist dist mkdir dist
goversioninfo -64 -icon=assets\logo.ico -manifest=app.manifest versioninfo.json || exit /b 1

set CGO_ENABLED=1
go build -trimpath -ldflags "-H=windowsgui -s -w -X main.version=%VERSION%" -o dist\sftp-relay.exe . || exit /b 1
del resource.syso

echo [OK] dist\sftp-relay.exe
endlocal
