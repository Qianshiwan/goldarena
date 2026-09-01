import { useEffect, useState } from 'react'
import { jinguiziAPI } from '../services/api'

// 交易类型 → 中文标签
const typeLabel = (t) => {
  switch (t) {
    case 'admin_recharge': return '管理员充值'
    case 'admin_deduct': return '管理员扣减'
    case 'contest_reward': return '选拔赛奖励'
    case 'contest_entry': return '报名发放'
    case 'settlement': return '结算'
    case 'contest_margin_freeze': return '参赛冻结保证金'
    case 'contest_margin_release': return '参赛释放保证金'
    case 'contest_pnl_credit': return '参赛盈亏 +'
    case 'contest_pnl_debit': return '参赛盈亏 -'
    default: return t
  }
}

const tierLabel = { small: '小账户 (100万)', medium: '中账户 (500万)', large: '大账户 (1000万)' }
const statusLabel = { active: '参赛中', settled: '已结算', eliminated: '已淘汰' }

// 收入类（余额增加）
const isIncomeType = (t) =>
  ['admin_recharge', 'contest_reward', 'settlement', 'contest_margin_release', 'contest_pnl_credit'].includes(t)

function fmt(n) {
  return (n ?? 0).toLocaleString('zh-CN', { minimumFractionDigits: 0, maximumFractionDigits: 2 })
}

// 选拔赛阶段门槛（与后端 jinguiziStageTargets 对应）
const stageDefs = [
  { months: 1, pct: 0.01, label: '1月≥1%' },
  { months: 3, pct: 0.10, label: '3月≥10%' },
  { months: 6, pct: 0.20, label: '6月≥20%' },
  { months: 9, pct: 0.29, label: '9月≥29%' },
]

export default function JinguiziWalletPage() {
  const [wallet, setWallet] = useState(null)
  const [enrollment, setEnrollment] = useState(null)
  const [equity, setEquity] = useState(null)
  const [transactions, setTransactions] = useState([])
  const [txnTotal, setTxnTotal] = useState(0)
  const [txnPage, setTxnPage] = useState(1)
  const [txnPageSize, setTxnPageSize] = useState(20)

  useEffect(() => {
    loadAll()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const loadAll = async () => {
    try {
      const { data } = await jinguiziAPI.getWallet()
      if (data.data) setWallet(data.data)
    } catch {}
    try {
      const { data } = await jinguiziAPI.getEnrollment()
      setEnrollment(data.data?.enrollment || null)
      setEquity(data.data?.equity || null)
    } catch {}
    await loadTransactions(1)
  }

  const loadTransactions = async (page) => {
    try {
      const { data } = await jinguiziAPI.getTransactions({ page, page_size: txnPageSize })
      const d = data.data
      setTransactions(d.list || [])
      setTxnTotal(d.total || 0)
      setTxnPage(page)
    } catch {}
  }

  return (
    <div className="max-w-4xl mx-auto space-y-6">
      <h1 className="text-xl font-bold gold-gradient">金龟子模拟币</h1>

      {/* 说明横幅 */}
      <div className="trade-card p-4 border-gold/30">
        <p className="text-xs text-gray-400 leading-relaxed">
          「金龟子模拟币」为选拔赛专用资金，与平台普通游戏币<strong className="text-gold">完全隔离</strong>，
          仅由管理员统一充值发放，不可自行充值或提现。
        </p>
      </div>

      {/* 钱包卡片 */}
      <div className="trade-card p-6">
        <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
          <div>
            <div className="text-gray-500 text-xs mb-1">可用余额</div>
            <div className="text-3xl font-mono font-bold text-gold">
              {wallet ? fmt(wallet.balance) : '—'}
            </div>
            <div className="text-xs text-gray-500 mt-0.5">金龟子币</div>
          </div>
          <div>
            <div className="text-gray-500 text-xs mb-1">冻结</div>
            <div className="text-3xl font-mono font-bold text-orange-400">
              {wallet ? fmt(wallet.frozen || 0) : '—'}
            </div>
            <div className="text-xs text-gray-500 mt-0.5">使用中</div>
          </div>
          <div>
            <div className="text-gray-500 text-xs mb-1">累计充值</div>
            <div className="text-3xl font-mono font-bold text-gray-300">
              {wallet ? fmt(wallet.total_recharged || 0) : '—'}
            </div>
            <div className="text-xs text-gray-500 mt-0.5">金龟子币</div>
          </div>
        </div>
      </div>

      {/* 选拔赛参赛状态 */}
      {enrollment && (
        <div className="trade-card p-6">
          <div className="flex items-center justify-between mb-4">
            <h3 className="text-sm font-semibold text-gold">选拔赛参赛状态</h3>
            <span className={`px-2 py-0.5 rounded text-xs ${
              enrollment.status === 'active' ? 'bg-green-900/30 text-green-400'
              : enrollment.status === 'settled' ? 'bg-gold/20 text-gold'
              : 'bg-red-900/30 text-red-400'
            }`}>
              {statusLabel[enrollment.status] || enrollment.status}
            </span>
          </div>
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
            <div>
              <div className="text-gray-500 text-xs mb-1">参赛档位</div>
              <div className="text-xl font-bold text-gold">{tierLabel[enrollment.tier] || enrollment.tier}</div>
            </div>
            <div>
              <div className="text-gray-500 text-xs mb-1">参赛资金</div>
              <div className="text-xl font-mono font-bold text-gray-200">{fmt(enrollment.initial_capital)}</div>
              <div className="text-xs text-gray-500 mt-0.5">金龟子币</div>
            </div>
            <div>
              <div className="text-gray-500 text-xs mb-1">报名时间</div>
              <div className="text-sm font-mono text-gray-300">{new Date(enrollment.enrolled_at).toLocaleString('zh-CN')}</div>
            </div>
            <div>
              <div className="text-gray-500 text-xs mb-1">结算时间</div>
              <div className="text-sm font-mono text-gray-300">
                {enrollment.settled_at ? new Date(enrollment.settled_at).toLocaleString('zh-CN') : '—'}
              </div>
            </div>
          </div>
          <p className="text-xs text-gray-500 leading-relaxed mt-3">
            参赛资金为选拔赛专用「金龟子模拟币」，与平台普通游戏币完全隔离，专款专用；比赛结束后由管理员按规则结算。
          </p>
        </div>
      )}

      {/* 实时权益 / 回撤 / 阶段进度（仅参赛用户显示） */}
      {enrollment && equity && (
        <div className="trade-card p-6">
          <h3 className="text-sm font-semibold text-gold mb-4">实时权益与淘汰进度</h3>
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-5">
            <div>
              <div className="text-gray-500 text-xs mb-1">动态权益</div>
              <div className="text-xl font-mono font-bold text-gray-200">{fmt(equity.dynamic_equity)}</div>
              <div className="text-xs text-gray-500 mt-0.5">金龟子币</div>
            </div>
            <div>
              <div className="text-gray-500 text-xs mb-1">累计收益率</div>
              <div className={`text-xl font-mono font-bold ${(equity.return_rate || 0) >= 0 ? 'price-up' : 'price-down'}`}>
                {(equity.return_rate * 100).toFixed(2)}%
              </div>
            </div>
            <div>
              <div className="text-gray-500 text-xs mb-1">本金回撤</div>
              <div className={`text-xl font-mono font-bold ${(equity.principal_drawdown || 0) >= 0.05 ? 'price-down' : 'text-gray-300'}`}>
                {(equity.principal_drawdown * 100).toFixed(2)}%
              </div>
              <div className="text-xs text-gray-500 mt-0.5">≥5% 淘汰</div>
            </div>
            <div>
              <div className="text-gray-500 text-xs mb-1">历史最高回撤</div>
              <div className={`text-xl font-mono font-bold ${(equity.peak_drawdown || 0) >= 0.06 ? 'price-down' : 'text-gray-300'}`}>
                {(equity.peak_drawdown * 100).toFixed(2)}%
              </div>
              <div className="text-xs text-gray-500 mt-0.5">≥6% 淘汰</div>
            </div>
          </div>
          <div>
            <div className="text-gray-500 text-xs mb-2">阶段盈利门槛进度</div>
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
            {enrollment.status === 'active' && (
              <p className="text-xs text-gray-500 leading-relaxed mt-3">
                系统每 15 秒自动判定：本金回撤 ≥5% 或 历史最高动态权益回撤 ≥6% 将自动淘汰并收回参赛资金；
                到对应月份未达阶段盈利门槛同样淘汰。
              </p>
            )}
          </div>
        </div>
      )}

      {/* 交易流水 */}
      <div className="trade-card">
        <div className="p-3 border-b border-gray-800">
          <h3 className="text-sm font-semibold text-gray-300">金龟子币流水</h3>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-gray-500 text-xs border-b border-gray-800">
                <th className="text-left p-3">类型</th>
                <th className="text-right p-3">变动</th>
                <th className="text-right p-3">变动前</th>
                <th className="text-right p-3">变动后</th>
                <th className="text-left p-3">备注</th>
                <th className="text-right p-3">时间</th>
              </tr>
            </thead>
            <tbody>
              {transactions.map((t) => {
                const income = isIncomeType(t.type) || t.amount >= 0
                return (
                  <tr key={t.id} className="border-b border-gray-800/50 hover:bg-dark-200/50">
                    <td className="p-3">
                      <span className={`px-2 py-0.5 rounded text-xs ${
                        income ? 'bg-green-900/30 text-green-400' : 'bg-red-900/30 text-red-400'
                      }`}>
                        {typeLabel(t.type)}
                      </span>
                    </td>
                    <td className={`text-right p-3 font-mono ${income ? 'price-up' : 'price-down'}`}>
                      {income ? '+' : ''}{fmt(t.amount)}
                    </td>
                    <td className="text-right p-3 font-mono text-gray-400">{fmt(t.balance_before)}</td>
                    <td className="text-right p-3 font-mono text-gray-300">{fmt(t.balance_after)}</td>
                    <td className="p-3 text-xs text-gray-500">{t.remark}</td>
                    <td className="text-right p-3 text-xs text-gray-500">
                      {new Date(t.created_at).toLocaleString('zh-CN')}
                    </td>
                  </tr>
                )
              })}
              {transactions.length === 0 && (
                <tr>
                  <td colSpan={6} className="p-8 text-center text-gray-500 text-sm">
                    暂无金龟子币流水
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
        {txnTotal > 0 && (
          <div className="p-3 flex items-center justify-between border-t border-gray-800">
            <span className="text-xs text-gray-500">共 {txnTotal} 条记录</span>
            <Pagination
              total={txnTotal}
              page={txnPage}
              pageSize={txnPageSize}
              onChange={(p) => loadTransactions(p)}
            />
          </div>
        )}
      </div>
    </div>
  )
}

// ========== 分页组件 (与 WalletPage 同款) ==========
function Pagination({ total, page, pageSize, onChange }) {
  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  if (totalPages <= 1) return null

  const pages = []
  const start = Math.max(1, page - 2)
  const end = Math.min(totalPages, page + 2)
  for (let i = start; i <= end; i++) pages.push(i)

  const btn = (label, target, disabled, active) => (
    <button
      key={label + (target || '')}
      disabled={disabled}
      onClick={() => !disabled && onChange(target)}
      className={`px-2.5 py-1 text-xs rounded border transition-colors ${
        active
          ? 'bg-gold text-black border-gold font-semibold'
          : disabled
          ? 'text-gray-600 border-gray-800 cursor-not-allowed'
          : 'text-gray-400 border-gray-700 hover:border-gold hover:text-gold'
      }`}
    >
      {label}
    </button>
  )

  return (
    <div className="flex items-center gap-1">
      {btn('上一页', page - 1, page <= 1, false)}
      {start > 1 && (
        <>
          {btn('1', 1, false, page === 1)}
          {start > 2 && <span className="text-gray-600 text-xs px-1">…</span>}
        </>
      )}
      {pages.map((p) => btn(String(p), p, false, p === page))}
      {end < totalPages && (
        <>
          {end < totalPages - 1 && <span className="text-gray-600 text-xs px-1">…</span>}
          {btn(String(totalPages), totalPages, false, page === totalPages)}
        </>
      )}
      {btn('下一页', page + 1, page >= totalPages, false)}
      <span className="text-xs text-gray-500 ml-2">{page}/{totalPages} 页</span>
    </div>
  )
}
