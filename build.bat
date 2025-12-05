@echo off
setlocal enabledelayedexpansion

REM ==========================================================
REM  🚀 Go SFTP Uploader Auto Build Script
REM  - Auto-increments build version in versioninfo.json
REM  - Embeds icon + manifest
REM  - Builds hidden-window EXE (tray app)
REM  - Optional UPX compression
REM ==========================================================

set APP_NAME=SFTPWatchdog
set OUTPUT_DIR=dist
set VERSION_FILE=versioninfo.json
set ICON_FILE=assets\logo.ico
set MANIFEST_FILE=app.manifest

echo.
echo ===============================================
echo [*] Building %APP_NAME%
echo ===============================================

REM --- Step 1: Generate timestamp ---
for /f "tokens=1-4 delims=/ " %%a in ('date /t') do (
    set DATESTR=%%d-%%b-%%c
)
for /f "tokens=1-2 delims=: " %%a in ("%time%") do (
    set TIMESTR=%%a%%b
)
set TIMESTR=%TIMESTR::=%
set BUILD_TIME=%DATESTR%_%TIMESTR%

REM --- Step 2: Ensure dist folder exists ---
if not exist "%OUTPUT_DIR%" mkdir "%OUTPUT_DIR%"

REM --- Step 3: Auto-increment build version in versioninfo.json ---
if not exist "%VERSION_FILE%" (
    echo [!] %VERSION_FILE% not found. Creating default one...
    echo { > "%VERSION_FILE%"
    echo   "FixedFileInfo": { >> "%VERSION_FILE%"
    echo     "FileVersion": {"Major":1,"Minor":0,"Patch":0,"Build":0}, >> "%VERSION_FILE%"
    echo     "ProductVersion": {"Major":1,"Minor":0,"Patch":0,"Build":0} >> "%VERSION_FILE%"
    echo   }, >> "%VERSION_FILE%"
    echo   "StringFileInfo": {"FileDescription":"SFTP File Uploader","ProductName":"SFTP Uploader"}, >> "%VERSION_FILE%"
    echo   "IconPath":"assets/logo.ico" >> "%VERSION_FILE%"
    echo } >> "%VERSION_FILE%"
)

echo [*] Reading current version from %VERSION_FILE%...

for /f "tokens=1-4 delims=:.," %%a in ('findstr /i "Build" "%VERSION_FILE%" ^| findstr /v "ProductVersion"') do (
    set /a BUILD_NUM=%%b
)

set /a NEW_BUILD_NUM=BUILD_NUM+1
echo [*] Incrementing build number: %BUILD_NUM% → %NEW_BUILD_NUM%

REM --- Step 4: Update version file ---
powershell -Command ^
    "(Get-Content '%VERSION_FILE%' -Raw) -replace '\"Build\": *[0-9]+', '\"Build\": %NEW_BUILD_NUM%' | Set-Content '%VERSION_FILE%'"

REM --- Step 5: Generate resource.syso ---
echo [*] Embedding icon and manifest...
if exist resource.syso del resource.syso
goversioninfo -icon="%ICON_FILE%" -manifest="%MANIFEST_FILE%" -64=true

REM --- Step 6: Build EXE ---
echo [*] Compiling Go executable...
go build -ldflags="-H=windowsgui -s -w" -o "%OUTPUT_DIR%\%APP_NAME%_%BUILD_TIME%.exe" ./cmd/uploader

if %errorlevel% neq 0 (
    echo [!] ❌ Build failed. Check your Go code.
    exit /b 1
)

REM --- Step 7: Compress EXE (optional) ---
if exist "%ProgramFiles%\UPX\upx.exe" (
    echo [*] Compressing with UPX...
    "%ProgramFiles%\UPX\upx.exe" --best "%OUTPUT_DIR%\%APP_NAME%_%BUILD_TIME%.exe"
) else (
    echo [*] UPX not found. Skipping compression.
)

echo.
echo ✅ Build successful!
echo Output: %OUTPUT_DIR%\%APP_NAME%_%BUILD_TIME%.exe
echo Version: Build %NEW_BUILD_NUM%
echo ===============================================
echo.
pause
endlocal
