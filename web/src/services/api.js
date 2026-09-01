import axios from 'axios'

const api = axios.create({
  baseURL: '/api/v1',
  timeout: 10000,
})

// Request interceptor: attach JWT token
api.interceptors.request.use((config) => {
  const token = localStorage.getItem('access_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// Response interceptor: on 401 (token missing/expired for an already-authed
// request) clear creds and bounce to /login. Auth endpoints (login/register/
// send-code/reset-password/...) surface their own errors and MUST NOT be
// hijacked to /login — otherwise a wrong password or expired reset code would
// silently kick the user out of the very flow they're trying to complete.
api.interceptors.response.use(
  (res) => res,
  async (err) => {
    const url = err.config?.url || ''
    const isAuthEndpoint = url.includes('/auth/')
    if (err.response?.status === 401 && !isAuthEndpoint) {
      localStorage.removeItem('access_token')
      localStorage.removeItem('refresh_token')
      if (!window.location.pathname.startsWith('/login')) {
        window.location.href = '/login'
      }
    }
    return Promise.reject(err)
  }
)

// ========== Auth API ==========
export const authAPI = {
  getCaptcha: () => api.get('/auth/captcha'),
  verifyCaptcha: (payload) => api.post('/auth/captcha/verify', payload),
  sendCode: (email, ticket) => api.post('/auth/send-code', { email, captcha_ticket: ticket }),
  sendResetCode: (email, ticket) => api.post('/auth/send-reset-code', { email, captcha_ticket: ticket }),
  resetPassword: (data) => api.post('/auth/reset-password', data),
  register: (data) => api.post('/auth/register', data),
  login: (data) => api.post('/auth/login', data),
  refresh: (refreshToken) => api.post('/auth/refresh', { refresh_token: refreshToken }),
}

// ========== User API ==========
export const userAPI = {
  getProfile: () => api.get('/user/profile'),
  updateProfile: (data) => api.put('/user/profile', data),
  getWallet: () => api.get('/user/wallet'),
  recharge: (amount) => api.post('/user/wallet/recharge', { amount }),
  getTransactions: (params) => api.get('/user/wallet/transactions', { params }),
}

// ========== Market API ==========
export const marketAPI = {
  getQuote: (symbol = 'XAU', contract = 'SPOT') =>
    api.get('/market/quote', { params: { symbol, contract_month: contract } }),
  getKLines: (symbol = 'XAU', contract = 'SPOT', period = '1m') =>
    api.get('/market/klines', { params: { symbol, contract_month: contract, period } }),
  getSymbols: () => api.get('/market/symbols'),
}

// ========== Trade API ==========
export const tradeAPI = {
  placeOrder: (data) => api.post('/trade/order', data),
  getPositions: (contestId) => api.get('/trade/positions', { params: { contest_id: contestId } }),
  closePosition: (positionId) => api.post('/trade/close', { position_id: positionId }),
  cancelOrder: (orderId) => api.post('/trade/cancel', { order_id: orderId }),
  getPendingOrders: () => api.get('/trade/pending'),
  updateSLTP: (positionId, data) => api.post('/trade/sltp', { position_id: positionId, ...data }),
  getPnL: () => api.get('/trade/pnl'),
  getClosed: (params) => api.get('/trade/closed', { params }),
}

// ========== Cultivation API (修仙等级) ==========
export const cultivationAPI = {
  getProgress: () => api.get('/cultivation/progress'),
  getAllLevels: () => api.get('/cultivation/levels'),
  getRank: () => api.get('/cultivation/rank'),
  breakthrough: () => api.post('/cultivation/breakthrough'),
  refresh: () => api.post('/cultivation/refresh'),
}

// ========== Payment API (真实支付充值) ==========
export const paymentAPI = {
  // 创建支付订单：amount 元, channel: wxpay | alipay
  create: (amount, channel) => api.post('/payment/create', { amount, channel }),
  // 查询我的支付订单（用于轮询是否到账）
  orders: () => api.get('/payment/orders'),
  // 沙箱模拟支付成功（仅 sandbox 模式可用）
  simulate: (outTradeNo) => api.post('/payment/simulate', { out_trade_no: outTradeNo }),
  // 二维码图片地址（后端 /api/v1/payment/qr?text=...）
  qrURL: (text) => `/api/v1/payment/qr?text=${encodeURIComponent(text)}`,
}

// ========== 金龟子模拟币 (隔离钱包，管理员统一充值) ==========
export const jinguiziAPI = {
  getWallet: () => api.get('/jinguizi/wallet'),
  getTransactions: (params) => api.get('/jinguizi/transactions', { params }),
  adminList: (params) => api.get('/admin/jinguizi/list', { params }),
  adminRecharge: (payload) => api.post('/admin/jinguizi/recharge', payload),
  adminAdjust: (payload) => api.post('/admin/jinguizi/adjust', payload),
}

// ========== Admin API (平台管理后台) ==========
export const adminAPI = {
  dashboard: () => api.get('/admin/dashboard'),
  listUsers: (params) => api.get('/admin/users', { params }),
  getUser: (id) => api.get(`/admin/users/${id}`),
  adjustBalance: (id, payload) => api.post(`/admin/users/${id}/balance`, payload),
  setStatus: (id, payload) => api.post(`/admin/users/${id}/status`, payload),
  listPositions: (params) => api.get('/admin/positions', { params }),
  forceClose: (id) => api.post(`/admin/positions/${id}/close`),
  listOrders: (params) => api.get('/admin/orders', { params }),
  listPayments: (params) => api.get('/admin/payments', { params }),
  creditPayment: (no) => api.post(`/admin/payments/${no}/credit`),
}

export default api
