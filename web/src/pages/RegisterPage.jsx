import { useState, useRef, useEffect } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import useAuthStore from '../stores/authStore'
import { authAPI } from '../services/api'
import SliderCaptcha from '../components/SliderCaptcha'

export default function RegisterPage() {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [nickname, setNickname] = useState('')
  const [email, setEmail] = useState('')
  const [code, setCode] = useState('')
  const [error, setError] = useState('')
  const [info, setInfo] = useState('')
  const [loading, setLoading] = useState(false)
  const [codeLoading, setCodeLoading] = useState(false)
  const [cooldown, setCooldown] = useState(0)
  const [captchaTicket, setCaptchaTicket] = useState('')
  const [captchaOk, setCaptchaOk] = useState(false)
  const timerRef = useRef(null)

  const register = useAuthStore((s) => s.register)
  const navigate = useNavigate()

  useEffect(() => () => clearInterval(timerRef.current), [])

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
      const { data } = await authAPI.sendCode(email, captchaTicket)
      const d = data.data
      setInfo('验证码已发送，请查收邮箱')
      if (d.dev_code) {
        // Dev mode only (no SMTP configured): show code for convenience
        setInfo(`开发模式验证码：${d.dev_code}（生产环境请查收邮箱）`)
      }
      // ticket is single-use — require a fresh captcha before the next send
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
    if (password.length < 6) {
      setError('密码至少6位')
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
      await register(username, password, nickname, email, code)
      navigate('/')
    } catch (err) {
      setError(err.response?.data?.message || '注册失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-[80vh] flex items-center justify-center">
      <div className="trade-card p-8 w-full max-w-md">
        <div className="text-center mb-6">
          <span className="text-4xl">🥇</span>
          <h1 className="text-xl font-bold gold-gradient mt-2">注册新账号</h1>
          <p className="text-gray-500 text-sm mt-1">邮箱验证注册即送 50,000 游戏币</p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="text-gray-400 text-sm">用户名</label>
            <input
              type="text"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              className="w-full"
              placeholder="3-50个字符"
              required
              minLength={3}
            />
          </div>
          <div>
            <label className="text-gray-400 text-sm">邮箱</label>
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              className="w-full"
              placeholder="用于接收验证码，一个邮箱仅能注册一个账号"
              required
            />
          </div>
          <div>
            <label className="text-gray-400 text-sm">滑块验证（防机器注册）</label>
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
            <label className="text-gray-400 text-sm">昵称 (可选)</label>
            <input
              type="text"
              value={nickname}
              onChange={(e) => setNickname(e.target.value)}
              className="w-full"
              placeholder="给自己取个好听的名字"
            />
          </div>
          <div>
            <label className="text-gray-400 text-sm">密码</label>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
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
            {loading ? '注册中...' : '注册并领取50,000游戏币'}
          </button>
        </form>

        <p className="text-center text-sm text-gray-500 mt-4">
          已有账号?{' '}
          <Link to="/login" className="text-gold hover:underline">
            立即登录
          </Link>
        </p>
      </div>
    </div>
  )
}
