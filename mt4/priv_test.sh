#!/bin/bash
set -x
export WINEDEBUG=-all
export HOME=/root
export WINEPREFIX=/root/.wine
rm -rf /root/.wine

echo "=================== ENV ==================="
wine --version
echo "--- which wine/wine64 ---"
which wine wine64
echo "--- 32-bit libwine ---"
ls -la /usr/lib/i386-linux-gnu/libwine.so* 2>/dev/null || echo "NO 32-bit libwine.so"
echo "--- 64-bit libwine ---"
ls -la /usr/lib/x86_64-linux-gnu/libwine.so* 2>/dev/null || echo "NO 64-bit libwine.so"
echo "--- wine builtin dll dirs ---"
ls -d /usr/lib/i386-linux-gnu/wine 2>/dev/null && ls /usr/lib/i386-linux-gnu/wine/ 2>/dev/null | head
ls -d /usr/lib/x86_64-linux-gnu/wine 2>/dev/null && ls /usr/lib/x86_64-linux-gnu/wine/ 2>/dev/null | head

echo "=================== LDD 64-bit libwine ==================="
ldd /usr/lib/x86_64-linux-gnu/libwine.so.1 2>&1 | grep -i "not found" || echo "64-bit libwine: all deps present"

echo "=================== LDD 32-bit libwine ==================="
if [ -f /usr/lib/i386-linux-gnu/libwine.so.1 ]; then
  ldd /usr/lib/i386-linux-gnu/libwine.so.1 2>&1 | grep -i "not found" || echo "32-bit libwine: all deps present"
else
  echo "skip 32-bit ldd (no libwine)"
fi

echo "=================== mmap_min_addr / personality ==================="
cat /proc/sys/vm/mmap_min_addr 2>/dev/null
uname -m

echo "=================== wineboot --init (win64 default) ==================="
wineboot --init
echo "WINEBOOT_EXIT=$?"

echo "=================== wineboot --init (win32) ==================="
export WINEPREFIX=/root/.wine32
rm -rf /root/.wine32
WINEARCH=win32 wineboot --init
echo "WINEBOOT32_EXIT=$?"
