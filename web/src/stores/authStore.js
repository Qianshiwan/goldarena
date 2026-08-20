import { create } from 'zustand'
import { authAPI, userAPI } from '../services/api'

const useAuthStore = create((set) => ({
  user: null,
  token: localStorage.getItem('access_token') || null,
  refreshToken: localStorage.getItem('refresh_token') || null,

  login: async (username, password) => {
    const { data } = await authAPI.login({ username, password })
    const d = data.data
    localStorage.setItem('access_token', d.access_token)
    localStorage.setItem('refresh_token', d.refresh_token)
    set({
      token: d.access_token,
      refreshToken: d.refresh_token,
      user: { id: d.user_id, username: d.username, nickname: d.nickname, role: d.role },
    })
    return d
  },

  register: async (username, password, nickname, email, code) => {
    const { data } = await authAPI.register({ username, password, nickname, email, code })
    const d = data.data
    localStorage.setItem('access_token', d.access_token)
    localStorage.setItem('refresh_token', d.refresh_token)
    set({
      token: d.access_token,
      refreshToken: d.refresh_token,
      user: { id: d.user_id, username: d.username, nickname: d.nickname, role: d.role },
    })
    return d
  },

  logout: () => {
    localStorage.removeItem('access_token')
    localStorage.removeItem('refresh_token')
    set({ user: null, token: null, refreshToken: null })
  },

  fetchProfile: async () => {
    try {
      const { data } = await userAPI.getProfile()
      set({ user: data.data })
    } catch {
      // silently fail if not authenticated
    }
  },
}))

export default useAuthStore
