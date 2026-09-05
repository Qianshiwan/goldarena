import { useEffect, useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import QuoteBar from '../components/trading/QuoteBar'
import KLineChart from '../components/charts/KLineChart'
import OrderPanel from '../components/trading/OrderPanel'
import PendingOrdersList from '../components/trading/PendingOrdersList'
import PositionList from '../components/trading/PositionList'
import { jinguiziAPI } from '../services/api'
import useAuthStore from '../stores/authStore'

const tierLabel = { small: '小账户 (100万)', medium: '中账户 (500万)', large: '大账户 (1000万)' }
const statusLabel = { active: '参赛中', settled: '已结算', eliminated: '已淘汰' }

// 阶段盈利门槛 — 与后端 jinguiziStageTargets 对应
const stageDefs = [
  { months: 1, pct: 0.01, label: '1月≥1%' },
  { months: 3, pct: 0.10, label: '3月≥10%' },
  { months: 6, pct: 0.29, label: '6月≥29%' },
]

const fmt = (n) => (n ?? 0).toLocaleString('zh-CN', { minimumFractionDigits: 0, maximumFractionDigits: 2 })

// 选拔赛交易大厅 — 使用「金龟子币」钱包（ga_jinguizi_wallets）
// 与交易大厅（游戏币）UI 完全一致但所有盈亏/结算走金龟子钱包；
// 顶部展示实时权益、回撤警戒线（≥5%/≥6% 红色高亮）和阶段进度。
export default function ContestTradePage() {
  const { user } = useAuthStore()
  const navigate = useNavigate()
  const contestId = user?.id

  const [wallet, setWallet] = useState(null)
  const [enrollment, setEnrollment] = useState(null)
  const [equity, setEquity] = useState(null)
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(async () => {
    try {
      const [wRes, eRes] = await Promise.all([
        jinguiziAPI.getWallet(),
        jinguiziAPI.getEnrollment(),
      ])
      if (wRes.data.data) setWallet(wRes.data.data)
      setEnrollment(eRes.data.data?.enrollment || null)
      setEquity(eRes.data.data?.equity || null)
    } catch {}
    setLoading(false)
  }, [])

  useEffect(() => {
    refresh()
    const id = setInterval(refresh, 3000)
    return () => clearInterval(id)
  }, [refresh])

  if (loading) {
    return (
      <div className="space-y-3">
        <div className="trade-card p-10 text-center text-gray-500">加载中…</div>
      </div>
    )
  }

  // 未报名 / 已结算 / 已淘汰：禁用交易，给出引导
  const canTrade = enrollment && enrollment.status === 'active'
  const status = enrollment?.status

  return (
    <div className="space-y-3">
      {/* 顶部模式横幅 */}
      <div className="trade-card p-3 flex items-center justify-between border-gold/40 bg-gradient-to-r from-gold/5 to-transparent">
        <div className="flex items-center gap-3">
          <span className="text-2xl">🏆</span>
          <div>
            <div className="text-sm font-bold text-gold">金龟子选拔赛 · 金龟子币交易</div>
            <div className="text-xs text-gray-500">
              保证金 / 盈亏 / 结算均使用「金龟子模拟币」，与「游戏币」完全隔离；系统每 15s 自动判定淘汰
            </div>
          </div>
        </div>
        <a
          href="/trade"
          className="text-xs px-3 py-1.5 rounded border border-gray-700 text-gray-300 hover:border-gold/40 hover:text-gold transition-colors"
        >
          ← 切换到 交易大厅(游戏币)
        </a>
      </div>

      {/* 实时权益 + 淘汰警戒线 + 阶段进度（参赛中才展示） */}
      {enrollment && status === 'active' && equity && (
        <div className="trade-card p-4">
          <div className="grid grid-cols-2 md:grid-cols-6 gap-3 mb-4">
            <div className="md:col-span-1">
              <div className="text-gray-500 text-xs mb-1">参赛档位</div>
              <div className="text-base font-bold text-gold">
                {tierLabel[enrollment.tier] || enrollment.tier}
              </div>
            </div>
            <div>
              <div className="text-gray-500 text-xs mb-1">金龟子币余额</div>
              <div className="text-base font-mono font-bold text-gray-200">
                {fmt(wallet?.balance)}
              </div>
            </div>
            <div>
              <div className="text-gray-500 text-xs mb-1">动态权益</div>
              <div className="text-base font-mono font-bold text-gray-200">
                {fmt(equity.dynamic_equity)}
              </div>
            </div>
            <div>
              <div className="text-gray-500 text-xs mb-1">累计收益率</div>
              <div className={`text-base font-mono font-bold ${(equity.return_rate || 0) >= 0 ? 'price-up' : 'price-down'}`}>
                {(equity.return_rate * 100).toFixed(2)}%
              </div>
            </div>
            <div>
              <div className="text-gray-500 text-xs mb-1">本金回撤</div>
              <div className={`text-base font-mono font-bold ${(equity.principal_drawdown || 0) >= 0.05 ? 'price-down' : 'text-gray-200'}`}>
                {(equity.principal_drawdown * 100).toFixed(2)}%
              </div>
              <div className={`text-[10px] mt-0.5 ${(equity.principal_drawdown || 0) >= 0.05 ? 'text-red-400' : 'text-gray-600'}`}>
                ≥5% 淘汰
              </div>
            </div>
            <div>
              <div className="text-gray-500 text-xs mb-1">峰值回撤</div>
              <div className={`text-base font-mono font-bold ${(equity.peak_drawdown || 0) >= 0.06 ? 'price-down' : 'text-gray-200'}`}>
                {(equity.peak_drawdown * 100).toFixed(2)}%
              </div>
              <div className={`text-[10px] mt-0.5 ${(equity.peak_drawdown || 0) >= 0.06 ? 'text-red-400' : 'text-gray-600'}`}>
                ≥6% 淘汰
              </div>
            </div>
          </div>

          {/* 阶段进度 */}
          <div>
            <div className="text-gray-500 text-xs mb-2">阶段盈利门槛（{enrollment.stage_reached || 0} 月已达成）</div>
            <div className="flex flex-wrap gap-2">
              {stageDefs.map((s) => {
                const reached = (equity.stage_reached || 0) >= s.months
                return (
                  <span
                    key={s.months}
                    className={`px-3 py-1 rounded-full text-xs border ${
                      reached
                        ? 'bg-green-900/30 text-green-400 border-green-700'
                        : 'bg-dark-200/40 text-gray-400 border-gray-700'
                    }`}
                  >
                    {s.label} {reached ? '✓' : ''}
                  </span>
                )
              })}
            </div>
          </div>
        </div>
      )}

      {/* 未报名 / 已结算 / 已淘汰：禁用交易 */}
      {!canTrade && (
        <div className="trade-card p-8 text-center border-yellow-700/50 bg-yellow-900/10">
          <div className="text-3xl mb-3">{status === 'eliminated' ? '⛔' : status === 'settled' ? '🏁' : '🎯'}</div>
          <div className="text-base font-semibold text-yellow-400 mb-2">
            {status === 'eliminated'
              ? '本场选拔赛已被淘汰'
              : status === 'settled'
              ? '本场选拔赛已结算'
              : '您尚未报名金龟子选拔赛'}
          </div>
          <div className="text-xs text-gray-400 mb-5">
            {status === 'eliminated'
              ? '如需继续参加，请前往赛事中心重新报名'
              : status === 'settled'
              ? '结算后如需继续参加，请前往赛事中心重新报名'
              : '选拔赛交易需要先报名参赛并获发参赛资金'}
          </div>
          <button
            onClick={() => navigate('/contest')}
            className="btn-gold px-6 py-2 text-sm"
          >
            前往赛事中心
          </button>
        </div>
      )}

      {/* Quote Bar — 始终展示行情 */}
      <QuoteBar />

      {/* 主网格：K 线 + 下单面板 */}
      <div className="grid grid-cols-1 xl:grid-cols-4 gap-3">
        <div className="xl:col-span-3">
          <KLineChart symbol="XAU" period="1m" />
        </div>
        <div>
          <OrderPanel contestId={contestId} disabled={!canTrade} />
        </div>
      </div>

      {/* 挂单 + 持仓 — 仅 contest 维度 */}
      <PendingOrdersList contestId={contestId} />
      <PositionList contestId={contestId} />
    </div>
  )
}