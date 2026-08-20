import { useEffect, useState } from 'react'
import { tradeAPI } from '../../services/api'

const ORDER_TYPE_NAMES = { 1: '市价', 2: '限价', 3: '止损价' }
const ORDER_STATUS_NAMES = { 1: '待成交', 2: '已成交', 3: '已撤销', 4: '已拒绝' }

export default function PendingOrdersList() {
  const [orders, setOrders] = useState([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetchOrders()
    // 每5秒刷新一次挂单状态
    const timer = setInterval(fetchOrders, 5000)
    return () => clearInterval(timer)
  }, [])

  const fetchOrders = async () => {
    try {
      const { data } = await tradeAPI.getPendingOrders()
      if (data.data) setOrders(data.data)
    } catch {}
    setLoading(false)
  }

  const handleCancel = async (orderId, orderNo) => {
    if (!confirm(`确定要撤单 ${orderNo} 吗?`)) return
    try {
      await tradeAPI.cancelOrder(orderId)
      fetchOrders()
    } catch (err) {
      alert('撤单失败: ' + (err.response?.data?.message || '未知错误'))
    }
  }

  if (loading) return null

  if (orders.length === 0) return null

  return (
    <div className="trade-card">
      <div className="p-3 border-b border-gray-800 flex items-center justify-between">
        <h3 className="text-sm font-semibold text-gray-300">挂单列表</h3>
        <span className="text-xs text-gray-500">{orders.length} 个待成交</span>
      </div>

      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-gray-500 text-xs border-b border-gray-800">
              <th className="text-left p-3">类型</th>
              <th className="text-left p-3">方向</th>
              <th className="text-right p-3">触发价</th>
              <th className="text-right p-3">手数</th>
              <th className="text-right p-3">杠杆</th>
              <th className="text-right p-3">止损</th>
              <th className="text-right p-3">止盈</th>
              <th className="text-right p-3">保证金</th>
              <th className="text-center p-3">操作</th>
            </tr>
          </thead>
          <tbody>
            {orders.map((o) => (
              <tr key={o.id} className="border-b border-gray-800/50 hover:bg-dark-200/50">
                <td className="p-3">
                  <span className="px-1.5 py-0.5 rounded text-xs font-medium bg-purple-900/30 text-purple-300">
                    {ORDER_TYPE_NAMES[o.order_type] || o.order_type}
                  </span>
                </td>
                <td className="p-3">
                  <span className={
                    o.direction === 1
                      ? 'px-2 py-0.5 rounded text-xs font-bold bg-green-900/30 text-green-400'
                      : 'px-2 py-0.5 rounded text-xs font-bold bg-red-900/30 text-red-400'
                  }>
                    {o.direction === 1 ? '做多' : '做空'}
                  </span>
                </td>
                <td className="text-right p-3 font-mono text-yellow-400">${o.price?.toFixed(2)}</td>
                <td className="text-right p-3 font-mono text-gray-300">{o.volume}</td>
                <td className="text-right p-3 text-gray-400">{o.leverage}x</td>
                <td className="text-right p-3 font-mono text-xs text-red-400">
                  {o.stop_loss != null ? `$${o.stop_loss.toFixed(2)}` : <span className="text-gray-600">—</span>}
                </td>
                <td className="text-right p-3 font-mono text-xs text-green-400">
                  {o.take_profit != null ? `$${o.take_profit.toFixed(2)}` : <span className="text-gray-600">—</span>}
                </td>
                <td className="text-right p-3 font-mono text-gray-400">{o.margin?.toFixed(2)}</td>
                <td className="text-center p-3">
                  <button
                    onClick={() => handleCancel(o.id, o.order_no)}
                    className="px-2 py-1 text-xs rounded bg-orange-500/20 text-orange-400 hover:bg-orange-500/40 transition-colors"
                  >
                    撤单
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
