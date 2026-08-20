import { useEffect, useState } from 'react'
import { userAPI, paymentAPI, tradeAPI } from '../services/api'

export default function WalletPage() {
  const [wallet, setWallet] = useState(null)
  const [transactions, setTransactions] = useState([])
  const [txnTotal, setTxnTotal] = useState(0)
  const [txnPage, setTxnPage] = useState(1)
  const [txnPageSize, setTxnPageSize] = useState(20)
  const [closedPositions, setClosedPositions] = useState([])
  const [closedTotal, setClosedTotal] = useState(0)
  const [closedPage, setClosedPage] = useState(1)
  const [closedPageSize, setClosedPageSize] = useState(20)
  const [tradePnL, setTradePnL] = useState(null)
  const [amount, setAmount] = useState(10)
  const [channel, setChannel] = useState('wxpay')
  const [msg, setMsg] = useState('')
  const [modal, setModal] = useState(null) // { order, sandbox, qr_content, pay_url }
  const [pollId, setPollId] = useState(null)

  useEffect(() => {
    loadAll()
    return () => {
      if (pollId) clearInterval(pollId)
    }
  }, [])

  const loadAll = async () => {
    try {
      const [wRes] = await Promise.all([userAPI.getWallet()])
      if (wRes.data.data) setWallet(wRes.data.data)
    } catch {}
    await Promise.all([loadTransactions(1), loadClosed(1), loadPnL()])
  }

  // 分页加载交易流水
  const loadTransactions = async (page) => {
    try {
      const { data } = await userAPI.getTransactions({ page, page_size: txnPageSize })
      const d = data.data
      setTransactions(d.list || [])
      setTxnTotal(d.total || 0)
      setTxnPage(page)
    } catch {}
  }

  // 分页加载已平仓记录
  const loadClosed = async (page) => {
    try {
      const { data } = await tradeAPI.getClosed({ page, page_size: closedPageSize })
      const d = data.data
      setClosedPositions(d.list || [])
      setClosedTotal(d.total || 0)
      setClosedPage(page)
    } catch {}
  }

  // 完整加载（交易日历 + 累计盈亏需要全部已平仓数据）
  const loadPnL = async () => {
    try {
      const { data } = await tradeAPI.getPnL()
      if (data.data) setTradePnL(data.data)
    } catch {}
  }

  const openPayment = async () => {
    if (amount < 10) {
      setMsg('最低充值10元')
      return
    }
    setMsg('')
    try {
      const { data } = await paymentAPI.create(amount, channel)
      const o = data.data.order
      setModal({
        order: o,
        sandbox: data.data.sandbox,
        qr_content: data.data.qr_content,
        pay_url: data.data.pay_url,
      })
      startPolling(o.out_trade_no)
    } catch (err) {
      setMsg('创建支付失败: ' + (err.response?.data?.message || '未知错误'))
    }
  }

  const startPolling = (outTradeNo) => {
    if (pollId) clearInterval(pollId)
    const id = setInterval(async () => {
      try {
        const { data } = await paymentAPI.orders()
        const o = (data.data || []).find((x) => x.out_trade_no === outTradeNo)
        if (o && o.status === 'paid') {
          clearInterval(id)
          setPollId(null)
          setModal(null)
          setMsg(`支付成功! +${Math.floor(o.game_coins).toLocaleString()} 游戏币已到账`)
          loadAll()
        }
      } catch {}
    }, 2500)
    setPollId(id)
  }

  const simulate = async () => {
    if (!modal) return
    try {
      await paymentAPI.simulate(modal.order.out_trade_no)
      setMsg(`支付成功! +${Math.floor(modal.order.game_coins).toLocaleString()} 游戏币已到账`)
      setModal(null)
      loadAll()
    } catch (err) {
      setMsg('模拟支付失败: ' + (err.response?.data?.message || '未知错误'))
    }
  }

  const closeModal = () => {
    if (pollId) clearInterval(pollId)
    setPollId(null)
    setModal(null)
  }

  return (
    <div className="max-w-4xl mx-auto space-y-6">
      <h1 className="text-xl font-bold gold-gradient">我的钱包</h1>

      {/* Wallet Card */}
      <div className="trade-card p-6">
        <div className="grid grid-cols-2 md:grid-cols-4 gap-6">
          <div>
            <div className="text-gray-500 text-xs mb-1">可用余额</div>
            <div className="text-2xl font-mono font-bold text-white">
              {wallet ? Math.floor(wallet.balance).toLocaleString() : '—'}
            </div>
            <div className="text-xs text-gray-500 mt-0.5">游戏币 (Gold Coins)</div>
          </div>
          <div>
            <div className="text-gray-500 text-xs mb-1">冻结保证金</div>
            <div className="text-2xl font-mono font-bold text-orange-400">
              {wallet ? Math.floor(wallet.frozen || 0).toLocaleString() : '—'}
            </div>
            <div className="text-xs text-gray-500 mt-0.5">开仓中</div>
          </div>
          <div>
            <div className="text-gray-500 text-xs mb-1">累计充值</div>
            <div className="text-2xl font-mono font-bold text-gray-300">
              {wallet ? Math.floor(wallet.total_recharged || 0).toLocaleString() : '—'}
            </div>
            <div className="text-xs text-gray-500 mt-0.5">元</div>
          </div>
          <div>
            <div className="text-gray-500 text-xs mb-1">汇率</div>
            <div className="text-2xl font-mono font-bold text-gold">
              ¥1 = 1,000 GC
            </div>
            <div className="text-xs text-gray-500 mt-0.5">10元起充</div>
          </div>
        </div>
      </div>

      {/* Recharge (真实支付) */}
      <div className="trade-card p-4">
        <h3 className="text-sm font-semibold text-gray-300 mb-3">充值游戏币（真实支付）</h3>
        <div className="flex flex-wrap items-center gap-3">
          <input
            type="number"
            min="10"
            step="10"
            value={amount}
            onChange={(e) => setAmount(parseInt(e.target.value) || 10)}
            className="w-32"
          />
          <span className="text-gray-400 text-sm">元</span>
          <span className="text-gray-500 text-sm">= {(amount * 1000).toLocaleString()} 游戏币</span>

          {/* Channel selector */}
          <div className="flex gap-2 ml-2">
            {[
              ['wxpay', '微信支付'],
              ['alipay', '支付宝'],
            ].map(([val, label]) => (
              <button
                key={val}
                onClick={() => setChannel(val)}
                className={`px-3 py-1.5 text-xs rounded-lg border transition-colors ${
                  channel === val
                    ? 'bg-gold text-black border-gold font-semibold'
                    : 'text-gray-400 border-gray-700 hover:border-gray-500'
                }`}
              >
                {label}
              </button>
            ))}
          </div>

          <button onClick={openPayment} className="btn-gold text-sm px-4 py-2 ml-2">
            去支付
          </button>
        </div>

        {/* Quick amounts */}
        <div className="flex flex-wrap gap-2 mt-3">
          {[10, 50, 100, 200, 500].map((v) => (
            <button
              key={v}
              onClick={() => setAmount(v)}
              className="px-3 py-1 text-xs rounded bg-dark-200 text-gray-400 hover:text-gold border border-gray-700"
            >
              ¥{v}
            </button>
          ))}
        </div>

        {msg && (
          <div className={`mt-3 p-2 rounded text-xs ${
            msg.includes('成功')
              ? 'bg-green-900/30 text-green-400'
              : 'bg-red-900/30 text-red-400'
          }`}>
            {msg}
          </div>
        )}
      </div>

      {/* Transactions */}
      <div className="trade-card">
        <div className="p-3 border-b border-gray-800">
          <h3 className="text-sm font-semibold text-gray-300">交易记录</h3>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-gray-500 text-xs border-b border-gray-800">
                <th className="text-left p-3">类型</th>
                <th className="text-right p-3">金额</th>
                <th className="text-right p-3">变动前</th>
                <th className="text-right p-3">变动后</th>
                <th className="text-left p-3">备注</th>
                <th className="text-right p-3">时间</th>
              </tr>
            </thead>
            <tbody>
              {transactions.map((t) => {
                const isIncome = [
                  'recharge', 'bonus', 'margin_release', 'pnl_credit', 'contest_reward',
                ].includes(t.type)
                return (
                  <tr key={t.id} className="border-b border-gray-800/50 hover:bg-dark-200/50">
                    <td className="p-3">
                      <span className={`px-2 py-0.5 rounded text-xs ${
                        isIncome ? 'bg-green-900/30 text-green-400' : 'bg-red-900/30 text-red-400'
                      }`}>
                        {t.type}
                      </span>
                    </td>
                    <td className={`text-right p-3 font-mono ${isIncome ? 'price-up' : 'price-down'}`}>
                      {isIncome ? '+' : '-'}{Math.abs(t.amount).toLocaleString()}
                    </td>
                    <td className="text-right p-3 font-mono text-gray-400">{Math.floor(t.balance_before).toLocaleString()}</td>
                    <td className="text-right p-3 font-mono text-gray-300">{Math.floor(t.balance_after).toLocaleString()}</td>
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
                    暂无交易记录
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

      {/* 已平仓交易记录 (按时间从近到远) */}
      <div className="trade-card">
        <div className="p-3 border-b border-gray-800 flex items-center justify-between">
          <h3 className="text-sm font-semibold text-gray-300">已平仓记录</h3>
          {tradePnL && (
            <span className={`text-xs font-mono font-bold ${tradePnL.total_pnl >= 0 ? 'text-green-400' : 'text-red-400'}`}>
              累计盈亏: {tradePnL.total_pnl >= 0 ? '+' : ''}{tradePnL.total_pnl.toFixed(2)}
            </span>
          )}
        </div>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-gray-500 text-xs border-b border-gray-800">
                <th className="text-left p-3">品种</th>
                <th className="text-left p-3">方向</th>
                <th className="text-right p-3">手数</th>
                <th className="text-right p-3">杠杆</th>
                <th className="text-right p-3">开仓价</th>
                <th className="text-right p-3">平仓价</th>
                <th className="text-right p-3">盈亏</th>
                <th className="text-right p-3">开仓时间</th>
                <th className="text-right p-3">平仓时间</th>
              </tr>
            </thead>
            <tbody>
              {closedPositions.map((t) => {
                const pnlColor = (t.pnl || 0) >= 0 ? 'price-up' : 'price-down'
                return (
                  <tr key={t.id || Math.random()} className="border-b border-gray-800/50 hover:bg-dark-200/50">
                    <td className="p-3 font-mono text-gray-300">{t.symbol || 'XAU'}</td>
                    <td className="p-3">
                      <span className={`px-2 py-0.5 rounded text-xs font-bold ${
                        t.direction === 1 ? 'bg-green-900/30 text-green-400' : 'bg-red-900/30 text-red-400'
                      }`}>
                        {t.direction === 1 ? '做多' : '做空'}
                      </span>
                    </td>
                    <td className="text-right p-3 font-mono text-gray-300">{t.volume || '—'}</td>
                    <td className="text-right p-3 text-gray-400">{t.leverage ? `${t.leverage}x` : '—'}</td>
                    <td className="text-right p-3 font-mono text-gray-300">{t.open_price?.toFixed(2) || '—'}</td>
                    <td className="text-right p-3 font-mono text-gray-300">{t.close_price?.toFixed(2) || '—'}</td>
                    <td className={`text-right p-3 font-mono ${pnlColor}`}>
                      {(t.pnl || 0) >= 0 ? '+' : ''}{(t.pnl || 0).toFixed(2)}
                    </td>
                    <td className="text-right p-3 text-xs text-gray-500">
                      {t.created_at ? new Date(t.created_at).toLocaleString('zh-CN') : '—'}
                    </td>
                    <td className="text-right p-3 text-xs text-gray-500">
                      {t.closed_at ? new Date(t.closed_at).toLocaleString('zh-CN') : '—'}
                    </td>
                  </tr>
                )
              })}
              {closedPositions.length === 0 && (
                <tr>
                  <td colSpan={9} className="p-8 text-center text-gray-500 text-sm">
                    暂无已平仓记录
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
        {closedTotal > 0 && (
          <div className="p-3 flex items-center justify-between border-t border-gray-800">
            <span className="text-xs text-gray-500">共 {closedTotal} 条已平仓记录</span>
            <Pagination
              total={closedTotal}
              page={closedPage}
              pageSize={closedPageSize}
              onChange={(p) => loadClosed(p)}
            />
          </div>
        )}
      </div>

      {/* 交易日历 (每日盈亏汇总，使用完整已平仓数据) */}
      <TradeCalendar closedPositions={tradePnL?.trades || []} />

      {/* Payment modal */}
      {modal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70" onClick={closeModal}>
          <div
            className="trade-card p-6 w-80 text-center"
            onClick={(e) => e.stopPropagation()}
          >
            <h3 className="text-base font-semibold text-gray-200 mb-1">
              {modal.order.channel === 'wxpay' ? '微信支付' : '支付宝'}扫码
            </h3>
            <p className="text-xs text-gray-500 mb-4">
              ¥{modal.order.amount_rmb} → {Math.floor(modal.order.game_coins).toLocaleString()} 游戏币
            </p>

            {/* QR code - 真实收款码或动态二维码 */}
            <div className="flex justify-center mb-3">
              {modal.sandbox ? (
                <img
                  src={paymentAPI.qrURL(modal.qr_content)}
                  alt="pay qr"
                  className="w-48 h-48 bg-white rounded-lg p-2"
                />
              ) : (
                <img
                  src="/wechat-pay-qr.jpg"
                  alt="微信收款码"
                  className="w-52 h-52 rounded-lg shadow-lg"
                />
              )}
            </div>

            {modal.sandbox ? (
              <p className="text-xs text-yellow-400/80 mb-3">
                沙箱模式：无真实商户，用下方按钮模拟支付成功
              </p>
            ) : (
              <div className="space-y-2">
                <p className="text-xs text-gray-400 mb-2">
                  请使用微信扫描上方收款码转账 ¥{modal.order.amount_rmb}，备注填手机号后4位
                </p>
                <p className="text-xs text-yellow-400/80">
                  转账后系统会自动确认并充值 {Math.floor(modal.order.game_coins).toLocaleString()} 游戏币
                </p>
              </div>
            )}

            {modal.pay_url && (
              <a
                href={modal.pay_url}
                target="_blank"
                rel="noreferrer"
                className="text-xs text-gold underline break-all block mb-3"
              >
                或点击此处打开支付链接
              </a>
            )}

            {modal.sandbox && (
              <button
                onClick={simulate}
                className="btn-gold text-sm px-4 py-2 w-full mb-2"
              >
                模拟支付成功
              </button>
            )}

            <button
              onClick={closeModal}
              className="text-xs text-gray-500 hover:text-gray-300 w-full"
            >
              取消
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

// ========== 分页组件 ==========
function Pagination({ total, page, pageSize, onChange }) {
  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  if (totalPages <= 1) return null

  // 计算需要显示的页码（当前页前后各 2 页）
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
      <span className="text-xs text-gray-500 ml-2">
        {page}/{totalPages} 页
      </span>
    </div>
  )
}

// ========== 交易日历组件 ==========
function TradeCalendar({ closedPositions }) {
  // 按日期聚合每日盈亏 (用平仓时间 closed_at，没有则用开仓时间 created_at)
  const dailyMap = {}
  ;(closedPositions || []).forEach((t) => {
    const dateStr = t.closed_at
      ? new Date(t.closed_at).toLocaleDateString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' })
      : (t.created_at ? new Date(t.created_at).toLocaleDateString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' }) : null)
    if (!dateStr) return
    if (!dailyMap[dateStr]) dailyMap[dateStr] = { pnl: 0, count: 0 }
    dailyMap[dateStr].pnl += t.pnl || 0
    dailyMap[dateStr].count += 1
  })

  const days = Object.entries(dailyMap)
    .sort(([a], [b]) => {
      // 解析 "2026/8/19" 格式排序
      const parseDate = (s) => new Date(s.replace(/\//g, '-'))
      return parseDate(b) - parseDate(a)
    })

  const totalPnL = days.reduce((s, [, d]) => s + d.pnl, 0)
  const winDays = days.filter(([, d]) => d.pnl > 0).length
  const lossDays = days.filter(([, d]) => d.pnl < 0).length

  // 计算最大盈亏用于条形图比例
  const maxAbsPnl = Math.max(...days.map(([, d]) => Math.abs(d.pnl)), 1)

  return (
    <div className="trade-card">
      <div className="p-3 border-b border-gray-800 flex items-center justify-between">
        <h3 className="text-sm font-semibold text-gray-300">交易日历</h3>
        <div className="flex gap-3 text-xs">
          <span className="text-gray-500">交易 {days.length} 天</span>
          <span className="text-green-400">盈利 {winDays} 天</span>
          <span className="text-red-400">亏损 {lossDays} 天</span>
          <span className={`font-mono font-bold ${totalPnL >= 0 ? 'text-green-400' : 'text-red-400'}`}>
            合计 {totalPnL >= 0 ? '+' : ''}{totalPnL.toFixed(2)}
          </span>
        </div>
      </div>

      {days.length === 0 ? (
        <div className="p-8 text-center text-gray-500 text-sm">
          暂无交易记录，完成交易后此处显示每日盈亏汇总
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-gray-500 text-xs border-b border-gray-800">
                <th className="text-left p-3">日期</th>
                <th className="text-right p-3">笔数</th>
                <th className="text-right p-3">盈亏</th>
                <th className="text-left p-3">盈亏分布</th>
              </tr>
            </thead>
            <tbody>
              {days.map(([date, data]) => {
                const isWin = data.pnl > 0
                const barPct = Math.abs(data.pnl) / maxAbsPnl * 100
                return (
                  <tr key={date} className="border-b border-gray-800/50 hover:bg-dark-200/50">
                    <td className="p-3 text-gray-300 text-sm">{date}</td>
                    <td className="text-right p-3 font-mono text-gray-400">{data.count}</td>
                    <td className={`text-right p-3 font-mono font-bold ${isWin ? 'text-green-400' : data.pnl < 0 ? 'text-red-400' : 'text-gray-500'}`}>
                      {isWin ? '+' : ''}{data.pnl.toFixed(2)}
                    </td>
                    <td className="p-3 w-40">
                      <div className="h-2.5 bg-gray-700/50 rounded-full overflow-hidden flex">
                        {data.pnl > 0 && (
                          <div className="h-full bg-green-500 rounded-l-full" style={{ width: `${barPct}%` }} />
                        )}
                        {data.pnl < 0 && (
                          <div className="h-full bg-red-500 rounded-r-full ml-auto" style={{ width: `${barPct}%` }} />
                        )}
                        {data.pnl === 0 && (
                          <div className="h-full bg-gray-500 w-2 mx-auto rounded-full" />
                        )}
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
