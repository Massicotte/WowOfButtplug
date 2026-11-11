@echo off
echo === WowOfButtplug ===
echo.

rem Start Intiface Central if it is installed but not already running
tasklist /FI "IMAGENAME eq IntifaceCentral.exe" 2>nul | find /I "IntifaceCentral.exe" >nul
if errorlevel 1 (
  if exist "%ProgramFiles%\Intiface Central\IntifaceCentral.exe" (
    start "" "%ProgramFiles%\Intiface Central\IntifaceCentral.exe"
  ) else if exist "%LOCALAPPDATA%\Programs\Intiface Central\IntifaceCentral.exe" (
    start "" "%LOCALAPPDATA%\Programs\Intiface Central\IntifaceCentral.exe"
  ) else (
    echo Intiface Central was not found on this PC.
    echo Install it from https://intiface.com/central/ and run this file again.
    echo.
  )
)

echo 1. In Intiface Central: turn your toy on, then click "Start Server".
echo 2. The bridge below waits for the server and connects automatically.
echo 3. Keep the WoW window in the foreground and take some damage!
echo.
"%~dp0WowOfButtplug.exe"
echo.
pause
