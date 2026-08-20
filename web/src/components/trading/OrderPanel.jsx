import { useState, useEffect, useCallback } from 'react'
import { tradeAPI, marketAPI } from '../../services/api'
import { wsClient } from '../../services/ws'

const ORDER_TYPES = [
  { value: 1, label: '市价单', desc: '以当前市价立即成交' },
  { value: 2, label: '限价单', desc: '达到指定价格时买入/卖出' },
  { value: 3, label: '止损单', desc: '突破指定价格时追涨/杀跌' },
]

export default function OrderPanel() {
  const [volume, setVolume] = useState(0.05)
  const [leverage, setLeverage] = useState(10)
  const [livePrice, setLivePrice] = useState(null)
  const [positions, setPositions] = useState([])
  const [loading, setLoading] = useState(false)
  const [closing, setClosing] = useState(false)
  const [result, setResult] = useState(null)

  // 挂单相关状态
  const [orderType, setOrderType] = useState(1) // 1=市价 2=限价 3=止损
  const [triggerPrice, setTriggerPrice] = useState('')
  const [stopLoss, setStopLoss] = useState('')
  const [takeProfit, setTakeProfit] = useState('')

  // 拉取当前 XAU 持仓
  const refreshPositions = useCallback(() => {
    tradeAPI.getPositions().then(({ data }) => {
      const list = (data.data || []).filter(
        (p) => p.symbol === 'XAU' && p.contract_month === 'SPOT' && p.status === 1
      )
      setPositions(list)
    }).catch(() => {})
  }, [])

  // 实时金价
  useEffect(() => {
    marketAPI.getQuote().then(({ data }) => {
      if (data.data?.price) setLivePrice(data.data.price)
    }).catch(() => {})
    const unsub = wsClient.subscribe(
      { channel: 'quote', symbol: 'XAU' },
      (msg) => { if (msg.data?.price) setLivePrice(msg.data.price) }
    )
    refreshPositions()
    const timer = setInterval(refreshPositions, 4000)
    return () => { unsub(); clearInterval(timer) }
  }, [refreshPositions])

  const estPrice = livePrice || 4400
  const margin = (estPrice * 100 * volume) / leverage

  // 市价单：一键做多/做空（保持原有逻辑）
  const oneClickOrder = async (direction) => {
    setLoading(true)
    setResult(null)
    try {
      const { data } = await tradeAPI.placeOrder({
        symbol: 'XAU',
        contract_month: 'SPOT',
        direction,
        order_type: 1,
        volume,
        leverage,
      })
      setResult({ success: true, ...data.data })
      refreshPositions()
    } catch (err) {
      setResult({ success: false, message: err.response?.data?.message || '下单失败' })
    } finally {
      setLoading(false)
    }
  }

  // 挂单交易（限价/止损）
  const placePendingOrder = async (direction) => {
    const price = parseFloat(triggerPrice)
    if (!price || price <= 0) {
      setResult({ success: false, message: '请输入有效的触发价格' })
      return
    }
    const sl = stopLoss ? parseFloat(stopLoss) : null
    const tp = takeProfit ? parseFloat(takeProfit) : null
    if (sl && tp && sl >= tp) {
      setResult({ success: false, message: '止损价必须低于止盈价' })
      return
    }

    setLoading(true)
    setResult(null)
    try {
      const payload = {
        symbol: 'XAU',
        contract_month: 'SPOT',
        direction,
        order_type: orderType,
        volume,
        leverage,
        price,
      }
      if (sl) payload.stop_loss = sl
      if (tp) payload.take_profit = tp

      const { data } = await tradeAPI.placeOrder(payload)
      setResult({ success: true, ...data.data, message: '挂单成功，等待成交...' })
      // 清空输入
      setTriggerPrice('')
      setStopLoss('')
      setTakeProfit('')
    } catch (err) {
      setResult({ success: false, message: err.response?.data?.message || '挂单失败' })
    } finally {
      setLoading(false)
    }
  }

  // 一键平仓
  const oneClickClose = async () => {
    if (positions.length === 0) return
    setClosing(true)
    setResult(null)
    let okCount = 0
    let lastMsg = ''
    for (const p of positions) {
      try {
        await tradeAPI.closePosition(p.id)
        okCount++
      } catch (err) {
        lastMsg = err.response?.data?.message || '平仓失败'
      }
    }
    setClosing(false)
    if (okCount > 0) {
      setResult({ success: true, message: `已平仓 ${okCount} 笔` })
    } else {
      setResult({ success: false, message: lastMsg || '平仓失败' })
    }
    refreshPositions()
  }

  const hasPosition = positions.length > 0
  const posSummary = hasPosition
    ? positions.map((p) => `${p.direction === 1 ? '多' : '空'} ${p.volume}手 @${p.open_price}`).join('，')
    : '无持仓'

  const isPendingOrder = orderType !== 1

  return (
    <div className="trade-card p-4">
      <div className="flex items-center justify-between mb-3">
        <h3 className="text-sm font-semibold text-gray-300">下单交易</h3>
        <span className="text-xs text-gray-500">
          市价 <span className="font-mono text-gold">${livePrice ? livePrice.toFixed(2) : '—'}</span>
        </span>
      </div>

      {/* 订单类型切换 */}
      <div className="mb-3">
        <label className="text-gray-500 text-xs mb-1 block">订单类型</label>
        <div className="grid grid-cols-3 gap-1">
          {ORDER_TYPES.map((ot) => (
            <button
              key={ot.value}
              onClick={() => setOrderType(ot.value)}
              className={`px-2 py-1.5 rounded text-xs font-medium transition-all ${
                orderType === ot.value
                  ? 'bg-blue-600 text-white'
                  : 'bg-dark-200 text-gray-400 hover:bg-dark-300'
              }`}
              title={ot.desc}
            >
              {ot.label}
            </button>
          ))}
        </div>
      </div>

      {/* 手数 + 杠杆 */}
      <div className="grid grid-cols-2 gap-3 mb-3">
        <div>
          <label className="text-gray-500 text-xs">手数</label>
          <input
            type="number"
            step="0.01"
            min="0.01"
            max="100"
            value={volume}
            onChange={(e) => setVolume(parseFloat(e.target.value) || 0.01)}
            className="w-full"
          />
        </div>
        <div>
          <label className="text-gray-500 text-xs">杠杆</label>
          <select value={leverage} onChange={(e) => setLeverage(parseInt(e.target.value))} className="w-full">
            {[1, 5, 10, 20, 50, 100, 200, 1000].map((l) => (
              <option key={l} value={l}>{l}x</option>
            ))}
          </select>
        </div>
      </div>

      {/* 挂单专用：触发价格 + 止盈止损 */}
      {isPendingOrder && (
        <div className="space-y-2 mb-3 p-2 bg-dark-200 rounded">
          <div>
            <label className="text-gray-500 text-xs">
              触发价格 {orderType === 2 ? '(限价)' : '(止损价)'}
            </label>
            <input
              type="number"
              step="0.01"
              placeholder={livePrice ? livePrice.toFixed(2) : '输入价格'}
              value={triggerPrice}
              onChange={(e) => setTriggerPrice(e.target.value)}
              className="w-full"
            />
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label className="text-gray-500 text-xs">止损价</label>
              <input
                type="number"
                step="0.01"
                placeholder="可选"
                value={stopLoss}
                onChange={(e) => setStopLoss(e.target.value)}
                className="w-full"
              />
            </div>
            <div>
              <label className="text-gray-500 text-xs">止盈价</label>
              <input
                type="number"
                step="0.01"
                placeholder="可选"
                value={takeProfit}
                onChange={(e) => setTakeProfit(e.target.value)}
                className="w-full"
              />
            </div>
          </div>
        </div>
      )}

      {/* 买卖按钮 */}
      {isPendingOrder ? (
        /* 挂单模式：显示"挂单做多"/"挂单做空" */
        <div className="grid grid-cols-2 gap-2 mb-3">
          <button
            onClick={() => placePendingOrder(1)}
            disabled={loading || !triggerPrice}
            className="btn-trade-long py-3 rounded font-bold text-sm transition-all disabled:opacity-60"
          >
            {loading ? '提交中...' : `挂单做多${triggerPrice ? ` @${parseFloat(triggerPrice).toFixed(2)}` : ''}`}
          </button>
          <button
            onClick={() => placePendingOrder(2)}
            disabled={loading || !triggerPrice}
            className="btn-trade-short py-3 rounded font-bold text-sm transition-all disabled:opacity-60"
          >
            {loading ? '提交中...' : `挂单做空${triggerPrice ? ` @${parseFloat(triggerPrice).toFixed(2)}` : ''}`}
          </button>
        </div>
      ) : (
        /* 市价模式：保持原有的一键做多/做空 */
        <div className="grid grid-cols-2 gap-2 mb-3">
          <button
            onClick={() => oneClickOrder(1)}
            disabled={loading}
            className="btn-trade-long py-3 rounded font-bold text-sm transition-all disabled:opacity-60"
          >
            一键做多
          </button>
          <button
            onClick={() => oneClickOrder(2)}
            disabled={loading}
            className="btn-trade-short py-3 rounded font-bold text-sm transition-all disabled:opacity-60"
          >
            一键做空
          </button>
        </div>
      )}

      {/* 保证金预览 */}
      <div className="p-3 bg-dark-200 rounded text-xs space-y-1 mb-3">
        <div className="flex justify-between text-gray-500">
          <span>保证金</span>
          <span className="font-mono">{margin.toFixed(2)} XAU</span>
        </div>
        <div className="flex justify-between text-gray-500 border-t border-gray-700 pt-1 mt-1">
          <span>当前持仓</span>
          <span className="font-mono text-gray-300">{posSummary}</span>
        </div>
      </div>

      {/* 一键平仓 */}
      <button
        onClick={oneClickClose}
        disabled={!hasPosition || closing}
        className={`w-full py-2.5 rounded font-bold text-sm transition-all ${
          hasPosition
            ? 'bg-amber-600 hover:bg-amber-500 text-white'
            : 'bg-dark-200 text-gray-600 border border-gray-700'
        }`}
      >
        {closing ? '平仓中...' : hasPosition ? '一键平仓' : '无持仓可平'}
      </button>

      {/* 结果提示 */}
      {result && (
        <div className={`mt-3 p-2 rounded text-xs ${
          result.success ? 'bg-green-900/40 text-green-400' : 'bg-red-900/40 text-red-400'
        }`}>
          {result.success ? (
            <div>
              <div>✅ {result.message || (result.status === 'pending' ? '挂单成功' : '下单成功')}</div>
              {result.order_no && (
                <>
                  <div className="font-mono mt-1">订单号: {result.order_no}</div>
                  {result.executed_price && (
                    <div className="font-mono">成交价: ${result.executed_price}</div>
                  )}
                  {result.trigger_price && (
                    <div className="font-mono">触发价: ${result.trigger_price}</div>
                  )}
                  {result.position_id && (
                    <div className="font-mono">持仓ID: #{result.position_id}</div>
                  )}
                </>
              )}
            </div>
          ) : (
            <div>❌ {result.message}</div>
          )}
        </div>
      )}
    </div>
  )
}
