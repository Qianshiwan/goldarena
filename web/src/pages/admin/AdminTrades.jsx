import { useEffect, useState } from 'react'
import { adminAPI } from '../../services/api'

function fmt(n) {
  return (n ?? 0).toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

export default function AdminTrades() {
  const [tab, setTab] = useState('positions')
  const [positions, setPositions] = useState([])
  const [orders, setOrders] = useState([])

  const loadPositions = async () => {
    try {
      const { data } = await adminAPI.listPositions()
      setPositions(data.data.list || [])
    } catch {}
  }
  const loadOrders = async () => {
    try {
      const { data } = await adminAPI.listOrders({ page: 1, size: 50 })
      setOrders(data.data.list || [])
    } catch {}
  }

  useEffect(() => {
    loadPositions()
    const id = setInterval(loadPositions, 5000)
    return () => clearInterval(id)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const onForceClose = async (id) => {
    if (!confirm('确认强制平仓该持仓？')) return
    try {
      await adminAPI.forceClose(id)
      alert('已强制平仓')
      loadPositions()
    } catch (e) {
      alert('失败: ' + (e.response?.data?.message || '未知错误'))
    }
  }

  const dirText = (d) => (d === 1 ? '做多' : '做空')

  return (
    <div>
      <h2 className="text-lg font-semibold text-gray-200 mb-4">交易监控</h2>
      <div className="flex gap-2 mb-4">
        <button
          onClick={() => setTab('positions')}
          className={`px-4 py-1.5 rounded text-sm ${tab === 'positions' ? 'bg-gold/15 text-gold' : 'text-gray-400 hover:text-gray-200'}`}
        >
          持仓 ({positions.length})
        </button>
        <button
          onClick={() => {
            setTab('orders')
            loadOrders()
          }}
          className={`px-4 py-1.5 rounded text-sm ${tab === 'orders' ? 'bg-gold/15 text-gold' : 'text-gray-400 hover:text-gray-200'}`}
        >
          订单
        </button>
      </div>

      {tab === 'positions' ? (
        <div className="trade-card overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-gray-500 text-xs border-b border-gray-800">
                <th className="text-left p-3">ID</th>
                <th className="text-left p-3">用户</th>
                <th className="text-left p-3">品种</th>
                <th className="text-left p-3">方向</th>
                <th className="text-right p-3">手数</th>
                <th className="text-right p-3">杠杆</th>
                <th className="text-right p-3">开仓价</th>
                <th className="text-right p-3">现价</th>
                <th className="text-right p-3">保证金</th>
                <th className="text-right p-3">浮动盈亏</th>
                <th className="text-center p-3">操作</th>
              </tr>
            </thead>
            <tbody>
              {positions.map((p) => (
                <tr key={p.id} className="border-b border-gray-800/50 hover:bg-dark-200/50">
                  <td className="p-3 text-gray-500">{p.id}</td>
                  <td className="p-3 text-gray-300">{p.nickname || p.user_id}</td>
                  <td className="p-3 font-mono text-gray-300">
                    {p.symbol}
                    {p.contract_month ? <span className="text-xs text-gray-600 ml-1">{p.contract_month}</span> : null}
                  </td>
                  <td className="p-3">
                    <span className={p.direction === 1 ? 'text-green-400' : 'text-red-400'}>{dirText(p.direction)}</span>
                  </td>
                  <td className="p-3 text-right font-mono text-gray-300">{p.volume}</td>
                  <td className="p-3 text-right text-gray-400">{p.leverage}x</td>
                  <td className="p-3 text-right font-mono text-gray-300">{fmt(p.open_price)}</td>
                  <td className="p-3 text-right font-mono text-gold">{fmt(p.current_price)}</td>
                  <td className="p-3 text-right font-mono text-gray-300">{fmt(p.margin)}</td>
                  <td className={`p-3 text-right font-mono ${p.floating_pnl >= 0 ? 'text-green-400' : 'text-red-400'}`}>
                    {(p.floating_pnl >= 0 ? '+' : '') + fmt(p.floating_pnl)}
                  </td>
                  <td className="p-3 text-center">
                    <button
                      onClick={() => onForceClose(p.id)}
                      className="text-xs text-red-400 hover:text-red-300"
                    >
                      强制平仓
                    </button>
                  </td>
                </tr>
              ))}
              {positions.length === 0 && (
                <tr>
                  <td colSpan={11} className="p-8 text-center text-gray-500 text-sm">
                    当前无持仓
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="trade-card overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-gray-500 text-xs border-b border-gray-800">
                <th className="text-left p-3">ID</th>
                <th className="text-left p-3">用户</th>
                <th className="text-left p-3">订单号</th>
                <th className="text-left p-3">品种</th>
                <th className="text-left p-3">方向</th>
                <th className="text-right p-3">手数</th>
                <th className="text-right p-3">杠杆</th>
                <th className="text-center p-3">类型</th>
                <th className="text-center p-3">状态</th>
                <th className="text-right p-3">成交价</th>
                <th className="text-left p-3">时间</th>
              </tr>
            </thead>
            <tbody>
              {orders.map((o) => (
                <tr key={o.id} className="border-b border-gray-800/50 hover:bg-dark-200/50">
                  <td className="p-3 text-gray-500">{o.id}</td>
                  <td className="p-3 text-gray-300">{o.user_id}</td>
                  <td className="p-3 font-mono text-gray-400 text-xs">{o.order_no}</td>
                  <td className="p-3 font-mono text-gray-300">{o.symbol}</td>
                  <td className="p-3">
                    <span className={o.direction === 1 ? 'text-green-400' : 'text-red-400'}>{dirText(o.direction)}</span>
                  </td>
                  <td className="p-3 text-right font-mono text-gray-300">{o.volume}</td>
                  <td className="p-3 text-right text-gray-400">{o.leverage}x</td>
                  <td className="p-3 text-center text-gray-400">
                    {o.order_type === 1 ? '市价' : o.order_type === 2 ? '限价' : o.order_type}
                  </td>
                  <td className="p-3 text-center text-gray-400">
                    {o.status === 2 ? '已成交' : o.status === 1 ? '待成交' : o.status === 3 ? '已撤' : o.status === 4 ? '已拒' : o.status}
                  </td>
                  <td className="p-3 text-right font-mono text-gray-300">{o.executed_price != null ? fmt(o.executed_price) : '-'}</td>
                  <td className="p-3 text-gray-500 text-xs">{new Date(o.created_at).toLocaleString()}</td>
                </tr>
              ))}
              {orders.length === 0 && (
                <tr>
                  <td colSpan={11} className="p-8 text-center text-gray-500 text-sm">
                    暂无订单
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
