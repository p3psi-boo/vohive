import { reactive } from 'vue'
import { api, setCsrf, type Session } from './api'

// 会话状态：App 启动时恢复一次，路由守卫据此放行或跳登录。
export const session = reactive({
  ready: false,
  authenticated: false,
  username: ''
})

export async function restoreSession(): Promise<boolean> {
  try {
    const s = await api<Session>('/api/auth/session')
    applySession(s)
    return true
  } catch {
    session.authenticated = false
    return false
  } finally {
    session.ready = true
  }
}

export function applySession(s: Session) {
  setCsrf(s.csrf_token || '')
  session.authenticated = true
  session.username = s.username || ''
}

export function clearSession() {
  setCsrf('')
  session.authenticated = false
  session.username = ''
}
