import QuoteBar from '../components/trading/QuoteBar'
import KLineChart from '../components/charts/KLineChart'
import OrderPanel from '../components/trading/OrderPanel'
import PendingOrdersList from '../components/trading/PendingOrdersList'
import PositionList from '../components/trading/PositionList'

export default function TradePage() {
  return (
    <div className="space-y-3">
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
          <OrderPanel />
        </div>
      </div>

      {/* Pending Orders List (挂单列表) */}
      <PendingOrdersList />

      {/* Position List */}
      <PositionList />
    </div>
  )
}
