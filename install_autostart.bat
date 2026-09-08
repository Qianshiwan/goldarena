@echo off
REM Sina gold relay - register ONLOGON autostart task (more reliable than startup folder)
REM Usage: right-click this file -> Run as administrator
schtasks /Create /TN "SinaGoldRelay" /TR "\"C:\Program Files\Python311\pythonw.exe\" \"D:\tools\Jinguizigoldtrader\goldarena\sina_gold_relay.py\"" /SC ONLOGON /F
if %errorlevel%==0 (echo [OK] SinaGoldRelay task registered; runs in background at next logon) else (echo [FAIL] please run this file as administrator)
pause
