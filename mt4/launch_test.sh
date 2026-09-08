#!/bin/bash
set -x
export WINEDEBUG=-all
export HOME=/root
export XDG_RUNTIME_DIR=/tmp
export WINEPREFIX=/root/.wine
rm -rf /root/.wine

echo "=== wineboot --init (NO DISPLAY) ==="
wineboot --init
echo "BOOT_EXIT=$?"

echo "=== start Xvfb + launch terminal.exe (WITH DISPLAY) ==="
command -v Xvfb >/dev/null && echo XVFB_OK || { echo NO_XVFB; exit 2; }
Xvfb :99 -screen 0 1024x768x24 >/tmp/x.log 2>&1 &
sleep 3
export DISPLAY=:99

cd "/stage/MetaTrader 4" 2>/dev/null || { echo NO_STAGE_DIR; ls /stage; exit 3; }
wine terminal.exe >/tmp/mt4.log 2>&1 &
MPID=$!
sleep 45
if kill -0 $MPID 2>/dev/null; then echo MT4_RUNNING_OK; else echo MT4_DIED; fi
echo "--- mt4.log tail ---"
tail -30 /tmp/mt4.log
echo "--- x.log tail ---"
tail -5 /tmp/x.log
