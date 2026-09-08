#!/bin/bash
export WINEDEBUG=-all
export HOME=/root
export WINEPREFIX=/tmp/wine
rm -rf /tmp/wine
# fix debian trixie deb822 mirror so apt works from CN
if [ -d /etc/apt/sources.list.d ]; then
  for f in /etc/apt/sources.list.d/*.sources; do
    [ -f "$f" ] || continue
    sed -i 's|http://deb.debian.org/debian|https://mirrors.tuna.tsinghua.edu.cn/debian|g; s|http://deb.debian.org/debian-security|https://mirrors.tuna.tsinghua.edu.cn/debian-security|g' "$f"
  done
fi
command -v scrot >/dev/null 2>&1 || { timeout 180 apt-get update >/dev/null 2>&1; timeout 180 apt-get install -y scrot xdotool >/dev/null 2>&1; }
echo "scrot: $(command -v scrot)  xdotool: $(command -v xdotool)"
# boot WITHOUT DISPLAY
wineboot --init
echo "BOOT_EXIT=$?"
Xvfb :99 -screen 0 1024x768x24 >/tmp/x.log 2>&1 &
export DISPLAY=:99
sleep 3
wine '/stage/MetaTrader 4/terminal.exe' >/tmp/mt4.log 2>&1 &
MPID=$!
sleep 25
# try to dismiss any popup (OK / close / Esc)
xdotool key Escape 2>/dev/null
xdotool search --name 'MetaTrader' windowactivate --sync 2>/dev/null && xdotool key Escape 2>/dev/null
sleep 3
kill -0 $MPID 2>/dev/null && echo MT4_ALIVE || echo MT4_DEAD
scrot -o /opt/mt4/shots/staging.png 2>/dev/null
echo "--- mt4.log tail ---"
tail -20 /tmp/mt4.log
