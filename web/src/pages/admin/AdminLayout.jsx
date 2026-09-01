import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import useAuthStore from '../../stores/authStore'

const items = [
  { to: '/admin', label: '平台概览', end: true },
  { to: '/admin/users', label: '用户管理' },
  { to: '/admin/trades', label: '交易监控' },
  { to: '/admin/payments', label: '支付管理' },
  { to: '/admin/jinguizi', label: '金龟子钱包' },
]

export default function AdminLayout() {
  const { user, logout } = useAuthStore()
  const navigate = useNavigate()

  return (
    <div className="flex min-h-[calc(100vh-3.5rem)]">
      <aside className="w-56 bg-dark-300 border-r border-gray-800 p-4 flex flex-col shrink-0">
        <div className="text-gold font-bold text-lg mb-6 px-2">平台管理后台</div>
        <nav className="flex flex-col gap-1">
          {items.map((it) => (
            <NavLink
              key={it.to}
              to={it.to}
              end={it.end}
              className={({ isActive }) =>
                `px-3 py-2 rounded text-sm transition-colors ${
                  isActive ? 'bg-gold/15 text-gold font-semibold' : 'text-gray-300 hover:bg-dark-200'
                }`
              }
            >
              {it.label}
            </NavLink>
          ))}
        </nav>
        <div className="mt-auto pt-4 border-t border-gray-800">
          <div className="px-3 text-xs text-gray-500 mb-2 truncate">{user?.nickname || user?.username}</div>
          <button
            onClick={() => {
              logout()
              navigate('/')
            }}
            className="px-3 text-xs text-gray-400 hover:text-red-400 transition-colors"
          >
            退出登录
          </button>
        </div>
      </aside>
      <section className="flex-1 p-5 overflow-x-auto">
        <Outlet />
      </section>
    </div>
  )
}
