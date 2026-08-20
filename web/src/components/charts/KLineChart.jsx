import { useEffect, useRef, useState } from 'react'
import { createChart } from 'lightweight-charts'
import { marketAPI } from '../../services/api'
import { wsClient } from '../../services/ws'

// 趋势指标均线配置（周期 / 颜色）
const MA_CONFIG = [
  { period: 5, color: '#FCD34D', label: 'MA5' },
  { period: 20, color: '#C084FC', label: 'MA20' },
  { period: 60, color: '#38BDF8', label: 'MA60' },
  { period: 80, color: '#FB7185', label: 'MA80' },
]

// 计算某周期的均线序列（基于收盘价，需按时间升序的 bars）
function computeMA(period, bars) {
  const res = []
  for (let i = 0; i < bars.length; i++) {
    if (i < period - 1) continue
    let sum = 0
    for (let j = 0; j < period; j++) sum += bars[i - j].close
    res.push({ time: bars[i].time, value: sum / period })
  }
  return res
}

// 取最新一根的均线值（用于图例实时显示）
function latestMAValues(bars) {
  const res = {}
  const i = bars.length - 1
  if (i < 0) return res
  for (const cfg of MA_CONFIG) {
    if (i >= cfg.period - 1) {
      let sum = 0
      for (let j = 0; j < cfg.period; j++) sum += bars[i - j].close
      res[cfg.period] = sum / cfg.period
    }
  }
  return res
}

export default function KLineChart({ symbol = 'XAU', period = '1m' }) {
  const containerRef = useRef(null)
  const chartRef = useRef(null)
  const candleSeriesRef = useRef(null)
  const maSeriesRef = useRef({}) // period -> line series
  const barsRef = useRef([]) // 已加载/推送的 K 线（升序）
  const [timeframe, setTimeframe] = useState(period)
  const [maValues, setMaValues] = useState({})

  useEffect(() => {
    if (!containerRef.current) return

    const chart = createChart(containerRef.current, {
      layout: {
        background: { color: '#1A2733' },
        textColor: '#9CA3AF',
      },
      grid: {
        vertLines: { color: '#2D3B4A' },
        horzLines: { color: '#2D3B4A' },
      },
      crosshair: {
        vertLine: { color: '#D4AF37', width: 1, style: 2 },
        horzLine: { color: '#D4AF37', width: 1, style: 2 },
      },
      timeScale: {
        borderColor: '#2D3B4A',
        timeVisible: true,
        secondsVisible: false,
      },
      rightPriceScale: {
        borderColor: '#2D3B4A',
      },
    })

    // 中国习惯：红涨绿跌
    const candleSeries = chart.addCandlestickSeries({
      upColor: '#EF4444',
      downColor: '#22C55E',
      borderDownColor: '#22C55E',
      borderUpColor: '#EF4444',
      wickDownColor: '#22C55E',
      wickUpColor: '#EF4444',
    })
    candleSeriesRef.current = candleSeries

    // 均线叠加线（不显示价格线/最新值标签，避免与蜡烛主图价格轴混淆）
    maSeriesRef.current = {}
    for (const cfg of MA_CONFIG) {
      maSeriesRef.current[cfg.period] = chart.addLineSeries({
        color: cfg.color,
        lineWidth: 1,
        priceLineVisible: false,
        lastValueVisible: false,
        crosshairMarkerVisible: false,
      })
    }

    let disposed = false

    // 加载真实历史 K 线（后端从真实价格 tick 累积生成）
    marketAPI.getKLines(symbol, 'SPOT', timeframe).then(({ data }) => {
      if (disposed) return
      const klines = data.data || []
      const chartData = klines
        .filter((k) => k.timestamp && k.open > 0)
        .map((k) => ({
          time: Math.floor(k.timestamp / 1000),
          open: k.open,
          high: k.high,
          low: k.low,
          close: k.close,
        }))
        .sort((a, b) => a.time - b.time)
      candleSeries.setData(chartData)
      barsRef.current = chartData
      // 计算并绘制各周期均线
      for (const cfg of MA_CONFIG) {
        maSeriesRef.current[cfg.period].setData(computeMA(cfg.period, chartData))
      }
      setMaValues(latestMAValues(chartData))
      chart.timeScale().fitContent()
    }).catch(() => {})

    // 订阅 WebSocket 实时 K 线推送（后端每 1 秒广播当前 forming candle）
    const unsubscribe = wsClient.subscribe(
      { channel: 'kline', symbol, period: timeframe },
      (msg) => {
        if (disposed) return
        const k = msg.data
        if (!k || !k.timestamp || k.open <= 0) return
        const bar = {
          time: Math.floor(k.timestamp / 1000),
          open: k.open,
          high: k.high,
          low: k.low,
          close: k.close,
        }
        try {
          candleSeries.update(bar)
        } catch (e) {
          // 时间戳乱序时忽略（切换周期竞态）
        }
        // 维护本地 bars 数组并重算均线（forming bar 更新或新 bar 追加）
        const bars = barsRef.current
        const last = bars[bars.length - 1]
        if (last && last.time === bar.time) {
          bars[bars.length - 1] = bar
        } else if (!last || bar.time > last.time) {
          bars.push(bar)
        }
        for (const cfg of MA_CONFIG) {
          maSeriesRef.current[cfg.period].setData(computeMA(cfg.period, bars))
        }
        setMaValues(latestMAValues(bars))
      }
    )

    chartRef.current = chart
    chart.timeScale().fitContent()

    const handleResize = () => chart.applyOptions({
      width: containerRef.current.clientWidth,
      height: containerRef.current.clientHeight,
    })
    window.addEventListener('resize', handleResize)

    return () => {
      disposed = true
      unsubscribe()
      chart.remove()
      window.removeEventListener('resize', handleResize)
    }
  }, [symbol, timeframe])

  const periods = ['1m', '5m', '15m', '30m', '1h', '4h', '1d']

  return (
    <div className="trade-card p-3">
      <div className="flex items-center justify-between mb-2">
        <h3 className="text-sm font-semibold text-gray-300">XAU 伦敦金现货 · LBM（真实行情）</h3>
        <div className="flex gap-1">
          {periods.map((p) => (
            <button
              key={p}
              onClick={() => setTimeframe(p)}
              className={`px-2 py-1 text-xs rounded ${
                timeframe === p
                  ? 'bg-gold text-dark font-semibold'
                  : 'text-gray-500 hover:text-gray-300'
              }`}
            >
              {p}
            </button>
          ))}
        </div>
      </div>
      {/* 趋势指标均线图例（实时值） */}
      <div className="flex items-center gap-3 mb-2 text-xs">
        {MA_CONFIG.map((cfg) => (
          <span key={cfg.period} className="flex items-center gap-1 font-mono">
            <span style={{ color: cfg.color }}>●</span>
            <span style={{ color: cfg.color }}>{cfg.label}</span>
            <span className="text-gray-400">
              {maValues[cfg.period] ? maValues[cfg.period].toFixed(2) : '--'}
            </span>
          </span>
        ))}
      </div>
      <div ref={containerRef} className="w-full" style={{ height: '420px' }} />
    </div>
  )
}
