#!/bin/bash
# mt4_start.sh - start MT4 (wine) under Xvfb, compile+attach push EA, one-time login.
set -x
export WINEDEBUG=-all
export HOME=/root
export XDG_RUNTIME_DIR=/tmp
export WINEPREFIX=/root/.wine

MT4_DIR="/stage/MetaTrader 4"
EA_SRC="/opt/mt4/GoldArenaPushEA.mq4"
EA_NAME="GoldArenaPushEA"
SHOTS="/opt/mt4/shots"
mkdir -p "$SHOTS"

# 1. init wine prefix (NO DISPLAY) - this is required; with DISPLAY set wineboot hangs
if [ ! -f "$WINEPREFIX/system.reg" ]; then
  echo "=== wineboot --init (no DISPLAY) ==="
  wineboot --init
fi

# 2. ensure automation tools exist (for one-time login)
if ! command -v xdotool >/dev/null 2>&1 || ! command -v scrot >/dev/null 2>&1; then
  if [ -f /etc/os-release ]; then . /etc/os-release; fi
  if [ "$ID" = "ubuntu" ]; then
    sed -i 's|http://archive.ubuntu.com/ubuntu|https://mirrors.tuna.tsinghua.edu.cn/ubuntu|g; s|http://security.ubuntu.com/ubuntu|https://mirrors.tuna.tsinghua.edu.cn/ubuntu|g' /etc/apt/sources.list 2>/dev/null
  elif [ "$ID" = "debian" ]; then
    for f in /etc/apt/sources.list.d/*.sources /etc/apt/sources.list; do
      [ -f "$f" ] || continue
      sed -i 's#deb\.debian\.org/debian-security#mirrors.tuna.tsinghua.edu.cn/debian-security#g; s#deb\.debian\.org/debian#mirrors.tuna.tsinghua.edu.cn/debian#g' "$f"
    done
  fi
  timeout 150 apt-get update >/dev/null 2>&1
  timeout 150 apt-get install -y xdotool scrot >/dev/null 2>&1
fi

# 3. place + compile EA into terminal MQL4/Experts
EXPERTS_DIR="$MT4_DIR/MQL4/Experts"
mkdir -p "$EXPERTS_DIR"
cp "$EA_SRC" "$EXPERTS_DIR/$EA_NAME.mq4" 2>/dev/null
wine "$MT4_DIR/metaeditor.exe" /compile:"$EXPERTS_DIR/$EA_NAME.mq4" >/tmp/compile.log 2>&1
ls -l "$EXPERTS_DIR/$EA_NAME.ex4" 2>&1
cat /tmp/compile.log 2>&1

# 4. start Xvfb
Xvfb :99 -screen 0 1024x768x24 >/tmp/x.log 2>&1 &
sleep 3
export DISPLAY=:99

# 5. start terminal
wine "$MT4_DIR/terminal.exe" >/tmp/mt4.log 2>&1 &

# 6. capture initial screenshot; login + EA attach is driven interactively via screenshots
sleep 20
scrot -o "$SHOTS/01_initial.png" 2>/dev/null

# 7. keep alive
wait
