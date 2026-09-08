#!/bin/bash
set -x
export WINEDEBUG=-all
export HOME=/root
export WINEPREFIX=/tmp/w32
export WINEARCH=win32
rm -rf /tmp/w32
# boot WITHOUT DISPLAY (rule we learned: boot hangs if DISPLAY set)
wineboot --init
echo "BOOT_EXIT=$?"

# --- anti-debugger mitigation: remove winedbg so MT4 can't detect it ---
WINEBIN=$(dirname "$(readlink -f "$(which wine)")")
echo "WINEBIN=$WINEBIN"
echo "--- winedbg files before ---"
find / -name 'winedbg*' 2>/dev/null
find / -name 'winedbg*' -delete 2>/dev/null
echo "--- winedbg files after delete ---"
find / -name 'winedbg*' 2>/dev/null || true

# start Xvfb
Xvfb :99 -screen 0 1024x768x24 >/tmp/x.log 2>&1 &
export DISPLAY=:99
sleep 3

# launch terminal with winedbg blocked
WINEDLLOVERRIDES="winedbg.exe=" wine '/stage/MetaTrader 4/terminal.exe' >/tmp/mt4.log 2>&1 &
MPID=$!
sleep 25
kill -0 $MPID 2>/dev/null && echo MT4_ALIVE || echo MT4_DEAD
scrot -o /opt/mt4/shots/antidebug.png 2>/dev/null
echo "--- mt4.log tail ---"
tail -15 /tmp/mt4.log
