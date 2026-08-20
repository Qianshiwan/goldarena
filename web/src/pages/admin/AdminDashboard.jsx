import { useEffect, useState } from 'react'
import { adminAPI } from '../../services/api'

function fmt(n) {
  return (n ?? 0).toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

function Stat({ label, value, sub, color }) {
  return (
    <div className="trade-card p-4">
      <div className="text-xs text-gray-500">{label}</div>
      <div className={`text-2xl font-bold mt-1 ${color || 'text-gray-100'}`}>{value}</div>
      {sub && <div className="text-xs text-gray-500 mt-1">{sub}</div>}
    </div>
  )
}

export default function AdminDashboard() {
  const [d, setD] = useState(null)

  const load = async () => {
    try {
      const { data } = await adminAPI.dashboard()
      setD(data.data)
    } catch {}
  }

  useEffect(() => {
    load()
    const id = setInterval(load, 5000)
    return () => clearInterval(id)
  }, [])

  if (!d) return <div className="text-gray-500 text-sm">加载中...</div>

  return (
    <div>
      <h2 className="text-lg font-semibold text-gray-200 mb-4">平台概览</h2>
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <Stat label="总用户数" value={d.total_users} sub={`今日新增 ${d.today_new_users}`} />
        <Stat label="流通游戏币" value={fmt(d.total_balance)} sub="可用余额合计" />
        <Stat label="当前金价" value={`$${fmt(d.current_price)}`} color="text-gold" />
        <Stat label="持仓数" value={d.open_positions} sub={`保证金 ${fmt(d.total_margin)}`} />
        <Stat
          label="总浮动盈亏"
          value={fmt(d.total_floating_pnl)}
          color={d.total_floating_pnl >= 0 ? 'text-green-400' : 'text-red-400'}
        />
        <Stat
          label="待处理支付"
          value={d.pending_payments}
          color={d.pending_payments > 0 ? 'text-yellow-400' : 'text-gray-100'}
        />
        <Stat label="累计充值 (¥)" value={fmt(d.total_recharge_rmb)} />
        <Stat label="今日充值 (¥)" value={fmt(d.today_recharge_rmb)} color="text-green-400" />
      </div>
      <p className="text-xs text-gray-600 mt-4">数据每 5 秒自动刷新。</p>
    </div>
  )
}
