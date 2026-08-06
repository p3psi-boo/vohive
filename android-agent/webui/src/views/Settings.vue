<template>
  <header class="topbar">
    <div class="brand">
      <button class="icon-button" aria-label="返回" @click="router.back()">
        <svg width="20" height="20" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12.5 4.5 7 10l5.5 5.5" />
        </svg>
      </button>
      设置
    </div>
  </header>

  <main class="workspace">
    <section class="section" style="margin-top: 0">
      <h3>运行状态</h3>
      <div class="card">
        <dl class="facts">
          <div v-for="fact in facts" :key="fact[0]"><dt>{{ fact[0] }}</dt><dd>{{ fact[1] }}</dd></div>
        </dl>
      </div>
    </section>

    <section v-if="urls.length" class="section">
      <h3>Web 管理地址</h3>
      <div class="url-list"><code v-for="url in urls" :key="url">{{ url }}</code></div>
    </section>

    <section class="section">
      <h3>修改密码</h3>
      <form class="card stack" @submit.prevent="changePassword">
        <label class="field">当前密码<input v-model="cred.current" type="password" autocomplete="current-password" required></label>
        <label class="field">新密码<input v-model="cred.next" type="password" minlength="12" autocomplete="new-password" required></label>
        <label class="field">确认新密码<input v-model="cred.confirm" type="password" minlength="12" autocomplete="new-password" required></label>
        <button class="secondary" type="submit" :disabled="busy">更新密码</button>
      </form>
    </section>

    <section class="section danger-zone">
      <h3>危险区</h3>
      <button class="danger-button" @click="resetPairing">解除配对</button>
      <button class="secondary" @click="logout">退出登录</button>
    </section>
  </main>
  <AppToast />
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, type Config, type Status } from '../api'
import { clearSession } from '../session'
import { toast } from '../toast'
import AppToast from '../components/AppToast.vue'

const router = useRouter()
const status = ref<Status>({})
const config = ref<Config>({})
const cred = reactive({ current: '', next: '', confirm: '' })
const busy = ref(false)

const urls = computed(() => status.value.web?.urls || [])

const facts = computed<[string, string][]>(() => {
  const s = status.value.service || {}
  return [
    ['设备型号', s.model || '—'],
    ['Android', s.android_version || '—'],
    ['Agent 版本', s.app_version || '—'],
    ['运行时长', formatDuration(s.uptime_ms || 0)],
    ['设备 ID', config.value.device_id || '未分配'],
    ['Agent ID', config.value.agent_id || '—']
  ]
})

function formatDuration(ms: number): string {
  const minutes = Math.floor(ms / 60000)
  const hours = Math.floor(minutes / 60)
  const days = Math.floor(hours / 24)
  if (days) return `${days}天 ${hours % 24}小时`
  if (hours) return `${hours}小时 ${minutes % 60}分钟`
  return `${minutes}分钟`
}

async function changePassword() {
  if (cred.next !== cred.confirm) {
    toast('两次输入的新密码不一致', true)
    return
  }
  busy.value = true
  try {
    await api('/api/auth/password', {
      method: 'PUT',
      // 账号固定为 admin，服务端协议仍要求携带 username
      body: { username: 'admin', current_password: cred.current, new_password: cred.next }
    })
    toast('密码已更新，请重新登录')
    clearSession()
    setTimeout(() => router.replace({ name: 'login' }), 700)
  } catch (e) {
    toast(e instanceof Error ? e.message : '更新失败', true)
  } finally {
    busy.value = false
  }
}

async function resetPairing() {
  if (!confirm('解除当前配对并重新进入局域网发现模式？')) return
  try {
    await api('/api/pairing/reset', { method: 'POST', body: {} })
    toast('已进入重新配对模式')
    router.replace({ name: 'home' })
  } catch (e) {
    toast(e instanceof Error ? e.message : '操作失败', true)
  }
}

async function logout() {
  try { await api('/api/auth/logout', { method: 'POST', body: {} }) } catch { /* 忽略 */ }
  clearSession()
  router.replace({ name: 'login' })
}

onMounted(async () => {
  try {
    const [s, c] = await Promise.all([api<Status>('/api/status'), api<Config>('/api/config')])
    status.value = s
    config.value = c
  } catch (e) {
    toast(e instanceof Error ? e.message : '加载失败', true)
  }
})
</script>
