import { reactive } from 'vue'

// PWA 安装提示状态。beforeinstallprompt 只在安全上下文 + 满足 PWA 条件时触发；
// 局域网 HTTP 下不会触发，此时降级为手动操作指引。
interface BeforeInstallPromptEvent extends Event {
  prompt(): Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>
}

const DISMISS_KEY = 'install-hint-dismissed'

export const installState = reactive({
  deferred: null as BeforeInstallPromptEvent | null,
  dismissed: localStorage.getItem(DISMISS_KEY) === '1'
})

export function isStandalone(): boolean {
  return window.matchMedia('(display-mode: standalone)').matches
    || (navigator as unknown as { standalone?: boolean }).standalone === true
}

export function isIos(): boolean {
  return /iphone|ipad|ipod/i.test(navigator.userAgent)
}

export function dismissInstallHint() {
  installState.dismissed = true
  try { localStorage.setItem(DISMISS_KEY, '1') } catch { /* 隐私模式下忽略 */ }
}

export async function promptInstall() {
  const event = installState.deferred
  if (!event) return
  await event.prompt()
  await event.userChoice.catch(() => null)
  installState.deferred = null
}

window.addEventListener('beforeinstallprompt', (event) => {
  event.preventDefault()
  installState.deferred = event as BeforeInstallPromptEvent
})
window.addEventListener('appinstalled', () => { installState.deferred = null })
