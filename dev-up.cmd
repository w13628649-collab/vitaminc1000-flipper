@echo off
rem Local dev stack in WSL: dockerd + TimescaleDB + guild-server, and rebuild dist\flipper-client.exe.
rem Works from any directory, or double-click. Real logic lives in guild\dev-up.sh.
rem   dev-up.cmd           build server + client, then (re)start everything
rem   dev-up.cmd nobuild   just bring services up with the last deployed binary (after a reboot)
set "NB="
if /i "%~1"=="nobuild" set "NB=env NOBUILD=1"
wsl -d Ubuntu -u root --cd "%~dp0." -- %NB% bash guild/dev-up.sh
if errorlevel 1 (
  pause
  exit /b 1
)
rem WSL shuts the whole distro down shortly after the last wsl.exe session exits, killing dockerd,
rem the database and guild-server with it. Keep one hidden "sleep infinity" session alive; it goes
rem away on logoff/reboot. Only start one: skip if it is already running.
powershell -NoProfile -ExecutionPolicy Bypass -Command "if (-not (Get-CimInstance Win32_Process | Where-Object { $_.Name -eq 'wsl.exe' -and $_.CommandLine -match 'sleep infinity' })) { Start-Process wsl.exe -WindowStyle Hidden -ArgumentList '-d','Ubuntu','-u','root','--','sleep','infinity'; echo 'keep-alive started' } else { echo 'keep-alive already running' }"
