import { useEffect, useRef, useState, useCallback } from 'react'
import { messageAPI } from '../services/api'

const fmtTime = (t) => {
  if (!t) return ''
  const d = new Date(t)
  const now = new Date()
  const sameDay = d.toDateString() === now.toDateString()
  const hm = d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
  if (sameDay) return hm
  return `${d.toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' })} ${hm}`
}

// 应用内留言：平台 ↔ 用户双向会话
export default function MessagesPage() {
  const [messages, setMessages] = useState([])
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const bottomRef = useRef(null)
  const pollRef = useRef(null)

  const scrollToBottom = () => {
    setTimeout(() => bottomRef.current?.scrollIntoView({ behavior: 'smooth' }), 50)
  }

  const load = useCallback(async (scroll) => {
    try {
      const { data } = await messageAPI.list()
      setMessages(data.data || [])
      if (scroll) scrollToBottom()
    } catch {}
    finally { setLoaded(true) }
  }, [])

  useEffect(() => {
    load(true)
    // 轮询刷新：对方（平台）可能有新回复
    pollRef.current = setInterval(() => load(false), 5000)
    return () => clearInterval(pollRef.current)
  }, [load])

  // 新消息到达（数量变化）时滚动到底部
  const countRef = useRef(0)
  useEffect(() => {
    if (messages.length !== countRef.current) {
      countRef.current = messages.length
      scrollToBottom()
    }
  }, [messages.length])

  const send = async () => {
    const content = input.trim()
    if (!content || sending) return
    setSending(true)
    try {
      await messageAPI.send(content)
      setInput('')
      await load(true)
    } catch (e) {
      alert(e?.response?.data?.message || '发送失败，请稍后再试')
    } finally {
      setSending(false)
    }
  }

  return (
    <div className="max-w-3xl mx-auto space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-bold gold-gradient">平台留言</h1>
        <span className="text-xs text-gray-500">有任何问题或建议，欢迎随时给我们留言</span>
      </div>

      <div className="trade-card flex flex-col h-[65vh]">
        {/* 会话头部 */}
        <div className="px-4 py-3 border-b border-gray-800 flex items-center gap-2">
          <span className="text-2xl">🥇</span>
          <div>
            <div className="text-sm font-semibold text-gold">金归子平台</div>
            <div className="text-xs text-gray-500">工作时间内会尽快回复您的留言</div>
          </div>
        </div>

        {/* 消息列表 */}
        <div className="flex-1 overflow-y-auto px-4 py-4 space-y-3">
          {!loaded && <div className="text-center text-gray-500 text-sm py-8">加载中…</div>}
          {loaded && messages.length === 0 && (
            <div className="text-center text-gray-500 text-sm py-8">
              暂无留言，发送第一条消息开始对话吧
            </div>
          )}
          {messages.map((m) => (
            <div key={m.id} className={`flex ${m.sender === 'user' ? 'justify-end' : 'justify-start'}`}>
              <div className={`max-w-[75%] rounded-2xl px-4 py-2.5 ${
                m.sender === 'user'
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

        {/* 输入区 */}
        <div className="p-3 border-t border-gray-800 flex gap-2">
          <textarea
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send() }
            }}
            placeholder="输入留言内容…（Enter 发送，Shift+Enter 换行）"
            rows={2}
            maxLength={2000}
            className="flex-1 bg-dark-400 border border-gray-700 rounded-lg px-3 py-2 text-sm text-gray-200
              placeholder-gray-600 focus:outline-none focus:border-gold/60 resize-none"
          />
          <button
            onClick={send}
            disabled={sending || !input.trim()}
            className="btn-gold px-5 rounded-lg text-sm font-semibold disabled:opacity-40 self-end"
          >
            {sending ? '发送中…' : '发送'}
          </button>
        </div>
      </div>

      <p className="text-xs text-gray-600 text-center">页面每 5 秒自动刷新，收到平台回复会即时显示</p>
    </div>
  )
}
