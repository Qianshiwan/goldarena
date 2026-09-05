import { useEffect, useState } from 'react'
import { jinguiziAPI } from '../../services/api'

function fmt(n) {
  return (n ?? 0).toLocaleString('zh-CN', { minimumFractionDigits: 0, maximumFractionDigits: 2 })
}

const statusLabel = { active: '参赛中', settled: '已结算', eliminated: '已淘汰' }

// 将"目标用户"输入框解析为后端需要的 user_id 或 username
// 纯数字同时传 user_id 和 username（因为用户名可能是 "555" 这种数字）
function resolveTarget(raw) {
  const v = (raw || '').trim()
  if (/^\d+$/.test(v)) {
    const id = Number(v)
    if (id > 0) return { user_id: id, username: v }
  }
  return { username: v }
}

export default function AdminJinguizi() {
  const [list, setList] = useState([])
  const [keyword, setKeyword] = useState('')
  const [msg, setMsg] = useState('')

  // 发放充值 (amount > 0)
  const [rechargeTarget, setRechargeTarget] = useState('')
  const [rechargeAmount, setRechargeAmount] = useState('')
  const [rechargeRemark, setRechargeRemark] = useState('')

  // 增减调整 (signed)
  const [adjustTarget, setAdjustTarget] = useState('')
  const [adjustAmount, setAdjustAmount] = useState('')
  const [adjustRemark, setAdjustRemark] = useState('')

  // 报名选拔赛（按档位发放参赛资金）
  const [enrollTarget, setEnrollTarget] = useState('')
  const [enrollTier, setEnrollTier] = useState('small')

  // 结算 / 淘汰
  const [settleTarget, setSettleTarget] = useState('')
  const [settleAction, setSettleAction] = useState('settle')

  const loadList = async () => {
    try {
      const { data } = await jinguiziAPI.adminList({ keyword })
      setList(data.data.list || [])
    } catch {}
  }

  useEffect(() => {
    loadList()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [keyword])

  const doRecharge = async () => {
    setMsg('')
    if (!rechargeTarget.trim()) return setMsg('请填写目标用户（ID 或 用户名）')
    const amt = parseFloat(rechargeAmount)
    if (!amt || amt <= 0) return setMsg('充值金额必须为正数')
    try {
      const { data } = await jinguiziAPI.adminRecharge({
        ...resolveTarget(rechargeTarget),
        amount: amt,
        remark: rechargeRemark.trim(),
      })
      const d = data.data
      setMsg(`充值成功：用户 ${d.user_id} 余额 ${fmt(d.balance_before)} → ${fmt(d.balance_after)} (+${fmt(d.amount)})`)
      setRechargeAmount('')
      setRechargeRemark('')
      loadList()
    } catch (e) {
      setMsg('充值失败: ' + (e.response?.data?.message || '未知错误'))
    }
  }

  const doAdjust = async () => {
    setMsg('')
    if (!adjustTarget.trim()) return setMsg('请填写目标用户（ID 或 用户名）')
    const amt = parseFloat(adjustAmount)
    if (!amt || amt === 0) return setMsg('增减金额不能为 0')
    try {
      const { data } = await jinguiziAPI.adminAdjust({
        ...resolveTarget(adjustTarget),
        amount: amt,
        remark: adjustRemark.trim(),
      })
      const d = data.data
      setMsg(`调整成功：用户 ${d.user_id} 余额 ${fmt(d.balance_before)} → ${fmt(d.balance_after)} (${d.delta >= 0 ? '+' : ''}${fmt(d.delta)})`)
      setAdjustAmount('')
      setAdjustRemark('')
      loadList()
    } catch (e) {
      setMsg('调整失败: ' + (e.response?.data?.message || '未知错误'))
    }
  }

  const doEnroll = async () => {
    setMsg('')
    if (!enrollTarget.trim()) return setMsg('请填写目标用户（ID 或 用户名）')
    try {
      const { data } = await jinguiziAPI.adminEnroll({
        ...resolveTarget(enrollTarget),
        tier: enrollTier,
      })
      const d = data.data
      setMsg(`报名成功：用户 ${d.user_id} · ${d.tier_label} · 发放 ${fmt(d.initial_capital)} (余额 ${fmt(d.balance_before)} → ${fmt(d.balance_after)})`)
      setEnrollTarget('')
      loadList()
    } catch (e) {
      setMsg('报名失败: ' + (e.response?.data?.message || '未知错误'))
    }
  }

  const doSettle = async () => {
    setMsg('')
    if (!settleTarget.trim()) return setMsg('请填写目标用户（ID 或 用户名）')
    try {
      const { data } = await jinguiziAPI.adminSettle({
        ...resolveTarget(settleTarget),
        action: settleAction,
      })
      const d = data.data
      if (d.action === 'eliminate') {
        setMsg(`淘汰结算成功：用户 ${d.user_id} · 收回参赛资金 · 余额 ${fmt(d.balance_before)} → ${fmt(d.balance_after)}`)
      } else {
        const refund = fmt(d.fee_refund || 0)
        const reward = fmt(d.reward || 0)
        const ret = ((d.return_pct || 0) * 100).toFixed(1)
        const triggered = d.triggered ? '✅ 达标奖励' : '⚠️ 未达触发线(仅退管理费)'
        setMsg(
          `结算成功：用户 ${d.user_id} · 档位 ${d.tier} · 盈利率 ${ret}% · ${triggered}\n` +
          `  · 退游戏币 ¥${refund} · 奖励金龟子币 ${reward}\n` +
          `  · 公式: ${d.reward_reason}\n` +
          `  · ⚠️ 平台只入金不出金, 奖励/退款已记为 manual 流水, 游戏币与金龟子钱包余额均未自动入账,\n` +
          `     请管理员在用户钱包页面按流水人工线下发放。`
        )
      }
      setSettleTarget('')
      loadList()
    } catch (e) {
      setMsg('结算失败: ' + (e.response?.data?.message || '未知错误'))
    }
  }

  const doJudge = async () => {
    setMsg('')
    try {
      const { data } = await jinguiziAPI.adminJudge()
      setMsg(`判定已执行，剩余参赛 ${data.data.active_remaining} 人`)
    } catch (e) {
      setMsg('判定失败: ' + (e.response?.data?.message || '未知错误'))
    }
  }

  return (
    <div className="space-y-5">
      <h2 className="text-lg font-semibold text-gray-200">金龟子钱包管理</h2>

      {/* 操作提示 */}
      {msg && (
        <div className={`p-3 rounded text-xs ${
          msg.includes('成功') ? 'bg-green-900/30 text-green-400' : 'bg-red-900/30 text-red-400'
        }`}>
          {msg}
        </div>
      )}

      {/* 操作面板 */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {/* 发放充值 */}
        <div className="trade-card p-4">
          <h3 className="text-sm font-semibold text-gold mb-3">发放金龟子币（充值）</h3>
          <div className="space-y-3">
            <div>
              <label className="block text-xs text-gray-500 mb-1">目标用户（用户ID 或 用户名）</label>
              <input
                value={rechargeTarget}
                onChange={(e) => setRechargeTarget(e.target.value)}
                placeholder="例如 6 或 jinguizi_user"
                className="w-full bg-[#0F1923] text-gray-200 border border-gray-700 rounded px-3 py-2"
              />
            </div>
            <div>
              <label className="block text-xs text-gray-500 mb-1">金额（必须为正数）</label>
              <input
                type="number"
                min="0"
                step="0.01"
                value={rechargeAmount}
                onChange={(e) => setRechargeAmount(e.target.value)}
                placeholder="100"
                className="w-full bg-[#0F1923] text-gray-200 border border-gray-700 rounded px-3 py-2"
              />
            </div>
            <div>
              <label className="block text-xs text-gray-500 mb-1">备注（可选）</label>
              <input
                value={rechargeRemark}
                onChange={(e) => setRechargeRemark(e.target.value)}
                placeholder="选拔赛报名发放"
                className="w-full bg-[#0F1923] text-gray-200 border border-gray-700 rounded px-3 py-2"
              />
            </div>
            <button onClick={doRecharge} className="btn-gold text-sm px-4 py-2 w-full">
              发放充值
            </button>
          </div>
        </div>

        {/* 增减调整 */}
        <div className="trade-card p-4">
          <h3 className="text-sm font-semibold text-gold mb-3">增减调整（可正可负）</h3>
          <div className="space-y-3">
            <div>
              <label className="block text-xs text-gray-500 mb-1">目标用户（用户ID 或 用户名）</label>
              <input
                value={adjustTarget}
                onChange={(e) => setAdjustTarget(e.target.value)}
                placeholder="例如 6 或 jinguizi_user"
                className="w-full bg-[#0F1923] text-gray-200 border border-gray-700 rounded px-3 py-2"
              />
            </div>
            <div>
              <label className="block text-xs text-gray-500 mb-1">金额（正数增加 / 负数扣减，最低归零）</label>
              <input
                type="number"
                step="0.01"
                value={adjustAmount}
                onChange={(e) => setAdjustAmount(e.target.value)}
                placeholder="-50"
                className="w-full bg-[#0F1923] text-gray-200 border border-gray-700 rounded px-3 py-2"
              />
            </div>
            <div>
              <label className="block text-xs text-gray-500 mb-1">备注（可选）</label>
              <input
                value={adjustRemark}
                onChange={(e) => setAdjustRemark(e.target.value)}
                placeholder="参赛扣除 / 违规扣减"
                className="w-full bg-[#0F1923] text-gray-200 border border-gray-700 rounded px-3 py-2"
              />
            </div>
            <button
              onClick={doAdjust}
              className="text-sm px-4 py-2 w-full rounded border border-gray-700 text-gray-300 hover:border-gold hover:text-gold transition-colors"
            >
              执行增减
            </button>
          </div>
        </div>

        {/* 报名选拔赛 */}
        <div className="trade-card p-4">
          <h3 className="text-sm font-semibold text-gold mb-3">报名选拔赛（按档位发放参赛资金）</h3>
          <div className="space-y-3">
            <div>
              <label className="block text-xs text-gray-500 mb-1">目标用户（用户ID 或 用户名）</label>
              <input
                value={enrollTarget}
                onChange={(e) => setEnrollTarget(e.target.value)}
                placeholder="例如 6 或 jinguizi_user"
                className="w-full bg-[#0F1923] text-gray-200 border border-gray-700 rounded px-3 py-2"
              />
            </div>
            <div>
              <label className="block text-xs text-gray-500 mb-1">参赛档位</label>
              <select
                value={enrollTier}
                onChange={(e) => setEnrollTier(e.target.value)}
                className="w-full bg-dark-200 border border-gray-700 rounded px-2 py-1.5 text-sm text-gray-200"
              >
                <option value="small">小账户 (100万)</option>
                <option value="medium">中账户 (500万)</option>
                <option value="large">大账户 (1000万)</option>
              </select>
            </div>
            <button onClick={doEnroll} className="btn-gold text-sm px-4 py-2 w-full">
              报名并发放参赛资金
            </button>
          </div>
        </div>

        {/* 结算 / 淘汰 */}
        <div className="trade-card p-4">
          <h3 className="text-sm font-semibold text-gold mb-3">结算 / 淘汰</h3>
          <div className="space-y-3">
            <div>
              <label className="block text-xs text-gray-500 mb-1">目标用户（用户ID 或 用户名）</label>
              <input
                value={settleTarget}
                onChange={(e) => setSettleTarget(e.target.value)}
                placeholder="例如 6 或 jinguizi_user"
                className="w-full bg-[#0F1923] text-gray-200 border border-gray-700 rounded px-3 py-2"
              />
            </div>
            <div>
              <label className="block text-xs text-gray-500 mb-1">操作</label>
              <select
                value={settleAction}
                onChange={(e) => setSettleAction(e.target.value)}
                className="w-full bg-dark-200 border border-gray-700 rounded px-2 py-1.5 text-sm text-gray-200"
              >
                <option value="settle">达标结算（按公式算奖励 + 退 6% 管理费, 仅记 manual 流水, 需人工发放）</option>
                <option value="eliminate">淘汰（收回参赛资金）</option>
              </select>
            </div>
            {settleAction === 'settle' && (
              <div className="p-2 rounded bg-dark-200 border border-gray-700/50 text-[11px] text-gray-400 leading-relaxed">
                <div className="font-semibold text-gray-300 mb-1">奖励公式（按档位自动计算, 无需手填）</div>
                <div>小账户(200元): <span className="font-mono text-gold">(1 + 20%×1) × 200 = 240</span></div>
                <div>中账户(1000元): <span className="font-mono text-gold">(1 + 20%×2) × 1000 = 1400</span></div>
                <div>大账户(2000元): <span className="font-mono text-gold">(2 + 20%×3) × 2000 = 5200</span></div>
                <div className="mt-1 text-cyan-300">触发线: 盈利 ≥ 100%；未达标仅退 6% 管理费, 无奖励</div>
                <div className="mt-2 text-amber-300 font-semibold">
                  ⚠️ 平台政策: 只入金不出金, 奖励/退款均<strong>不自动入账</strong>。
                  本次结算仅写入 manual 流水(type=contest_reward_manual / contest_fee_refund_manual),
                  余额前后相等。请按流水记录<strong>线下联系用户发放</strong>。
                </div>
              </div>
            )}
            <button
              onClick={doSettle}
              className="text-sm px-4 py-2 w-full rounded border border-gray-700 text-gray-300 hover:border-gold hover:text-gold transition-colors"
            >
              执行结算 / 淘汰
            </button>
          </div>
        </div>
      </div>

      {/* 钱包列表 */}
      <div className="trade-card overflow-x-auto">
        <div className="p-3 border-b border-gray-800 flex items-center justify-between">
          <h3 className="text-sm font-semibold text-gray-300">金龟子钱包列表</h3>
          <div className="flex items-center gap-2">
            <button
              onClick={doJudge}
              className="text-xs px-3 py-1.5 rounded border border-gray-700 text-gray-300 hover:border-gold hover:text-gold transition-colors"
            >
              强制判定
            </button>
            <input
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              placeholder="搜索 用户名 / 昵称"
              className="w-56 text-xs py-1.5"
            />
          </div>
        </div>
        <table className="w-full text-sm">
          <thead>
            <tr className="text-gray-500 text-xs border-b border-gray-800">
              <th className="text-left p-3">用户ID</th>
              <th className="text-left p-3">用户名</th>
              <th className="text-left p-3">昵称</th>
              <th className="text-right p-3">余额</th>
              <th className="text-right p-3">冻结</th>
              <th className="text-right p-3">累计充值</th>
              <th className="text-left p-3">参赛状态</th>
              <th className="text-left p-3">档位</th>
              <th className="text-right p-3">阶段</th>
            </tr>
          </thead>
          <tbody>
            {list.map((w) => (
              <tr key={w.user_id} className="border-b border-gray-800/50 hover:bg-dark-200/50">
                <td className="p-3 font-mono text-gray-400 text-xs">{w.user_id}</td>
                <td className="p-3 text-gray-300">{w.username || '-'}</td>
                <td className="p-3 text-gray-400">{w.nickname || '-'}</td>
                <td className="p-3 text-right font-mono text-gold font-bold">{fmt(w.balance)}</td>
                <td className="p-3 text-right font-mono text-orange-400">{fmt(w.frozen || 0)}</td>
                <td className="p-3 text-right font-mono text-gray-300">{fmt(w.total_recharged || 0)}</td>
                <td className="p-3">
                  {w.enrollment_status ? (
                    <span className={`px-2 py-0.5 rounded text-xs ${
                      w.enrollment_status === 'active' ? 'bg-green-900/30 text-green-400'
                        : w.enrollment_status === 'settled' ? 'bg-gold/20 text-gold'
                        : 'bg-red-900/30 text-red-400'
                    }`}>
                      {statusLabel[w.enrollment_status] || w.enrollment_status}
                    </span>
                  ) : (
                    <span className="text-gray-600 text-xs">未参赛</span>
                  )}
                </td>
                <td className="p-3 text-gray-400 text-xs">{w.tier ? ({ small: '小', medium: '中', large: '大' }[w.tier] || w.tier) : '—'}</td>
                <td className="p-3 text-right font-mono text-gray-300">{w.stage_reached ? `${w.stage_reached}月` : '—'}</td>
              </tr>
            ))}
            {list.length === 0 && (
              <tr>
                <td colSpan={6} className="p-8 text-center text-gray-500 text-sm">
                  暂无金龟子钱包（尚未向任何用户发放）
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
