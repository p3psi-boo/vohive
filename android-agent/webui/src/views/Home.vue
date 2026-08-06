<template>
  <header class="topbar">
    <div class="brand"><span class="mark small">VH</span>VoHive Agent</div>
    <div class="top-state"><i :class="{ online: connected }"></i>{{ stateLabel }}</div>
    <button class="icon-button" aria-label="设置" @click="router.push({ name: 'settings' })">
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="12" cy="12" r="3" />
        <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z" />
      </svg>
    </button>
  </header>

  <main class="workspace">
    <section class="hero">
      <h2>{{ hero.title }}</h2>
      <p v-if="hero.copy">{{ hero.copy }}</p>
    </section>

    <!-- 未配对：一个配对卡片，默认只需输配对码 -->
    <section v-if="!paired" class="card">
      <h3>配对到 VoHive</h3>
      <p class="hint">{{ discovered ? `已发现 ${discovered}` : '正在局域网内发现服务器' }}</p>
      <form class="pair-form" @submit.prevent="pair">
        <label class="field">
          配对码
          <input v-model="pairCode" class="mono" inputmode="numeric" pattern="[0-9]{6}" maxlength="6"
                 placeholder="000000" autocomplete="off" required>
        </label>
        <details class="manual">
          <summary>手动输入服务器地址</summary>
          <label class="field">
            服务器地址
            <input v-model="serverUrl" class="mono" placeholder="http://192.168.1.2:7575" required>
          </label>
        </details>
        <button class="primary" type="submit" :disabled="busy">{{ busy ? '配对中' : '配对' }}</button>
      </form>
    </section>

    <!-- 已配对 -->
    <template v-else>
      <a v-if="connected" class="primary" :href="config.server_url || '#'" target="_blank" rel="noreferrer">
        打开 VoHive 控制台
      </a>
      <button v-else class="primary" :disabled="busy" @click="reconnect">
        {{ busy ? '正在重连' : '重新连接' }}
      </button>

      <section class="card section">
        <div class="cap-list">
          <div v-for="cap in capabilities" :key="cap.name" class="cap-row">
            <span>{{ cap.name }}</span>
            <strong :class="{ ready: cap.ready, na: cap.na }">{{ cap.label }}</strong>
          </div>
        </div>
      </section>
    </template>

    <InstallBanner />
  </main>
  <AppToast />
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, type Config, type Status } from '../api'
import { toast } from '../toast'
import AppToast from '../components/AppToast.vue'
import InstallBanner from '../components/InstallBanner.vue'

const router = useRouter()
const status = ref<Status>({})
const config = ref<Config>({})
const pairCode = ref('')
const serverUrl = ref('')
const busy = ref(false)
let timer: ReturnType<typeof setInterval> | undefined

const paired = computed(() => config.value.paired === true)
const connected = computed(() => status.value.upstream?.connected === true)
const discovered = computed(() => config.value.discovered_server_url || '')

const stateLabel = computed(() => connected.value ? '已连接' : paired.value ? '未连接' : '未配对')

const hero = computed(() => {
  if (connected.value) return { title: '已接入 VoHive', copy: '' }
  if (paired.value) return { title: '正在连接', copy: connectionLabel(status.value.upstream?.state) }
  return { title: '等待配对', copy: '在 VoHive 控制台获取六位配对码' }
})

const capabilities = computed(() => {
  const p = status.value.permissions || {}
  const t = status.value.telephony || {}
  const smsReady = !!(p.send_sms && p.receive_sms && p.read_sms)
  const netReady = connected.value && t.data_connected === true
  const esimSupported = t.esim_supported === true
  const esimPrivileged = p.write_embedded_subscriptions === true
  return [
    { name: 'SMS', label: smsReady ? '就绪' : '未授权', ready: smsReady, na: false },
    {
      name: 'NET',
      label: netReady ? '就绪' : connected.value ? '蜂窝网络未连接' : '不可用',
      ready: netReady,
      na: !connected.value
    },
    {
      name: 'eSIM',
      label: !esimSupported ? '不支持' : esimPrivileged ? '就绪' : '待授权',
      ready: esimSupported && esimPrivileged,
      na: !esimSupported
    }
  ]
})

function connectionLabel(value?: string): string {
  if (!value) return '未知'
  if (value === 'connecting') return '正在连接 VoHive'
  if (value.startsWith('reconnecting')) return '连接中断，正在重连'
  if (value.startsWith('waiting')) return '连接配置无效'
  if (value.startsWith('connection error')) return '连接错误'
  return '未连接'
}

async function load(silent = true) {
  try {
    const [s, c] = await Promise.all([api<Status>('/api/status'), api<Config>('/api/config')])
    status.value = s
    config.value = c
    if (c.discovered_server_url && !serverUrl.value) serverUrl.value = c.discovered_server_url
  } catch (e) {
    if (!silent) toast(e instanceof Error ? e.message : '加载失败', true)
  }
}

async function pair() {
  if (!/^[0-9]{6}$/.test(pairCode.value.trim())) {
    toast('请输入六位配对码', true)
    return
  }
  busy.value = true
  try {
    await api('/api/config', {
      method: 'PUT',
      body: { server_url: serverUrl.value.trim(), pair_token: pairCode.value.trim(), agent_enabled: true }
    })
    toast('配对请求已提交')
    pairCode.value = ''
    setTimeout(() => load(), 800)
  } catch (e) {
    toast(e instanceof Error ? e.message : '配对失败', true)
  } finally {
    busy.value = false
  }
}

async function reconnect() {
  busy.value = true
  try {
    await api('/api/agent/reconnect', { method: 'POST', body: {} })
    toast('正在重新连接')
    setTimeout(() => load(), 800)
  } catch (e) {
    toast(e instanceof Error ? e.message : '操作失败', true)
  } finally {
    busy.value = false
  }
}

onMounted(() => {
  load(false)
  timer = setInterval(() => load(), 5000)
})
onBeforeUnmount(() => clearInterval(timer))
</script>
