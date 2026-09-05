import { useEffect, useState } from 'react'
import { jinguiziAPI } from '../services/api'

const tierLabel = { small: '小账户 (100万)', medium: '中账户 (500万)', large: '大账户 (1000万)' }
const statusLabel = { active: '参赛中', settled: '已结算', eliminated: '已淘汰' }

// 阶段门槛（与海报文案、后端 jinguiziStageTargets 一致）
const stageDefs = [
  { months: 1, pct: 0.01, label: '1月≥1%' },
  { months: 3, pct: 0.10, label: '3月≥10%' },
  { months: 6, pct: 0.29, label: '6月≥29%' },
]

const fmt = (n) => (n ?? 0).toLocaleString('zh-CN', { minimumFractionDigits: 0, maximumFractionDigits: 2 })

function RuleBox({ label, value }) {
  return (
    <div className="bg-dark-bg rounded-xl border border-gray-800 p-3">
      <div className="text-xs text-gray-500 mb-1">{label}</div>
      <div className="text-sm font-semibold text-gray-200">{value}</div>
    </div>
  )
}

function Info({ label, value }) {
  return (
    <div>
      <div className="text-xs text-gray-500 mb-1">{label}</div>
      <div className="text-lg font-semibold text-gray-200">{value}</div>
    </div>
  )
}

export default function ContestCenterPage() {
  const [wallet, setWallet] = useState(null)
  const [enrollment, setEnrollment] = useState(null)
  const [equity, setEquity] = useState(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    loadAll()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const loadAll = async () => {
    try {
      const w = await jinguiziAPI.getWallet()
      if (w.data.data) setWallet(w.data.data)
    } catch {}
    try {
      const e = await jinguiziAPI.getEnrollment()
      setEnrollment(e.data.data?.enrollment || null)
      setEquity(e.data.data?.equity || null)
    } catch {}
    setLoading(false)
  }

  if (loading) return <div className="max-w-4xl mx-auto p-10 text-center text-gray-500">加载中…</div>

  const status = enrollment?.status
  const isActive = status === 'active'
  const isEnded = status === 'eliminated' || status === 'settled'

  return (
    <div className="max-w-4xl mx-auto space-y-6">
      <h1 className="text-xl font-bold gold-gradient">赛事中心 · 金龟子黄金现货模拟选拔赛</h1>

      {/* 赛事宗旨 */}
      <div className="rounded-2xl bg-gradient-to-r from-gold/20 to-gold-light/10 border border-gold/30 p-5">
        <h2 className="text-sm font-semibold text-gold mb-1">赛事宗旨</h2>
        <p className="text-sm text-gray-300 leading-relaxed">
          以真实伦敦金行情为练兵场，在零风险模拟中训练盘手看盘、择时与严格风控的本领，
          系统提升交易水平与稳定盈利能力，选拔并培养真正合格的交易人才。
        </p>
      </div>

      {/* 赛事规则 */}
      <div className="trade-card p-6 space-y-4">
        <h2 className="text-lg font-semibold text-gray-200">赛事规则</h2>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          <RuleBox label="赛期" value="6 个月" />
          <RuleBox label="交易标的" value="伦敦金 XAU" />
          <RuleBox label="合约规格" value="100 盎司/手" />
          <RuleBox label="账户性质" value="虚拟本金·零风险" />
        </div>
        <div>
          <div className="text-sm text-gray-400 mb-2">阶段盈利门槛（逐月达成，达标晋级 / 最终通过）</div>
          <div className="flex flex-wrap gap-2">
            {stageDefs.map((s) => (
              <span key={s.months} className="px-3 py-1.5 rounded-lg bg-dark-bg border border-gray-800 text-gray-300 text-sm">
                {s.label}
              </span>
            ))}
          </div>
        </div>
        <div className="text-sm text-gray-400 leading-relaxed border-t border-gray-800 pt-3 space-y-1">
          <div>
            <span className="text-gray-300">淘汰规则：</span>
            本金回撤 ≥ 5% 或 历史最高动态权益回撤 ≥ 6% 自动淘汰并收回参赛资金；阶段盈利门槛到期未达标亦淘汰。
          </div>
          <div>
            <span className="text-gray-300">达标奖励：</span>
            奖励 =（档位基数 + 账户盈利率）× 管理费 200 元（小/中/大基数 0/1/2），三档均另退 6% 管理费。
          </div>
        </div>
      </div>

      {/* 我的参赛专区 / 记录 */}
      {isActive ? (
        <div className="space-y-4">
          <h2 className="text-lg font-semibold text-gray-200">我的参赛专区</h2>

          <div className="trade-card p-5 grid grid-cols-2 md:grid-cols-4 gap-4">
            <Info label="参赛档位" value={tierLabel[enrollment.tier] || enrollment.tier} />
            <Info
              label="状态"
              value={<span className="text-green-400">{statusLabel[enrollment.status] || enrollment.status}</span>}
            />
            <Info label="报名本金" value={fmt(enrollment.initial_capital)} />
            <Info label="金龟子余额" value={fmt(wallet?.balance)} />
          </div>

          {equity && (
            <div className="trade-card p-5 space-y-4">
              <h3 className="text-sm font-semibold text-gray-300">实时权益与淘汰进度</h3>
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <Info label="动态权益" value={<span className="font-mono text-gray-100">{fmt(equity.dynamic_equity)}</span>} />
                <Info
                  label="累计收益率"
                  value={
                    <span className={`font-mono ${(equity.return_rate || 0) >= 0 ? 'text-green-400' : 'text-red-400'}`}>
                      {((equity.return_rate || 0) * 100).toFixed(2)}%
                    </span>
                  }
                />
                <Info
                  label="本金回撤"
                  value={
                    <span className={`font-mono ${(equity.principal_drawdown || 0) >= 0.05 ? 'text-red-400' : 'text-gray-300'}`}>
                      {((equity.principal_drawdown || 0) * 100).toFixed(2)}%
                    </span>
                  }
                />
                <Info
                  label="峰值回撤"
                  value={
                    <span className={`font-mono ${(equity.peak_drawdown || 0) >= 0.06 ? 'text-red-400' : 'text-gray-300'}`}>
                      {((equity.peak_drawdown || 0) * 100).toFixed(2)}%
                    </span>
                  }
                />
              </div>
              <div>
                <div className="text-xs text-gray-500 mb-2">阶段达成（绿=已通过）</div>
                <div className="flex flex-wrap gap-2">
                  {stageDefs.map((s) => {
                    const reached = (equity.stage_reached || 0) >= s.months
                    return (
                      <span
                        key={s.months}
                        className={`px-3 py-1.5 rounded-lg text-sm border ${
                          reached
                            ? 'bg-green-900/30 border-green-700 text-green-400'
                            : 'bg-dark-bg border-gray-800 text-gray-500'
                        }`}
                      >
                        {s.label} {reached ? '✓' : ''}
                      </span>
                    )
                  })}
                </div>
              </div>
              <p className="text-xs text-gray-500">
                系统每 15 秒自动判定一次回撤与阶段达标，触发淘汰将收回参赛资金。
              </p>
            </div>
          )}
        </div>
      ) : isEnded ? (
        <div className="space-y-4">
          <h2 className="text-lg font-semibold text-gray-200">我的参赛记录</h2>
          <div className="trade-card p-5 grid grid-cols-2 md:grid-cols-4 gap-4">
            <Info label="参赛档位" value={tierLabel[enrollment.tier] || enrollment.tier} />
            <Info
              label="状态"
              value={
                <span className={enrollment.status === 'eliminated' ? 'text-red-400' : 'text-gray-300'}>
                  {statusLabel[enrollment.status] || enrollment.status}
                </span>
              }
            />
            <Info label="报名本金" value={fmt(enrollment.initial_capital)} />
            <Info label="金龟子余额" value={fmt(wallet?.balance)} />
          </div>
          {equity && (
            <div className="trade-card p-5 space-y-4">
              <h3 className="text-sm font-semibold text-gray-300">最终权益快照</h3>
              <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                <Info label="动态权益" value={<span className="font-mono text-gray-100">{fmt(equity.dynamic_equity)}</span>} />
                <Info
                  label="累计收益率"
                  value={
                    <span className={`font-mono ${(equity.return_rate || 0) >= 0 ? 'text-green-400' : 'text-red-400'}`}>
                      {((equity.return_rate || 0) * 100).toFixed(2)}%
                    </span>
                  }
                />
                <Info label="本金回撤" value={<span className="font-mono text-gray-300">{((equity.principal_drawdown || 0) * 100).toFixed(2)}%</span>} />
                <Info label="峰值回撤" value={<span className="font-mono text-gray-300">{((equity.peak_drawdown || 0) * 100).toFixed(2)}%</span>} />
              </div>
            </div>
          )}
          <div className="trade-card p-4 text-center text-sm text-gray-400">
            本次参赛已{statusLabel[enrollment.status] || enrollment.status}。如需再次参赛，请联系管理员重新报名。
          </div>
        </div>
      ) : (
        <div className="trade-card p-6 text-center">
          <p className="text-gray-400 text-sm mb-2">你尚未报名参赛</p>
          <p className="text-xs text-gray-500">
            选拔赛由管理员统一报名。请联系管理员报名对应档位，开启你的模拟选拔之旅。
          </p>
        </div>
      )}
    </div>
  )
}
