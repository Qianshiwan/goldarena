import { useEffect, useMemo, useState } from 'react'
import { tradeAPI } from '../services/api'

const fmt = (n) => (n ?? 0).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const pct = (n) => `${(n * 100).toFixed(2)}%`

function StatCard({ label, value, accent }) {
  const color = accent === 'up' ? 'text-green-400' : accent === 'down' ? 'text-red-400' : 'text-gray-100'
  return (
    <div className="bg-dark-card rounded-2xl border border-gray-800 p-5">
      <div className="text-xs text-gray-500 mb-1">{label}</div>
      <div className={`text-xl font-mono font-bold ${color}`}>{value}</div>
    </div>
  )
}

function Empty({ text }) {
  return <div className="p-8 text-center text-gray-500 text-sm">{text}</div>
}

// 累计盈亏曲线：按平仓时间升序累积 pnl，纯 SVG 绘制
function EquityCurve({ trades }) {
  const sorted = [...trades]
    .filter((t) => t.closed_at || t.created_at)
    .sort((a, b) => new Date(a.closed_at || a.created_at) - new Date(b.closed_at || b.created_at))
  let cum = 0
  const pts = sorted.map((t) => {
    cum += t.pnl || 0
    return cum
  })
  if (!pts.length) return <Empty text="暂无已平仓记录，平仓后将自动绘制盈亏曲线" />
  const w = 640, h = 220, pad = 16
  const max = Math.max(...pts, 0), min = Math.min(...pts, 0)
  const range = max - min || 1
  const xStep = pts.length > 1 ? (w - 2 * pad) / (pts.length - 1) : 0
  const yOf = (v) => h - pad - ((v - min) / range) * (h - 2 * pad)
  const linePts = pts.map((v, i) => `${pad + i * xStep},${yOf(v)}`).join(' ')
  const areaPts = `${pad},${h - pad} ${linePts} ${pad + (pts.length - 1) * xStep},${h - pad}`
  const color = cum >= 0 ? '#22c55e' : '#ef4444'
  return (
    <svg viewBox={`0 0 ${w} ${h}`} className="w-full" style={{ height: 220 }} preserveAspectRatio="none">
      <polygon points={areaPts} fill={color} fillOpacity="0.12" />
      <polyline points={linePts} fill="none" stroke={color} strokeWidth="2" />
      <line x1={pad} y1={yOf(0)} x2={w - pad} y2={yOf(0)} stroke="#4b5563" strokeWidth="1" strokeDasharray="4 4" />
      <text x={pad} y={Math.max(yOf(max) - 4, 12)} fill={color} fontSize="11">高点 {fmt(max)}</text>
      <text x={pad} y={Math.min(yOf(min) + 14, h - 4)} fill={color} fontSize="11">低点 {fmt(min)}</text>
      <text x={w - pad} y={h - 4} fill="#9ca3af" fontSize="11" textAnchor="end">
        累计 {cum >= 0 ? '+' : ''}{fmt(cum)}
      </text>
    </svg>
  )
}

// 每日盈亏汇总（复用 WalletPage 的聚合逻辑）
function DailyPnL({ trades }) {
  const dailyMap = {}
  trades.forEach((t) => {
    const d = t.closed_at ? new Date(t.closed_at) : t.created_at ? new Date(t.created_at) : null
    if (!d) return
    const key = d.toLocaleDateString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' })
    if (!dailyMap[key]) dailyMap[key] = { pnl: 0, count: 0 }
    dailyMap[key].pnl += t.pnl || 0
    dailyMap[key].count += 1
  })
  const days = Object.entries(dailyMap).sort(
    (a, b) => new Date(b[0].replace(/\//g, '-')) - new Date(a[0].replace(/\//g, '-'))
  )
  const totalPnL = days.reduce((s, [, d]) => s + d.pnl, 0)
  const winDays = days.filter(([, d]) => d.pnl > 0).length
  const lossDays = days.filter(([, d]) => d.pnl < 0).length
  const maxAbs = Math.max(...days.map(([, d]) => Math.abs(d.pnl)), 1)
  if (!days.length) {
    return (
      <div className="trade-card p-5">
        <h3 className="text-sm font-semibold text-gray-300 mb-3">每日盈亏</h3>
        <Empty text="暂无交易记录" />
      </div>
    )
  }
  return (
    <div className="trade-card p-5">
      <div className="flex items-center justify-between mb-3">
        <h3 className="text-sm font-semibold text-gray-300">每日盈亏</h3>
        <div className="flex gap-3 text-xs">
          <span className="text-gray-500">交易 {days.length} 天</span>
          <span className="text-green-400">盈利 {winDays} 天</span>
          <span className="text-red-400">亏损 {lossDays} 天</span>
          <span className={`font-mono font-bold ${totalPnL >= 0 ? 'text-green-400' : 'text-red-400'}`}>
            合计 {totalPnL >= 0 ? '+' : ''}{fmt(totalPnL)}
          </span>
        </div>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-gray-500 text-xs border-b border-gray-800">
              <th className="text-left p-2">日期</th>
              <th className="text-right p-2">笔数</th>
              <th className="text-right p-2">盈亏</th>
              <th className="text-left p-2">分布</th>
            </tr>
          </thead>
          <tbody>
            {days.map(([date, d]) => {
              const isWin = d.pnl > 0
              const barPct = (Math.abs(d.pnl) / maxAbs) * 100
              return (
                <tr key={date} className="border-b border-gray-800/50">
                  <td className="p-2 text-gray-300 text-sm">{date}</td>
                  <td className="text-right p-2 font-mono text-gray-400">{d.count}</td>
                  <td className={`text-right p-2 font-mono font-bold ${isWin ? 'text-green-400' : d.pnl < 0 ? 'text-red-400' : 'text-gray-500'}`}>
                    {isWin ? '+' : ''}{fmt(d.pnl)}
                  </td>
                  <td className="p-2 w-40">
                    <div className="h-2.5 bg-gray-700/50 rounded-full overflow-hidden flex">
                      {d.pnl > 0 && <div className="h-full bg-green-500 rounded-l-full" style={{ width: `${barPct}%` }} />}
                      {d.pnl < 0 && <div className="h-full bg-red-500 rounded-r-full ml-auto" style={{ width: `${barPct}%` }} />}
                      {d.pnl === 0 && <div className="h-full bg-gray-500 w-2 mx-auto rounded-full" />}
                    </div>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function Distribution({ wins, losses, flats, total }) {
  if (!total) return <Empty text="暂无交易记录" />
  const rows = [
    { label: '盈利', n: wins, color: 'bg-green-500' },
    { label: '亏损', n: losses, color: 'bg-red-500' },
    { label: '持平', n: flats, color: 'bg-gray-500' },
  ]
  return (
    <div className="space-y-3">
      {rows.map((r) => {
        const p = (r.n / total) * 100
        return (
          <div key={r.label} className="flex items-center gap-3">
            <span className="w-10 text-xs text-gray-400">{r.label}</span>
            <div className="flex-1 h-3 bg-gray-700/50 rounded-full overflow-hidden">
              <div className={`h-full ${r.color}`} style={{ width: `${p}%` }} />
            </div>
            <span className="w-24 text-right text-xs font-mono text-gray-300">
              {r.n} 笔 ({p.toFixed(1)}%)
            </span>
          </div>
        )
      })}
    </div>
  )
}

export default function ProfitStatsPage() {
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    tradeAPI
      .getPnL()
      .then(({ data: res }) => setData(res.data))
      .catch(() => setData({ total_pnl: 0, trades: [] }))
      .finally(() => setLoading(false))
  }, [])

  const trades = data?.trades || []
  const stats = useMemo(() => {
    const total = trades.length
    const wins = trades.filter((t) => (t.pnl || 0) > 0).length
    const losses = trades.filter((t) => (t.pnl || 0) < 0).length
    const flats = total - wins - losses
    const totalPnL = trades.reduce((s, t) => s + (t.pnl || 0), 0)
    const winRate = total ? wins / total : 0
    const avg = total ? totalPnL / total : 0
    return { total, wins, losses, flats, totalPnL, winRate, avg }
  }, [trades])

  if (loading) return <div className="max-w-4xl mx-auto p-10 text-center text-gray-500">加载中…</div>

  return (
    <div className="max-w-4xl mx-auto space-y-6">
      <h1 className="text-xl font-bold gold-gradient">盈亏统计</h1>

      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <StatCard label="交易笔数" value={stats.total} />
        <StatCard label="胜率" value={pct(stats.winRate)} accent={stats.winRate >= 0.5 ? 'up' : 'down'} />
        <StatCard label="总盈亏" value={`${stats.totalPnL >= 0 ? '+' : ''}${fmt(stats.totalPnL)}`} accent={stats.totalPnL >= 0 ? 'up' : 'down'} />
        <StatCard label="平均盈亏" value={`${stats.avg >= 0 ? '+' : ''}${fmt(stats.avg)}`} accent={stats.avg >= 0 ? 'up' : 'down'} />
      </div>

      <div className="trade-card p-5">
        <h3 className="text-sm font-semibold text-gray-300 mb-3">累计盈亏曲线（按平仓时间）</h3>
        <EquityCurve trades={trades} />
      </div>

      <DailyPnL trades={trades} />

      <div className="trade-card p-5">
        <h3 className="text-sm font-semibold text-gray-300 mb-3">盈亏分布</h3>
        <Distribution wins={stats.wins} losses={stats.losses} flats={stats.flats} total={stats.total} />
      </div>
    </div>
  )
}
