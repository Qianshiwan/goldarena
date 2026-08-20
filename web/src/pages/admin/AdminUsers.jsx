import { useEffect, useState } from 'react'
import { adminAPI } from '../../services/api'

function fmt(n) {
  return (n ?? 0).toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

export default function AdminUsers() {
  const [list, setList] = useState([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [keyword, setKeyword] = useState('')
  const [editing, setEditing] = useState(null)
  const [amount, setAmount] = useState('')
  const [remark, setRemark] = useState('')

  const load = async () => {
    try {
      const { data } = await adminAPI.listUsers({ page, size: 20, keyword })
      setList(data.data.list || [])
      setTotal(data.data.total || 0)
    } catch {}
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, keyword])

  const onAdjust = async () => {
    const amt = parseFloat(amount)
    if (!amt) {
      alert('请输入金额')
      return
    }
    try {
      await adminAPI.adjustBalance(editing.id, { amount: amt, remark })
      alert('调整成功')
      setEditing(null)
      setAmount('')
      setRemark('')
      load()
    } catch (e) {
      alert('失败: ' + (e.response?.data?.message || '未知错误'))
    }
  }

  const onToggleStatus = async (u) => {
    if (!confirm(u.status === 1 ? '确认冻结该账号？' : '确认解冻该账号？')) return
    try {
      await adminAPI.setStatus(u.id, { status: u.status === 1 ? 0 : 1 })
      load()
    } catch {
      alert('操作失败')
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / 20))

  return (
    <div>
      <h2 className="text-lg font-semibold text-gray-200 mb-4">用户管理</h2>

      <div className="flex gap-2 mb-4">
        <input
          value={keyword}
          onChange={(e) => {
            setKeyword(e.target.value)
            setPage(1)
          }}
          placeholder="搜索用户名 / 邮箱 / 昵称"
          className="bg-dark-300 border border-gray-700 rounded px-3 py-1.5 text-sm text-gray-200 w-64"
        />
        <button onClick={load} className="btn-gold text-sm px-4 py-1.5">
          搜索
        </button>
      </div>

      <div className="trade-card overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="text-gray-500 text-xs border-b border-gray-800">
              <th className="text-left p-3">ID</th>
              <th className="text-left p-3">用户名</th>
              <th className="text-left p-3">昵称</th>
              <th className="text-left p-3">邮箱</th>
              <th className="text-left p-3">角色</th>
              <th className="text-right p-3">余额</th>
              <th className="text-right p-3">冻结</th>
              <th className="text-center p-3">状态</th>
              <th className="text-center p-3">操作</th>
            </tr>
          </thead>
          <tbody>
            {list.map((u) => (
              <tr key={u.id} className="border-b border-gray-800/50 hover:bg-dark-200/50">
                <td className="p-3 text-gray-500">{u.id}</td>
                <td className="p-3 font-mono text-gray-300">{u.username}</td>
                <td className="p-3 text-gray-300">{u.nickname}</td>
                <td className="p-3 text-gray-500">{u.email}</td>
                <td className="p-3 text-gray-400">{u.role}</td>
                <td className="p-3 text-right font-mono text-gray-200">{fmt(u.balance)}</td>
                <td className="p-3 text-right font-mono text-gray-500">{fmt(u.frozen)}</td>
                <td className="p-3 text-center">
                  {u.status === 1 ? (
                    <span className="text-green-400 text-xs">正常</span>
                  ) : (
                    <span className="text-red-400 text-xs">已冻结</span>
                  )}
                </td>
                <td className="p-3 text-center whitespace-nowrap">
                  <button
                    onClick={() => {
                      setEditing(u)
                      setAmount('')
                      setRemark('')
                    }}
                    className="text-xs text-gold hover:text-yellow-300 mr-2"
                  >
                    调余额
                  </button>
                  <button
                    onClick={() => onToggleStatus(u)}
                    className={`text-xs ${u.status === 1 ? 'text-red-400 hover:text-red-300' : 'text-green-400 hover:text-green-300'}`}
                  >
                    {u.status === 1 ? '冻结' : '解冻'}
                  </button>
                </td>
              </tr>
            ))}
            {list.length === 0 && (
              <tr>
                <td colSpan={9} className="p-8 text-center text-gray-500 text-sm">
                  暂无用户
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      <div className="flex items-center justify-between mt-3 text-xs text-gray-500">
        <span>共 {total} 人</span>
        <div className="flex gap-2">
          <button
            disabled={page <= 1}
            onClick={() => setPage((p) => p - 1)}
            className="px-3 py-1 border border-gray-700 rounded disabled:opacity-40"
          >
            上一页
          </button>
          <span>
            {page} / {totalPages}
          </span>
          <button
            disabled={page >= totalPages}
            onClick={() => setPage((p) => p + 1)}
            className="px-3 py-1 border border-gray-700 rounded disabled:opacity-40"
          >
            下一页
          </button>
        </div>
      </div>

      {editing && (
        <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-50">
          <div className="bg-dark-300 border border-gray-700 rounded-lg p-5 w-96">
            <h3 className="text-gray-200 font-semibold mb-3">调整余额 - {editing.username}</h3>
            <p className="text-xs text-gray-500 mb-3">
              当前余额：{fmt(editing.balance)}（正数为发放，负数为扣减）
            </p>
            <input
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              type="number"
              placeholder="金额（游戏币）"
              className="w-full bg-dark-200 border border-gray-700 rounded px-3 py-2 text-sm text-gray-200 mb-3"
            />
            <input
              value={remark}
              onChange={(e) => setRemark(e.target.value)}
              placeholder="备注（选填）"
              className="w-full bg-dark-200 border border-gray-700 rounded px-3 py-2 text-sm text-gray-200 mb-4"
            />
            <div className="flex justify-end gap-2">
              <button
                onClick={() => setEditing(null)}
                className="px-4 py-1.5 text-sm text-gray-400 hover:text-gray-200"
              >
                取消
              </button>
              <button onClick={onAdjust} className="btn-gold text-sm px-4 py-1.5">
                确认调整
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
