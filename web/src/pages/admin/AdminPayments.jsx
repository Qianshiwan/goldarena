import { useEffect, useState } from 'react'
import { adminAPI } from '../../services/api'

function fmt(n) {
  return (n ?? 0).toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

const statusText = (s) => {
  if (s === 'paid') return <span className="text-green-400 text-xs">已支付</span>
  if (s === 'pending') return <span className="text-yellow-400 text-xs">待支付</span>
  if (s === 'failed') return <span className="text-red-400 text-xs">失败</span>
  return <span className="text-gray-400 text-xs">{s}</span>
}

export default function AdminPayments() {
  const [list, setList] = useState([])
  const [status, setStatus] = useState('')

  const load = async () => {
    try {
      const { data } = await adminAPI.listPayments({ status })
      setList(data.data.list || [])
    } catch {}
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status])

  const onCredit = async (no) => {
    if (!confirm('确认为该订单手动补单入账（发放游戏币）？')) return
    try {
      await adminAPI.creditPayment(no)
      alert('补单成功，游戏币已入账')
      load()
    } catch (e) {
      alert('失败: ' + (e.response?.data?.message || '未知错误'))
    }
  }

  return (
    <div>
      <h2 className="text-lg font-semibold text-gray-200 mb-4">支付管理</h2>
      <div className="flex gap-2 mb-4">
        {[
          { v: '', t: '全部' },
          { v: 'pending', t: '待支付' },
          { v: 'paid', t: '已支付' },
          { v: 'failed', t: '失败' },
        ].map((opt) => (
          <button
            key={opt.v}
            onClick={() => setStatus(opt.v)}
            className={`px-3 py-1.5 rounded text-sm ${status === opt.v ? 'bg-gold/15 text-gold' : 'text-gray-400 hover:text-gray-200 border border-gray-700'}`}
          >
            {opt.t}
          </button>
        ))}
      </div>

      <div className="trade-card overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-gray-500 text-xs border-b border-gray-800">
              <th className="text-left p-3">流水号</th>
              <th className="text-left p-3">用户</th>
              <th className="text-left p-3">渠道</th>
              <th className="text-right p-3">金额(¥)</th>
              <th className="text-right p-3">游戏币</th>
              <th className="text-center p-3">状态</th>
              <th className="text-left p-3">创建时间</th>
              <th className="text-left p-3">支付时间</th>
              <th className="text-center p-3">操作</th>
            </tr>
          </thead>
          <tbody>
            {list.map((o) => (
              <tr key={o.out_trade_no} className="border-b border-gray-800/50 hover:bg-dark-200/50">
                <td className="p-3 font-mono text-gray-400 text-xs">{o.out_trade_no}</td>
                <td className="p-3 text-gray-300">{o.user_id}</td>
                <td className="p-3 text-gray-400">{o.channel}</td>
                <td className="p-3 text-right font-mono text-gray-300">{fmt(o.amount_rmb)}</td>
                <td className="p-3 text-right font-mono text-gold">{fmt(o.game_coins)}</td>
                <td className="p-3 text-center">{statusText(o.status)}</td>
                <td className="p-3 text-gray-500 text-xs">{new Date(o.created_at).toLocaleString()}</td>
                <td className="p-3 text-gray-500 text-xs">{o.paid_at ? new Date(o.paid_at).toLocaleString() : '-'}</td>
                <td className="p-3 text-center">
                  {o.status === 'pending' ? (
                    <button onClick={() => onCredit(o.out_trade_no)} className="text-xs text-gold hover:text-yellow-300">
                      补单入账
                    </button>
                  ) : (
                    <span className="text-gray-600 text-xs">-</span>
                  )}
                </td>
              </tr>
            ))}
            {list.length === 0 && (
              <tr>
                <td colSpan={9} className="p-8 text-center text-gray-500 text-sm">
                  暂无支付订单
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
