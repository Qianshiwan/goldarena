@echo off
REM 新浪黄金桥接 - 注册"登录时自动启动"(比启动文件夹更可靠, 不会被跳过)
REM 用法: 右键本文件 -> 以管理员身份运行
schtasks /Create /TN "SinaGoldRelay" /TR "wscript.exe \"D:\tools\Jinguizigoldtrader\goldarena\sina_gold_relay_startup.vbs\"" /SC ONLOGON /F
if %errorlevel%==0 (echo [OK] 已注册任务 SinaGoldRelay, 下次登录会自动后台运行) else (echo [失败] 请确认以管理员身份运行此文件)
pause
