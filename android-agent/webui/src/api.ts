// 与 LocalHttpServer 的会话协议对齐：HttpOnly Cookie + CSRF 头。
// 401 时抛出带 status 的错误，由 router 守卫统一跳转登录。

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

let csrfToken = ''

export function setCsrf(token: string) {
  csrfToken = token
}

export async function api<T>(path: string, options: { method?: string; body?: unknown } = {}): Promise<T> {
  const method = options.method || 'GET'
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (options.body !== undefined) headers['Content-Type'] = 'application/json'
  if (csrfToken && method !== 'GET') headers['X-CSRF-Token'] = csrfToken

  const response = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers,
    body: options.body === undefined ? undefined : JSON.stringify(options.body)
  })
  const text = await response.text()
  let payload: Record<string, unknown> = {}
  try {
    payload = text ? JSON.parse(text) : {}
  } catch {
    payload = { message: text }
  }
  if (!response.ok) {
    throw new ApiError(response.status, (payload.message as string) || (payload.error as string) || '请求失败')
  }
  return payload as T
}

// ---- 类型（对齐 AgentService.webStatus / webConfig 的字段）----

export interface Session {
  authenticated: boolean
  username: string
  csrf_token: string
  expires_at: string
}

export interface Status {
  upstream?: { connected?: boolean; state?: string }
  service?: {
    model?: string
    android_version?: string
    app_version?: string
    uptime_ms?: number
  }
  telephony?: { data_connected?: boolean; esim_supported?: boolean }
  permissions?: {
    send_sms?: boolean
    receive_sms?: boolean
    read_sms?: boolean
    write_embedded_subscriptions?: boolean
  }
  web?: { urls?: string[] }
}

export interface Config {
  paired?: boolean
  server_url?: string
  discovered_server_url?: string
  device_id?: string
  agent_id?: string
  web_username?: string
}
