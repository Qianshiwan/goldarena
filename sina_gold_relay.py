#!/usr/bin/env python3
# sina_gold_relay.py
# 轻量伦敦金行情桥接: 本机拉新浪 hf_XAU (伦敦金现货, USD/盎司, 实时)
#   -> POST 到金归子后端 /api/v1/market/push
# 不依赖 MT4, 不需要券商账号。仅要求运行本脚本的机器能访问 hq.sinajs.cn (中国本机通常可以)。
# 后端对该推送源的优先级高于 gold-api.io; 休市无 tick 时本脚本暂停推送, 后端自动降级 gold-api。
#
# 后台常驻(开机自启):
#   用 pythonw 运行本脚本(见 sina_gold_relay_startup.vbs), 日志写入同目录 sina_relay.log

import time, json, logging, logging.handlers, urllib.request, urllib.error, datetime, os, ssl

SINA_URL   = "https://hq.sinajs.cn/list=hf_XAU"
REFERER    = "https://finance.sina.com.cn"
PUSH_TOKEN = "GAmt4Push_8Kx2qL9vRtZ"
# 域名优先; 若被中间层 RST(10054), 直连 IP + Host 头兜底(与 curl 实测一致)
PUSH_URL_HOST = "https://www.kinguizi.top/api/v1/market/push?token=" + PUSH_TOKEN
PUSH_URL_IP   = "https://106.53.99.74/api/v1/market/push?token=" + PUSH_TOKEN
PUSH_HOST_HDR = "kinguizi.top"
POLL       = 1.0      # 轮询间隔(秒)
STALE_MAX  = 30       # 连续 N 次时间不变 -> 判定休市, 暂停推送
SPREAD     = 0.30     # 合成卖价点差(美元/盎司); 真实黄金零售点差约 0.3
# 重要: 新浪 hf_XAU 的第2字段(p[1])是失真卖价(常卡在高位不动, 如 4601),
# 绝不能当真实 ask 用; 现价直接取买价 p[0](=真实现货伦敦金), 卖价用 bid+SPREAD 合成。

# ---- 日志: 控制台 + 文件(1MB 轮转, 保留 3 份) ----
LOG_FILE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "sina_relay.log")
_logger = logging.getLogger("sina_relay")
_logger.setLevel(logging.INFO)
_fh = logging.handlers.RotatingFileHandler(LOG_FILE, maxBytes=1_000_000, backupCount=3, encoding="utf-8")
_fh.setFormatter(logging.Formatter("%(asctime)s %(message)s"))
_logger.addHandler(_fh)
_ch = logging.StreamHandler()
_ch.setFormatter(logging.Formatter("%(asctime)s %(message)s"))
_logger.addHandler(_ch)

def now():
    return datetime.datetime.now().strftime("%H:%M:%S")

def fetch_sina():
    last = None
    for attempt in range(3):
        try:
            req = urllib.request.Request(SINA_URL, headers={
                "Referer": REFERER,
                "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"
            })
            with urllib.request.urlopen(req, timeout=8) as r:
                text = r.read().decode("gbk", errors="ignore")
            s = text.split('"')[1]
            p = s.split(",")
            return {
                "bid":  float(p[0]),
                # 新浪 p[1] 是失真卖价(卡死不动), 用 bid + 小点差合成真实感卖价
                "ask":  round(float(p[0]) + SPREAD, 2),
                "open": float(p[2]),
                # high/low 以当前价(买价)为基准, 由后端按 tick 聚合,
                # 避免新浪 p[4]/p[5] 失真字段(如 high 4600+)污染 K 线
                "high": float(p[0]),
                "low":  float(p[0]),
                "time": p[6],
            }
        except Exception as e:
            last = e
            time.sleep(0.3)
    raise last

def push(q):
    # 现价 = 买价(真实现货伦敦金), 不用 (bid + 失真 ask)/2
    price = round(q["bid"], 2)
    payload = {
        "symbol": "XAU",
        "contract_month": "SPOT",
        "price": price,
        "bid": q["bid"],
        "ask": q["ask"],
        "open": q["open"],
        "high": q["high"],
        "low": q["low"],
        "volume": 0,
        "timestamp": int(time.time() * 1000),
    }
    data = json.dumps(payload).encode()
    last = None
    # (url, host_header_or_None, ssl_ctx_or_None) — 域名优先, IP+Host 兜底
    attempts = [
        (PUSH_URL_HOST, None, None),
        (PUSH_URL_IP, PUSH_HOST_HDR, ssl._create_unverified_context()),
    ]
    for attempt in range(4):
        url, host, ctx = attempts[attempt % 2]
        try:
            req = urllib.request.Request(
                url, data=data,
                headers={"Content-Type": "application/json"}, method="POST")
            if host:
                req.add_header("Host", host)
            with urllib.request.urlopen(req, timeout=8, context=ctx) as r:
                return r.read().decode()
        except Exception as e:
            last = e  # 单次失败不刷屏, 仅在所有尝试都失败时才抛出
            time.sleep(0.3)
    raise last

def main():
    _logger.info(f"Sina gold relay started (poll={POLL}s). Ctrl+C to stop.")
    last_t = None
    stale = 0
    while True:
        try:
            q = fetch_sina()
            if q["time"] == last_t:
                stale += 1
            else:
                stale = 0
                last_t = q["time"]
            if stale > STALE_MAX:
                _logger.info(f"market idle (no tick x{stale}), pausing push 60s")
                time.sleep(60)
                continue
            resp = push(q)
            _logger.info(f"XAU bid={q['bid']} ask={q['ask']} -> {resp.strip()[:90]}")
        except Exception as e:
            _logger.error(f"ERR {e}")
        time.sleep(POLL)

if __name__ == "__main__":
    main()
