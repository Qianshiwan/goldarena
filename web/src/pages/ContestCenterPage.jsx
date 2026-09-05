import { useEffect, useRef, useState } from 'react'
import { jinguiziAPI, paymentAPI } from '../services/api'

const tierLabel = { small: '小账户 (100万)', medium: '中账户 (500万)', large: '大账户 (1000万)' }
const tierFee = { small: 200, medium: 1000, large: 2000 }
const tierCapital = { small: '100万金龟子币', medium: '500万金龟子币', large: '1000万金龟子币' }
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

  // 缴费报名流程状态
  const [payModal, setPayModal] = useState(null) // { order, qr_content, pay_url, sandbox, tier }
  const [creating, setCreating] = useState(false)
  const [payError, setPayError] = useState('')
  const pollRef = useRef(null)

  useEffect(() => {
    loadAll()
    return () => { if (pollRef.current) clearInterval(pollRef.current) }
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

  // 创建报名费订单
  const startEnroll = async (tier) => {
    if (creating) return
    setCreating(true)
    setPayError('')
    try {
      const { data } = await jinguiziAPI.createEnrollOrder(tier)
      const d = data.data
      setPayModal({ ...d, tier })
      // 轮询报名状态：支付成功后端自动报名
      if (pollRef.current) clearInterval(pollRef.current)
      pollRef.current = setInterval(async () => {
        try {
          const e = await jinguiziAPI.getEnrollment()
          if (e.data.data?.enrollment?.status === 'active') {
            clearInterval(pollRef.current)
            pollRef.current = null
            setPayModal(null)
            loadAll()
          }
        } catch {}
      }, 3000)
    } catch (e) {
      setPayError(e?.response?.data?.message || '创建订单失败，请稍后再试')
    } finally {
      setCreating(false)
    }
  }

  // 沙箱模式：模拟支付成功
  const simulatePay = async () => {
    if (!payModal?.order?.out_trade_no) return
    try {
      await paymentAPI.simulate(payModal.order.out_trade_no)
    } catch {}
  }

  const closePayModal = () => {
    if (pollRef.current) { clearInterval(pollRef.current); pollRef.current = null }
    setPayModal(null)
    loadAll()
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
            奖励 =（档位基数 + 20% × 档位系数）× 管理费，按三档固定额发放。基数 小 1 / 中 1 / 大 2，系数 小 1 / 中 2 / 大 3，
            管理费 小¥200 / 中¥1000 / 大¥2000；<span className="text-gold font-semibold">触发线: 盈利 ≥ 100%</span>，达标奖励小账户 ¥240 / 中账户 ¥1400 / 大账户 ¥5200，
            未达标仅退 6% 管理费(¥12 / ¥60 / ¥120)。
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
            本次参赛已{statusLabel[enrollment.status] || enrollment.status}。如需再次参赛，可重新缴费报名。
          </div>
        </div>
      ) : (
        <div className="space-y-4">
          <h2 className="text-lg font-semibold text-gray-200">缴费报名</h2>
          <div className="trade-card p-6">
            <p className="text-gray-400 text-sm mb-4">
              选择参赛档位并支付报名费（管理费），支付成功后系统将自动开通参赛账户并发放对应参赛资金。
              <strong className="text-gold ml-1">注意：比赛结束后的达标奖励和 6% 管理费退款，
              均需由管理员按规则核算后线下人工发放，不会自动入账。</strong>
            </p>
            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
              {['small', 'medium', 'large'].map((t) => (
                <div key={t} className="rounded-xl border border-gold/30 bg-dark-bg p-4 flex flex-col gap-2">
                  <div className="text-sm font-semibold text-gold">{tierLabel[t]}</div>
                  <div className="text-2xl font-bold text-gray-100">¥{tierFee[t]}</div>
                  <div className="text-xs text-gray-500">参赛资金：{tierCapital[t]}</div>
                  <button
                    onClick={() => startEnroll(t)}
                    disabled={creating}
                    className="btn-gold text-sm py-2 rounded-lg font-semibold disabled:opacity-40"
                  >
                    {creating ? '创建订单中…' : '缴费报名'}
                  </button>
                </div>
              ))}
            </div>
            {payError && <p className="text-red-400 text-xs mt-3">{payError}</p>}
            <p className="text-xs text-gray-500 mt-4">
              报名费即赛事管理费，达标结算时退还 6%（小/中/大分别退 ¥12 / ¥60 / ¥120）。
            </p>
          </div>
        </div>
      )}

      {/* 支付弹窗 */}
      {payModal && (
        <div className="fixed inset-0 bg-black/70 flex items-center justify-center z-50" onClick={closePayModal}>
          <div
            className="trade-card p-6 w-[360px] max-w-[92vw] space-y-4"
            onClick={(e) => e.stopPropagation()}
          >
            <h3 className="text-base font-semibold text-gold text-center">
              缴费报名 · {tierLabel[payModal.tier]}
            </h3>
            <div className="text-center text-2xl font-bold text-gray-100">¥{payModal.fee ?? tierFee[payModal.tier]}</div>
            {payModal.qr_content && (
              <div className="flex justify-center">
                <img
                  src={paymentAPI.qrURL(payModal.qr_content)}
                  alt="支付二维码"
                  className="w-48 h-48 rounded-lg bg-white p-1"
                />
              </div>
            )}
            <p className="text-xs text-gray-400 text-center leading-relaxed">
              {payModal.sandbox
                ? '沙箱模式：点击下方按钮模拟支付成功，验证缴费报名全流程。'
                : '请使用微信扫码支付。支付成功后系统将自动为你报名并发放参赛资金。'}
            </p>
            {payModal.sandbox && (
              <button
                onClick={simulatePay}
                className="btn-gold w-full py-2 rounded-lg text-sm font-semibold"
              >
                模拟支付成功
              </button>
            )}
            <div className="text-[10px] text-gray-600 text-center">
              订单号 {payModal.order?.out_trade_no} · 支付状态自动检测中…
            </div>
            <button
              onClick={closePayModal}
              className="w-full py-2 rounded-lg text-sm text-gray-400 hover:text-gray-200 border border-gray-700"
            >
              关闭
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
