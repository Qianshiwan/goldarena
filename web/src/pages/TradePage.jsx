import QuoteBar from '../components/trading/QuoteBar'
import KLineChart from '../components/charts/KLineChart'
import OrderPanel from '../components/trading/OrderPanel'
import PendingOrdersList from '../components/trading/PendingOrdersList'
import PositionList from '../components/trading/PositionList'

// 交易大厅：使用游戏币钱包（ga_wallets）。contestId=null 让后端走游戏币路由
// 同时前端过滤 contest 持仓/挂单，保证两套钱包完全隔离。
export default function TradePage() {
  return (
    <div className="space-y-3">
      {/* 模式标识横幅 */}
      <div className="trade-card p-3 flex items-center justify-between border-gold/30">
        <div className="flex items-center gap-3">
          <span className="text-2xl">📊</span>
          <div>
            <div className="text-sm font-bold gold-gradient">交易大厅 · 游戏币</div>
            <div className="text-xs text-gray-500">
              保证金 / 盈亏 / 结算均使用「游戏币」，与「金龟子模拟币」完全隔离
            </div>
          </div>
        </div>
        <a
          href="/contest-trade"
          className="text-xs px-3 py-1.5 rounded border border-gold/40 text-gold hover:bg-gold/10 transition-colors"
        >
          切换到 选拔赛交易 →
        </a>
      </div>

      {/* Quote Bar */}
      <QuoteBar />

      {/* Main Grid: Chart + Order Panel */}
      <div className="grid grid-cols-1 xl:grid-cols-4 gap-3">
        {/* Chart takes 3/4 */}
        <div className="xl:col-span-3">
          <KLineChart symbol="XAU" period="1m" />
        </div>
        {/* Order Panel takes 1/4 */}
        <div>
          <OrderPanel contestId={null} />
        </div>
      </div>

      {/* Pending Orders List (挂单列表) — 仅游戏币挂单 */}
      <PendingOrdersList contestId={null} />

      {/* Position List — 仅游戏币持仓 */}
      <PositionList contestId={null} />
    </div>
  )
}