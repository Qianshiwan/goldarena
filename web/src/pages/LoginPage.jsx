import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import useAuthStore from '../stores/authStore'
import ForgotPassword from '../components/ForgotPassword'

export default function LoginPage() {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [showForgot, setShowForgot] = useState(false)
  const login = useAuthStore((s) => s.login)
  const navigate = useNavigate()

  const handleSubmit = async (e) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      await login(username, password)
      navigate('/')
    } catch (err) {
      setError(err.response?.data?.message || '登录失败，请检查用户名和密码')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-[80vh] flex items-center justify-center">
      <div className="trade-card p-8 w-full max-w-md">
        <div className="text-center mb-6">
          <span className="text-4xl">🥇</span>
          <h1 className="text-xl font-bold gold-gradient mt-2">金龟子 GoldArena</h1>
          <p className="text-gray-500 text-sm mt-1">金龟子现货模拟交易游戏平台</p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="text-gray-400 text-sm">用户名</label>
            <input
              type="text"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              className="w-full"
              placeholder="请输入用户名"
              required
            />
          </div>
          <div>
            <label className="text-gray-400 text-sm">密码</label>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="w-full"
              placeholder="请输入密码"
              required
            />
          </div>

          {error && (
            <div className="p-2 rounded text-sm bg-red-900/30 text-red-400">{error}</div>
          )}

          <button
            type="submit"
            disabled={loading}
            className="btn-gold w-full py-2.5 text-sm"
          >
            {loading ? '登录中...' : '登录'}
          </button>
        </form>

        <div className="flex items-center justify-between mt-4">
          <p className="text-sm text-gray-500">
            还没有账号?{' '}
            <Link to="/register" className="text-gold hover:underline">
              立即注册
            </Link>
          </p>
          <button
            type="button"
            onClick={() => setShowForgot(true)}
            className="text-sm text-gold hover:underline"
          >
            忘记密码？
          </button>
        </div>
      </div>

      {showForgot && <ForgotPassword onClose={() => setShowForgot(false)} />}
    </div>
  )
}
