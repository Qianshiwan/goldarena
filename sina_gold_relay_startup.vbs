' sina_gold_relay_startup.vbs
' 隐藏(无窗口)启动 sina_gold_relay.py, 用于 Windows 开机自启常驻。
Set WshShell = CreateObject("WScript.Shell")
PyExe  = "C:\Program Files\Python311\pythonw.exe"
Script = "D:\tools\Jinguizigoldtrader\goldarena\sina_gold_relay.py"
WshShell.Run """" & PyExe & """ """ & Script & """", 0, False
