import { useState, useRef, useEffect } from 'react'
import { authAPI } from '../services/api'
import SliderCaptcha from './SliderCaptcha'

export default function ForgotPassword({ onClose }) {
  const [email, setEmail] = useState('')
  const [code, setCode] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [error, setError] = useState('')
  const [info, setInfo] = useState('')
  const [loading, setLoading] = useState(false)
  const [codeLoading, setCodeLoading] = useState(false)
  const [cooldown, setCooldown] = useState(0)
  const [captchaTicket, setCaptchaTicket] = useState('')
  const [captchaOk, setCaptchaOk] = useState(false)
  const [recovered, setRecovered] = useState(null) // {username, nickname}
  const timerRef = useRef(null)

  useEffect(() => () => clearInterval(timerRef.current), [])

  // 找回流程进行中（已输入任意内容 / 已发码 / 已成功）时，禁止误触背景遮罩
  // 直接关闭弹窗、丢失进度；只有完全空白时才允许点背景关闭，否则必须用 × 按钮。
  const handleBackdropClose = () => {
    const pristine = !email && !code && !newPassword && cooldown === 0 && !recovered
    if (pristine) onClose()
  }

  const startCooldown = () => {
    setCooldown(60)
    clearInterval(timerRef.current)
    timerRef.current = setInterval(() => {
      setCooldown((c) => {
        if (c <= 1) {
          clearInterval(timerRef.current)
          return 0
        }
        return c - 1
      })
    }, 1000)
  }

  const handleSendCode = async () => {
    setError('')
    setInfo('')
    if (!captchaOk || !captchaTicket) {
      setError('请先完成滑块拼图验证')
      return
    }
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
      setError('请输入有效的邮箱地址')
      return
    }
    setCodeLoading(true)
    try {
      const { data } = await authAPI.sendResetCode(email, captchaTicket)
      const d = data.data
      setInfo('验证码已发送，请查收邮箱')
      if (d.dev_code) {
        setInfo(`开发模式验证码：${d.dev_code}（生产环境请查收邮箱）`)
      }
      setCaptchaOk(false)
      setCaptchaTicket('')
      startCooldown()
    } catch (err) {
      setError(err.response?.data?.message || '发送验证码失败')
    } finally {
      setCodeLoading(false)
    }
  }

  const handleSubmit = async (e) => {
    e.preventDefault()
    setError('')
    if (newPassword.length < 6) {
      setError('新密码至少6位')
      return
    }
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
      setError('请输入有效的邮箱地址')
      return
    }
    if (code.length !== 6) {
      setError('请输入6位邮箱验证码')
      return
    }
    setLoading(true)
    try {
      const { data } = await authAPI.resetPassword({ email, code, new_password: newPassword })
      setRecovered({ username: data.data.username, nickname: data.data.nickname })
    } catch (err) {
      setError(err.response?.data?.message || '重置失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" onClick={handleBackdropClose}>
      <div
        className="trade-card p-8 w-full max-w-md"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-bold gold-gradient">找回账号 / 重置密码</h2>
          <button onClick={onClose} className="text-gray-500 hover:text-gray-300 text-xl leading-none">×</button>
        </div>

        {recovered ? (
          <div className="space-y-4">
            <div className="p-3 rounded text-sm bg-green-900/30 text-green-400">
              密码重置成功！您的登录账号已找回：
            </div>
            <div className="text-center py-2">
              <p className="text-gray-400 text-sm">用户名（登录账号）</p>
              <p className="text-2xl font-bold text-gold mt-1">{recovered.username}</p>
              {recovered.nickname && recovered.nickname !== recovered.username && (
                <p className="text-gray-500 text-sm mt-1">昵称：{recovered.nickname}</p>
              )}
            </div>
            <p className="text-gray-500 text-sm text-center">
              现在可以用该用户名和新密码登录了。
            </p>
            <button onClick={onClose} className="btn-gold w-full py-2.5 text-sm">去登录</button>
          </div>
        ) : (
          <form onSubmit={handleSubmit} className="space-y-4">
            <div>
              <label className="text-gray-400 text-sm">注册邮箱</label>
              <input
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                className="w-full"
                placeholder="请输入注册时使用的邮箱"
                required
              />
            </div>
            <div>
              <label className="text-gray-400 text-sm">滑块验证（防机器）</label>
              <SliderCaptcha
                onVerified={(ticket) => {
                  setCaptchaTicket(ticket)
                  setCaptchaOk(true)
                }}
              />
            </div>
            <div>
              <label className="text-gray-400 text-sm">邮箱验证码</label>
              <div className="flex gap-2">
                <input
                  type="text"
                  value={code}
                  onChange={(e) => setCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                  className="w-full"
                  placeholder="6位数字"
                  required
                  inputMode="numeric"
                />
                <button
                  type="button"
                  onClick={handleSendCode}
                  disabled={codeLoading || cooldown > 0 || !captchaOk}
                  className="btn-gold px-4 py-2 text-sm whitespace-nowrap disabled:opacity-50"
                >
                  {cooldown > 0 ? `${cooldown}s后重发` : codeLoading ? '发送中...' : '获取验证码'}
                </button>
              </div>
            </div>
            <div>
              <label className="text-gray-400 text-sm">新密码</label>
              <input
                type="password"
                value={newPassword}
                onChange={(e) => setNewPassword(e.target.value)}
                className="w-full"
                placeholder="至少6位"
                required
                minLength={6}
              />
            </div>

            {error && (
              <div className="p-2 rounded text-sm bg-red-900/30 text-red-400">{error}</div>
            )}
            {info && (
              <div className="p-2 rounded text-sm bg-green-900/30 text-green-400">{info}</div>
            )}

            <button
              type="submit"
              disabled={loading}
              className="btn-gold w-full py-2.5 text-sm"
            >
              {loading ? '提交中...' : '重置密码'}
            </button>
            <p className="text-center text-xs text-gray-600">
              若邮箱也忘记了，请联系平台客服或管理员协助。
            </p>
          </form>
        )}
      </div>
    </div>
  )
}
