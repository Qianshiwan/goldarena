import { useEffect, useState } from 'react'
import { jinguiziAPI } from '../../services/api'

function fmt(n) {
  return (n ?? 0).toLocaleString('zh-CN', { minimumFractionDigits: 0, maximumFractionDigits: 2 })
}

// 将"目标用户"输入框解析为后端需要的 user_id 或 username
function resolveTarget(raw) {
  const v = (raw || '').trim()
  if (/^\d+$/.test(v)) {
    const id = Number(v)
    if (id > 0) return { user_id: id }
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
                className="w-full"
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
                className="w-full"
              />
            </div>
            <div>
              <label className="block text-xs text-gray-500 mb-1">备注（可选）</label>
              <input
                value={rechargeRemark}
                onChange={(e) => setRechargeRemark(e.target.value)}
                placeholder="选拔赛报名发放"
                className="w-full"
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
                className="w-full"
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
                className="w-full"
              />
            </div>
            <div>
              <label className="block text-xs text-gray-500 mb-1">备注（可选）</label>
              <input
                value={adjustRemark}
                onChange={(e) => setAdjustRemark(e.target.value)}
                placeholder="参赛扣除 / 违规扣减"
                className="w-full"
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
      </div>

      {/* 钱包列表 */}
      <div className="trade-card overflow-x-auto">
        <div className="p-3 border-b border-gray-800 flex items-center justify-between">
          <h3 className="text-sm font-semibold text-gray-300">金龟子钱包列表</h3>
          <input
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            placeholder="搜索 用户名 / 昵称"
            className="w-56 text-xs py-1.5"
          />
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
