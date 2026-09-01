import { Link, useNavigate } from 'react-router-dom'
import useAuthStore from '../../stores/authStore'

export default function Navbar() {
  const { user, token, logout } = useAuthStore()
  const navigate = useNavigate()

  return (
    <nav className="bg-dark-300 border-b border-gray-800">
      <div className="max-w-[1600px] mx-auto px-4 h-14 flex items-center justify-between">
        {/* Logo */}
        <Link to="/" className="flex items-center gap-2 text-lg font-bold">
          <span className="text-2xl">🥇</span>
          <span className="gold-gradient">金归子</span>
          <span className="text-xs text-gray-500 font-normal ml-1">GoldArena</span>
        </Link>

        {/* Navigation */}
        <div className="flex items-center gap-6">
          {token ? (
            <>
              <Link to="/" className="text-gray-300 hover:text-gold transition-colors text-sm">
                交易大厅
              </Link>
              <Link to="/cultivation" className="text-gray-300 hover:text-gold transition-colors text-sm">
                交易境界
              </Link>
              <Link to="/wallet" className="text-gray-300 hover:text-gold transition-colors text-sm">
                钱包
              </Link>
              <Link to="/jinguizi" className="text-gold hover:text-yellow-300 transition-colors text-sm font-semibold">
                金龟子币
              </Link>
              {user?.role === 'admin' && (
                <Link to="/admin" className="text-gold hover:text-yellow-300 transition-colors text-sm font-semibold">
                  管理后台
                </Link>
              )}
              <div className="flex items-center gap-3 ml-4 pl-4 border-l border-gray-700">
                <span className="text-gray-400 text-sm">{user?.nickname || user?.username}</span>
                <button
                  onClick={() => { logout(); navigate('/login') }}
                  className="text-xs text-gray-500 hover:text-red-400 transition-colors"
                >
                  退出
                </button>
              </div>
            </>
          ) : (
            <>
              <Link to="/login" className="text-gray-300 hover:text-gold transition-colors text-sm">
                登录
              </Link>
              <Link to="/register" className="btn-gold text-sm px-4 py-1.5">
                注册
              </Link>
            </>
          )}
        </div>
      </div>
    </nav>
  )
}
