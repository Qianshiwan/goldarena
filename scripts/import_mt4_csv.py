#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
MT4 历史数据 → 金归子本地 K 线库 导入器
========================================
把 MetaTrader 4 导出的 XAUUSD 历史 CSV 合并进 data/kline_history.json，
作为离线、零配额的 K 线回填源（尤其补齐日线）。

MT4 导出方法: 终端按 F2 打开 History Center → 选 XAUUSD 及周期 → 点 Export，
导出的 CSV 列: Date,Time,Open,High,Low,Close,Volume[,Spread]
(日期 YYYY.MM.DD, 时间经纪商服务器时间 HH:MM / HH:MM:SS)

用法:
  python scripts/import_mt4_csv.py
      --dir data/mt4_import        # CSV 所在目录(默认)
      --tz 2                       # 经纪商服务器时区相对 UTC 的偏移(小时)。GMT+2 填 2, GMT+3 填 3
      --max-bars 3000             # 每个周期最多保留的根数(保留最新的)

文件名需带周期标记以便自动识别, 例如:
  XAUUSD_M5.csv  XAUUSD_M15.csv  XAUUSD_M30.csv  XAUUSD_H1.csv  XAUUSD_D1.csv

注意:
  - 导入器把服务器时间减去 --tz 小时转成 UTC, 再对齐到 UTC 周期边界(与平台实时 K 线一致)。
  - 经纪商若启用夏令时(GMT+2/GMT+3 切换), 跨季数据会有最多 1 小时的边界偏差, 回填用途可接受。
  - 推荐流程: 先停网关 → 跑本脚本 → 再启动网关。网关启动的 seed 逻辑发现某周期已有充足缓存会跳过网络拉取, 不会覆盖导入的数据。
"""
import argparse
import csv
import json
import os
import sys
import calendar

# MT4 周期代码 -> 平台 period（用完整后缀精确匹配，避免 XAUUSDm15 被误判成 1m）
PERIOD_MAP = {
    "M1": "1m", "M5": "5m", "M15": "15m", "M30": "30m",
    "M60": "1h", "M240": "4h", "M1440": "1d", "M10080": "1w", "M43200": "1mo",
    "H1": "1h", "H4": "4h", "D1": "1d", "W1": "1w", "MN1": "1mo",
    "1M": "1m", "5M": "5m", "15M": "15m", "30M": "30m",
    "60M": "1h", "240M": "4h", "1440M": "1d", "10080M": "1w", "43200M": "1mo",
}

# 平台周期 -> 秒(用于对齐 UTC 周期边界)
PERIOD_SECONDS = {
    "1m": 60, "5m": 300, "15m": 900, "30m": 1800,
    "1h": 3600, "4h": 14400, "1d": 86400, "1w": 604800, "1mo": 2592000,
}

SYMBOL = "XAU"
CONTRACT_MONTH = "SPOT"


def detect_period(filename: str):
    import re
    base = filename.upper()
    base = re.sub(r'^XAUUSD', '', base)
    base = re.sub(r'\.CSV$', '', base)
    base = base.strip()
    return PERIOD_MAP.get(base)


def parse_datetime(date_str: str, time_str: str):
    date_str = date_str.strip()
    time_str = (time_str or "00:00").strip()
    for dfmt in ("%Y.%m.%d", "%Y-%m-%d", "%Y/%m/%d"):
        try:
            d = _strptime(date_str, dfmt)
            break
        except ValueError:
            d = None
    if d is None:
        return None
    for tfmt in ("%H:%M:%S", "%H:%M", "%H"):
        try:
            t = _strptime(time_str, tfmt)
            break
        except ValueError:
            t = None
    if t is None:
        return None
    return d.replace(hour=t.hour, minute=t.minute, second=t.second, microsecond=0)


def _strptime(s, fmt):
    from datetime import datetime
    return datetime.strptime(s, fmt)


def align_utc_ms(utc_dt, period: str) -> int:
    """把 UTC naive datetime 对齐到该周期的 UTC 起点, 返回毫秒时间戳。"""
    import datetime as dt
    secs = PERIOD_SECONDS.get(period, 60)
    epoch = calendar.timegm(utc_dt.timetuple())
    aligned = (epoch // secs) * secs
    return int(aligned) * 1000


def parse_csv(path: str, tz_offset: int):
    rows = []
    with open(path, "r", encoding="utf-8-sig", newline="") as f:
        reader = csv.reader(f)
        raw = list(reader)
    if not raw:
        return rows
    # 探测表头
    start = 0
    header = [c.strip().lower() for c in raw[0]]
    if "date" in header or "open" in header or "time" in header:
        start = 1
    col = {"date": 0, "time": 1, "open": 2, "high": 3, "low": 4, "close": 5, "volume": 6}
    if start == 1:
        for i, h in enumerate(header):
            if h in col:
                col[h] = i
    for r in raw[start:]:
        if len(r) < 6:
            continue
        try:
            dstr = r[col["date"]]
            tstr = r[col["time"]] if col["time"] < len(r) else "00:00"
            naive = parse_datetime(dstr, tstr)
            if naive is None:
                continue
            # 服务器时间 -> UTC
            import datetime as dt
            utc = naive - dt.timedelta(hours=tz_offset)
            o = float(r[col["open"]])
            h = float(r[col["high"]])
            l = float(r[col["low"]])
            c = float(r[col["close"]])
            v = float(r[col["volume"]]) if col["volume"] < len(r) and r[col["volume"]].strip() else 1.0
            if o <= 0 or c <= 0 or h <= 0 or l <= 0:
                continue
            rows.append((utc, o, h, l, c, v))
        except (ValueError, IndexError):
            continue
    return rows


def main():
    ap = argparse.ArgumentParser(description="MT4 CSV -> 金归子 kline_history.json 导入器")
    ap.add_argument("--dir", default="data/mt4_import", help="CSV 目录(默认 data/mt4_import)")
    ap.add_argument("--tz", type=float, default=0.0,
                    help="经纪商服务器时区相对 UTC 偏移(小时)。GMT+2 填 2, GMT+3 填 3")
    ap.add_argument("--max-bars", type=int, default=3000, help="每周期最多保留根数")
    ap.add_argument("--history", default="data/kline_history.json", help="目标 kline_history.json")
    args = ap.parse_args()

    d = args.dir
    if not os.path.isdir(d):
        os.makedirs(d, exist_ok=True)
        print(f"[import] 目录 {d} 不存在, 已创建。请把 MT4 导出的 CSV 放进去后重跑。")
        return

    files = [f for f in os.listdir(d) if f.lower().endswith(".csv")]
    if not files:
        print(f"[import] {d} 下没有 CSV 文件。请放入 XAUUSD_M5.csv / M15 / M30 / H1 / D1 等后重跑。")
        return

    # 载入现有 kline 库
    hist = {}
    if os.path.exists(args.history):
        try:
            hist = json.load(open(args.history, "r", encoding="utf-8"))
        except Exception as e:
            print(f"[import][warn] 读取现有 {args.history} 失败: {e}, 将以空库开始")
            hist = {}

    total_imported = 0
    summary = []
    for fn in sorted(files):
        period = detect_period(fn)
        if period is None:
            print(f"[import][skip] {fn}: 文件名无法识别周期, 跳过")
            continue
        path = os.path.join(d, fn)
        rows = parse_csv(path, args.tz)
        if not rows:
            print(f"[import][skip] {fn}: 没有解析到有效行")
            continue
        key = f"{SYMBOL}:{CONTRACT_MONTH}:{period}"
        existing = hist.get(key, [])
        merged = {b["timestamp"]: b for b in existing}
        imported_count = 0
        for utc, o, h, l, c, v in rows:
            ts = align_utc_ms(utc, period)
            merged[ts] = {
                "symbol": SYMBOL,
                "contract_month": CONTRACT_MONTH,
                "period": period,
                "open": o, "high": h, "low": l, "close": c,
                "volume": v,
                "timestamp": ts,
                "created_at": "0001-01-01T00:00:00Z",
            }
            imported_count += 1
        # 排序并裁剪到 max_bars(保留最新)
        bars = sorted(merged.values(), key=lambda b: b["timestamp"])
        if len(bars) > args.max_bars:
            bars = bars[-args.max_bars:]
        hist[key] = bars
        total_imported += imported_count
        summary.append((key, imported_count, len(bars)))
        print(f"[import] {fn} -> {key}: 导入 {imported_count} 根, 合并后共 {len(bars)} 根")

    with open(args.history, "w", encoding="utf-8") as f:
        json.dump(hist, f, ensure_ascii=False, separators=(",", ":"))

    print(f"[import] 完成。共导入 {total_imported} 根。已写入 {args.history}")
    print("[import] 下一步: 重启网关(先停后启), 启动日志应显示从本地 K 线库恢复, 日线等将显示导入的历史。")


if __name__ == "__main__":
    main()
