import { Routes, Route, Navigate } from 'react-router-dom'
import useAuthStore from './stores/authStore'
import Navbar from './components/layout/Navbar'
import DashboardPage from './pages/DashboardPage'
import TradePage from './pages/TradePage'
import LoginPage from './pages/LoginPage'
import RegisterPage from './pages/RegisterPage'
import WalletPage from './pages/WalletPage'
import CultivationPage from './pages/CultivationPage'
import AdminLayout from './pages/admin/AdminLayout'
import AdminDashboard from './pages/admin/AdminDashboard'
import AdminUsers from './pages/admin/AdminUsers'
import AdminTrades from './pages/admin/AdminTrades'
import AdminPayments from './pages/admin/AdminPayments'

function PrivateRoute({ children }) {
  const token = useAuthStore((s) => s.token)
  return token ? children : <Navigate to="/login" />
}

function AdminRoute({ children }) {
  const token = useAuthStore((s) => s.token)
  const role = useAuthStore((s) => s.user?.role)
  if (!token) return <Navigate to="/login" />
  if (role !== 'admin') return <Navigate to="/" />
  return children
}

export default function App() {
  return (
    <div className="min-h-screen bg-dark-200">
      <Navbar />
      <main className="max-w-[1600px] mx-auto px-4 py-4">
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/register" element={<RegisterPage />} />
          <Route path="/" element={<PrivateRoute><DashboardPage /></PrivateRoute>} />
          <Route path="/trade" element={<PrivateRoute><TradePage /></PrivateRoute>} />
          <Route path="/wallet" element={<PrivateRoute><WalletPage /></PrivateRoute>} />
          <Route path="/cultivation" element={<PrivateRoute><CultivationPage /></PrivateRoute>} />
          <Route path="/admin" element={<AdminRoute><AdminLayout /></AdminRoute>}>
            <Route index element={<AdminDashboard />} />
            <Route path="users" element={<AdminUsers />} />
            <Route path="trades" element={<AdminTrades />} />
            <Route path="payments" element={<AdminPayments />} />
          </Route>
          <Route path="*" element={<Navigate to="/" />} />
        </Routes>
      </main>
    </div>
  )
}
