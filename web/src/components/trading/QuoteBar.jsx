import { useEffect, useState } from 'react'
import { marketAPI } from '../../services/api'
import { wsClient } from '../../services/ws'

export default function QuoteBar() {
  const [quote, setQuote] = useState(null)

  useEffect(() => {
    // 兜底轮询（WebSocket 断开时仍能刷新）
    const fetch = () => {
      marketAPI.getQuote().then(({ data }) => {
        if (data.data) setQuote(data.data)
      }).catch(() => {})
    }
    fetch()
    const id = setInterval(fetch, 10000)
    // WebSocket 实时推送报价
    const unsubscribe = wsClient.subscribe(
      { channel: 'quote', symbol: 'XAU' },
      (msg) => {
        if (msg.data?.price) setQuote(msg.data)
      }
    )
    return () => {
      clearInterval(id)
      unsubscribe()
    }
  }, [])

  if (!quote) return null

  const isUp = quote.change >= 0

  return (
    <div className="trade-card p-3 flex items-center justify-between flex-wrap gap-4">
      {/* Symbol & Price */}
      <div className="flex items-center gap-4">
        <div>
          <div className="flex items-center gap-2">
            <span className="text-sm font-bold text-white">XAU</span>
            <span className="text-xs text-gray-500">LBM {quote.contract_month || 'SPOT'}</span>
          </div>
          <div className="flex items-baseline gap-3 mt-1">
            <span className={`text-2xl font-mono font-bold ${isUp ? 'price-up' : 'price-down'}`}>
              {quote.price?.toFixed(2)}
            </span>
            <span className={`text-sm font-mono ${isUp ? 'price-up' : 'price-down'}`}>
              {isUp ? '+' : ''}{quote.change?.toFixed(2)} ({quote.change_percent?.toFixed(2)}%)
            </span>
          </div>
        </div>
      </div>

      {/* Bid / Ask */}
      <div className="flex gap-6 text-sm">
        <div className="text-right">
          <div className="text-gray-500 text-xs">卖价 Ask</div>
          <div className="font-mono price-down">${quote.ask?.toFixed(2)}</div>
        </div>
        <div className="text-right">
          <div className="text-gray-500 text-xs">买价 Bid</div>
          <div className="font-mono price-up">${quote.bid?.toFixed(2)}</div>
        </div>
      </div>

      {/* Stats */}
      <div className="flex gap-6 text-xs text-gray-400">
        <div>
          <span className="text-gray-500">开盘</span>
          <div className="font-mono text-gray-300">${quote.open?.toFixed(2)}</div>
        </div>
        <div>
          <span className="text-gray-500">最高</span>
          <div className="font-mono price-up">${quote.high?.toFixed(2)}</div>
        </div>
        <div>
          <span className="text-gray-500">最低</span>
          <div className="font-mono price-down">${quote.low?.toFixed(2)}</div>
        </div>
        <div>
          <span className="text-gray-500">成交量</span>
          <div className="font-mono text-gray-300">{quote.volume?.toLocaleString()}</div>
        </div>
      </div>
    </div>
  )
}
