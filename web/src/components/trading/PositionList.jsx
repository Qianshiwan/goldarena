import { useEffect, useState } from 'react'
import { tradeAPI } from '../../services/api'
import { wsClient } from '../../services/ws'

// P&L 公式与后端 trade_handler.calculatePnL 保持一致:
//   contractSize = 100; 多: (现价-开仓)*100*volume; 空: (开仓-现价)*100*volume
const CONTRACT_SIZE = 100
function calcPnl(position, currentPrice) {
  const diff =
    position.direction === 1
      ? currentPrice - position.open_price
      : position.open_price - currentPrice
  return diff * CONTRACT_SIZE * position.volume
}

export default function PositionList({ contestId = null }) {
  const [positions, setPositions] = useState([])
  const [loading, setLoading] = useState(true)
  const [livePrice, setLivePrice] = useState(null)

  useEffect(() => {
    fetchPositions()
    const id = setInterval(fetchPositions, 5000)

    const unsub = wsClient.subscribe(
      { channel: 'quote', symbol: 'XAU' },
      (msg) => {
        const price = msg?.data?.price
        if (typeof price === 'number' && price > 0) setLivePrice(price)
      }
    )

    // 订阅交易事件（挂单成交/止损触发自动刷新）
    const unsubTrade = wsClient.subscribe(
      { channel: 'trade', user_id: 'self' },
      () => { fetchPositions() }
    )

    return () => {
      clearInterval(id)
      unsub()
      if (unsubTrade) unsubTrade()
    }
  }, [contestId])

  const fetchPositions = async () => {
    try {
      const { data } = await tradeAPI.getPositions(contestId)
      if (data.data) setPositions(data.data)
    } catch {}
    setLoading(false)
  }

  const handleClose = async (positionId) => {
    if (!confirm('确定要平仓吗?')) return
    try {
      await tradeAPI.closePosition(positionId)
      fetchPositions()
    } catch (err) {
      alert('平仓失败: ' + (err.response?.data?.message || '未知错误'))
    }
  }

  // 修改止盈止损
  const handleUpdateSLTP = async (positionId, currentSL, currentTP) => {
    const sl = prompt('输入止损价 (留空不变):', currentSL != null ? String(currentSL) : '')
    const tp = prompt('输入止盈价 (留空不变):', currentTP != null ? String(currentTP) : '')
    if (!sl && !tp) return

    const payload = {}
    if (sl !== '' && sl !== null) payload.stop_loss = parseFloat(sl)
    if (tp !== '' && tp !== null) payload.take_profit = parseFloat(tp)

    try {
      await tradeAPI.updateSLTP(positionId, payload)
      fetchPositions()
      alert('止盈止损已更新')
    } catch (err) {
      alert('更新失败: ' + (err.response?.data?.message || '未知错误'))
    }
  }

  // 用实时金价覆盖现价并重算浮盈
  const shown = positions.map((p) => {
    if (livePrice != null && p.symbol === 'XAU' && p.contract_month === 'SPOT') {
      return {
        ...p,
        current_price: livePrice,
        floating_pnl: calcPnl(p, livePrice),
      }
    }
    return p
  })

  // Calculate totals
  const totalPnl = shown.reduce((sum, p) => sum + (p.floating_pnl || 0), 0)
  const totalMargin = shown.reduce((sum, p) => sum + (p.margin || 0), 0)

  return (
    <div className="trade-card">
      <div className="p-3 border-b border-gray-800 flex items-center justify-between">
        <h3 className="text-sm font-semibold text-gray-300">持仓列表</h3>
        <div className="flex gap-4 text-xs">
          <span className="text-gray-500">
            保证金: <span className="font-mono text-gray-300">{totalMargin.toFixed(2)}</span>
          </span>
          <span className={totalPnl >= 0 ? 'price-up' : 'price-down'}>
            浮动盈亏: <span className="font-mono">{totalPnl >= 0 ? '+' : ''}{totalPnl.toFixed(2)}</span>
          </span>
        </div>
      </div>

      {loading ? (
        <div className="p-8 text-center text-gray-500 text-sm">加载中...</div>
      ) : positions.length === 0 ? (
        <div className="p-8 text-center text-gray-500 text-sm">
          暂无持仓，快去下单吧!
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-gray-500 text-xs border-b border-gray-800">
                <th className="text-left p-3">品种</th>
                <th className="text-left p-3">方向</th>
                <th className="text-right p-3">手数</th>
                <th className="text-right p-3">杠杆</th>
                <th className="text-right p-3">开仓价</th>
                <th className="text-right p-3">现价</th>
                <th className="text-right p-3">止损</th>
                <th className="text-right p-3">止盈</th>
                <th className="text-right p-3">浮动盈亏</th>
                <th className="text-center p-3">操作</th>
              </tr>
            </thead>
            <tbody>
              {shown.map((p) => {
                const pnlColor = (p.floating_pnl || 0) >= 0 ? 'price-up' : 'price-down'
                return (
                  <tr key={p.id} className="border-b border-gray-800/50 hover:bg-dark-200/50">
                    <td className="p-3">
                      <span className="font-mono text-gray-300">{p.symbol}</span>
                      {p.contract_month && (
                        <span className="text-xs text-gray-600 ml-1">{p.contract_month}</span>
                      )}
                    </td>
                    <td className="p-3">
                      <span className={
                        p.direction === 1
                          ? 'px-2 py-0.5 rounded text-xs font-bold bg-green-900/30 text-green-400'
                          : 'px-2 py-0.5 rounded text-xs font-bold bg-red-900/30 text-red-400'
                      }>
                        {p.direction === 1 ? '做多' : '做空'}
                      </span>
                    </td>
                    <td className="text-right p-3 font-mono text-gray-300">{p.volume}</td>
                    <td className="text-right p-3 text-gray-400">{p.leverage}x</td>
                    <td className="text-right p-3 font-mono text-gray-300">${p.open_price?.toFixed(2)}</td>
                    <td className="text-right p-3 font-mono text-gray-300">${p.current_price?.toFixed(2)}</td>
                    <td className="text-right p-3 font-mono text-xs text-red-400">
                      {p.stop_loss != null ? `$${p.stop_loss.toFixed(2)}` : <span className="text-gray-600">—</span>}
                    </td>
                    <td className="text-right p-3 font-mono text-xs text-green-400">
                      {p.take_profit != null ? `$${p.take_profit.toFixed(2)}` : <span className="text-gray-600">—</span>}
                    </td>
                    <td className={`text-right p-3 font-mono ${pnlColor}`}>
                      {(p.floating_pnl || 0) >= 0 ? '+' : ''}{(p.floating_pnl || 0).toFixed(2)}
                    </td>
                    <td className="text-center p-3">
                      <div className="flex gap-1 justify-center">
                        <button
                          onClick={() => handleUpdateSLTP(p.id, p.stop_loss, p.take_profit)}
                          className="px-2 py-1 text-xs rounded bg-blue-500/20 text-blue-400 hover:bg-blue-500/40 transition-colors"
                          title="修改止盈止损"
                        >
                          SL/TP
                        </button>
                        <button
                          onClick={() => handleClose(p.id)}
                          className="px-2 py-1 text-xs rounded bg-red-500/20 text-red-400 hover:bg-red-500/40 transition-colors"
                        >
                          平仓
                        </button>
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
