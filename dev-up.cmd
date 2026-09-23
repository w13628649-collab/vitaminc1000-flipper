@echo off
rem Local dev stack in WSL: dockerd + TimescaleDB + guild-server, and rebuild dist\flipper-client.exe.
rem Works from any directory, or double-click. Real logic lives in guild\dev-up.sh.
wsl -d Ubuntu -u root --cd "%~dp0." -- bash guild/dev-up.sh
if errorlevel 1 pause
