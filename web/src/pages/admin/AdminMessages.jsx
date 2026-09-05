import { useEffect, useRef, useState, useCallback } from 'react'
import { messageAPI } from '../../services/api'

const fmtTime = (t) => {
  if (!t) return ''
  const d = new Date(t)
  return `${d.toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' })} ${d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })}`
}

const lastMsgPreview = (s, n = 24) => (s?.length > n ? s.slice(0, n) + '…' : s || '')

// 管理后台·应用内留言管理：左侧会话列表 + 右侧与单个用户的会话
export default function AdminMessages() {
  const [conversations, setConversations] = useState([])
  const [activeUser, setActiveUser] = useState(null) // { user_id, username }
  const [thread, setThread] = useState([])
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const bottomRef = useRef(null)

  const loadConversations = useCallback(async () => {
    try {
      const { data } = await messageAPI.adminConversations()
      setConversations(data.data || [])
    } catch {}
  }, [])

  const loadThread = useCallback(async (userId, scroll = true) => {
    try {
      const { data } = await messageAPI.adminThread(userId)
      setThread(data.data || [])
      if (scroll) setTimeout(() => bottomRef.current?.scrollIntoView({ behavior: 'smooth' }), 50)
    } catch {}
  }, [])

  useEffect(() => {
    loadConversations()
    const t = setInterval(loadConversations, 10000)
    return () => clearInterval(t)
  }, [loadConversations])

  // 已选中的会话持续轮询
  useEffect(() => {
    if (!activeUser) return
    loadThread(activeUser.user_id, true)
    const t = setInterval(() => loadThread(activeUser.user_id, false), 5000)
    return () => clearInterval(t)
  }, [activeUser, loadThread])

  const openUser = (c) => setActiveUser({ user_id: c.user_id, username: c.username })

  const reply = async () => {
    const content = input.trim()
    if (!content || !activeUser || sending) return
    setSending(true)
    try {
      await messageAPI.adminReply(activeUser.user_id, content)
      setInput('')
      await loadThread(activeUser.user_id, true)
      loadConversations()
    } catch (e) {
      alert(e?.response?.data?.message || '回复失败')
    } finally {
      setSending(false)
    }
  }

  const totalUnread = conversations.reduce((s, c) => s + (c.unread || 0), 0)

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-bold gold-gradient">留言管理</h1>
        <span className="text-xs text-gray-500">
          共 {conversations.length} 个会话{totalUnread > 0 && <span className="text-red-400 font-semibold"> · {totalUnread} 条未读</span>}
        </span>
      </div>

      <div className="trade-card flex h-[70vh]">
        {/* 左侧会话列表 */}
        <div className="w-72 border-r border-gray-800 overflow-y-auto shrink-0">
          {conversations.length === 0 && (
            <div className="text-center text-gray-500 text-sm py-10">暂无用户留言</div>
          )}
          {conversations.map((c) => (
            <button
              key={c.user_id}
              onClick={() => openUser(c)}
              className={`w-full text-left px-3 py-3 border-b border-gray-800/60 transition-colors ${
                activeUser?.user_id === c.user_id ? 'bg-gold/10' : 'hover:bg-dark-200'
              }`}
            >
              <div className="flex items-center justify-between gap-2">
                <span className={`text-sm truncate ${c.unread > 0 ? 'text-gold font-semibold' : 'text-gray-300'}`}>
                  {c.username || `用户#${c.user_id}`}
                </span>
                {c.unread > 0 && (
                  <span className="bg-red-500 text-white text-[10px] rounded-full px-1.5 py-0.5 shrink-0">{c.unread}</span>
                )}
              </div>
              <div className="text-xs text-gray-500 truncate mt-1">{lastMsgPreview(c.last_content)}</div>
              <div className="text-[10px] text-gray-600 mt-0.5">{fmtTime(c.last_at)}</div>
            </button>
          ))}
        </div>

        {/* 右侧会话区 */}
        {!activeUser ? (
          <div className="flex-1 flex items-center justify-center text-gray-500 text-sm">
            ← 从左侧选择一个用户会话
          </div>
        ) : (
          <div className="flex-1 flex flex-col min-w-0">
            <div className="px-4 py-3 border-b border-gray-800 flex items-center gap-2">
              <span className="text-sm font-semibold text-gold">{activeUser.username || `用户#${activeUser.user_id}`}</span>
              <span className="text-xs text-gray-500">ID: {activeUser.user_id}</span>
            </div>

            <div className="flex-1 overflow-y-auto px-4 py-4 space-y-3">
              {thread.length === 0 && <div className="text-center text-gray-500 text-sm py-8">暂无消息</div>}
              {thread.map((m) => (
                <div key={m.id} className={`flex ${m.sender === 'platform' ? 'justify-end' : 'justify-start'}`}>
                  <div className={`max-w-[70%] rounded-2xl px-4 py-2.5 ${
                    m.sender === 'platform'
                      ? 'bg-gold/20 text-gray-100 rounded-br-sm'
                      : 'bg-dark-400 text-gray-200 rounded-bl-sm'
                  }`}>
                    <div className="text-sm whitespace-pre-wrap break-words leading-relaxed">{m.content}</div>
                    <div className="text-[10px] text-gray-500 mt-1 text-right">{fmtTime(m.created_at)}</div>
                  </div>
                </div>
              ))}
              <div ref={bottomRef} />
            </div>

            <div className="p-3 border-t border-gray-800 flex gap-2">
              <textarea
                value={input}
                onChange={(e) => setInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); reply() }
                }}
                placeholder="以平台身份回复…（Enter 发送，Shift+Enter 换行）"
                rows={2}
                maxLength={2000}
                className="flex-1 bg-dark-400 border border-gray-700 rounded-lg px-3 py-2 text-sm text-gray-200
                  placeholder-gray-600 focus:outline-none focus:border-gold/60 resize-none"
              />
              <button
                onClick={reply}
                disabled={sending || !input.trim()}
                className="btn-gold px-5 rounded-lg text-sm font-semibold disabled:opacity-40 self-end"
              >
                {sending ? '发送中…' : '回复'}
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
