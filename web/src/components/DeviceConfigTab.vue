<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import {
  Delete24Regular,
  Key24Regular,
  Phone24Regular,
  Save24Regular
} from '@vicons/fluent'
import type { AndroidAgentStatus, AndroidSubscription, DeviceConfigDTO, DeviceOverviewItem } from '../types/api'
import { devicesService } from '../services/devices'
import { isWwanQmiControlPath } from '../utils/deviceBackend'

const props = defineProps<{
  editConfig: DeviceConfigDTO | null
  deviceStatus?: DeviceOverviewItem | null
  saving: boolean
  deleting: boolean
}>()

const emit = defineEmits<{ save: []; autosave: []; delete: [] }>()

const isAndroid = computed(() => props.editConfig?.device_kind === 'android' || props.editConfig?.device_backend === 'android')
const activeControlDevice = computed(() => props.deviceStatus?.control_device || props.editConfig?.control_device)
const activeInterface = computed(() => props.deviceStatus?.interface || props.editConfig?.interface)
const activeATPort = computed(() => props.deviceStatus?.at_port || props.editConfig?.at_port)
const activeUsbPath = computed(() => props.deviceStatus?.usb_path || props.editConfig?.usb_path)
const isQMIBackendOnly = computed(() => !isAndroid.value && isWwanQmiControlPath(activeControlDevice.value))
const isMBIMBackendOnly = computed(() => !isAndroid.value && String(props.editConfig?.device_backend || '').toLowerCase() === 'mbim')

const agentStatus = ref<AndroidAgentStatus | null>(null)
const subscriptions = ref<AndroidSubscription[]>([])
const androidLoading = ref(false)
const selectedSubscriptionID = ref<number | null>(null)
const subscriptionActionLoading = ref(false)
let refreshTimer: number | null = null
let nameSaveTimer: number | null = null
let autoSaveDeviceID = ''

const activeSubscriptions = computed(() => subscriptions.value.filter(item => item.active))
const selectedSubscription = computed(() =>
  subscriptions.value.find(item => item.selected)
  || subscriptions.value.find(item => item.subscription_id === selectedSubscriptionID.value)
  || activeSubscriptions.value[0]
  || null
)
const showSubscriptionControls = computed(() => activeSubscriptions.value.length > 1)
const smsReady = computed(() => {
  const access = agentStatus.value?.snapshot?.access
  return !!(access?.send_sms && access?.receive_sms && access?.read_sms)
})
const proxyReady = computed(() => !!(agentStatus.value?.online && agentStatus.value.snapshot?.data_connected))
const esimSupported = computed(() => agentStatus.value?.snapshot?.esim_supported === true)
const esimPrivileged = computed(() => agentStatus.value?.snapshot?.access?.write_embedded_subscriptions === true)

watch(isQMIBackendOnly, (locked) => {
  if (locked && props.editConfig) props.editConfig.device_backend = 'qmi'
}, { immediate: true })

watch(
  () => [isAndroid.value, props.editConfig?.id] as const,
  ([android]) => {
    autoSaveDeviceID = String(props.editConfig?.id || '')
    if (refreshTimer !== null) window.clearInterval(refreshTimer)
    refreshTimer = null
    agentStatus.value = null
    subscriptions.value = []
    if (!android) return
    void refreshAndroidAgent()
    refreshTimer = window.setInterval(() => void refreshAndroidAgent(true), 5000)
  },
  { immediate: true }
)

watch(
  () => props.editConfig?.name,
  (name, previous) => {
    const currentDeviceID = String(props.editConfig?.id || '')
    if (currentDeviceID !== autoSaveDeviceID) {
      autoSaveDeviceID = currentDeviceID
      return
    }
    if (!isAndroid.value || name === previous || previous === undefined) return
    if (nameSaveTimer !== null) window.clearTimeout(nameSaveTimer)
    nameSaveTimer = window.setTimeout(() => emit('autosave'), 700)
  }
)

onBeforeUnmount(() => {
  if (refreshTimer !== null) window.clearInterval(refreshTimer)
  if (nameSaveTimer !== null) window.clearTimeout(nameSaveTimer)
})

async function refreshAndroidAgent(silent = false) {
  const id = String(props.editConfig?.id || '').trim()
  if (!id || !isAndroid.value || androidLoading.value) return
  androidLoading.value = true
  try {
    const statusResult = await devicesService.getAndroidAgentStatus(id)
    if (!statusResult.ok) throw new Error(statusResult.error.message)
    agentStatus.value = statusResult.data
    selectedSubscriptionID.value = statusResult.data.snapshot?.selected_subscription_id ?? null
    if (statusResult.data.online) {
      const listResult = await devicesService.listAndroidSubscriptions(id)
      if (listResult.ok) {
        subscriptions.value = listResult.data
        const selected = listResult.data.find(item => item.selected)
        if (selected) selectedSubscriptionID.value = selected.subscription_id
      }
    } else {
      subscriptions.value = statusResult.data.snapshot?.subscriptions || []
    }
  } catch (error) {
    if (!silent) ElMessage.error(error instanceof Error ? error.message : 'Android Agent 状态读取失败')
  } finally {
    androidLoading.value = false
  }
}

async function selectSubscription() {
  const id = String(props.editConfig?.id || '').trim()
  if (!id || selectedSubscriptionID.value == null) return
  subscriptionActionLoading.value = true
  try {
    const result = await devicesService.selectAndroidSubscription(id, selectedSubscriptionID.value)
    if (!result.ok) throw new Error(result.error.message)
    ElMessage.success('短信和代理已切换到所选 SIM')
    await refreshAndroidAgent()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : 'SIM 切换失败')
  } finally {
    subscriptionActionLoading.value = false
  }
}

async function switchESIM(item: AndroidSubscription) {
  const id = String(props.editConfig?.id || '').trim()
  if (!id) return
  subscriptionActionLoading.value = true
  try {
    const result = await devicesService.switchAndroidESIM(id, item.subscription_id, item.port_index || 0)
    if (!result.ok) throw new Error(result.error.message)
    ElMessage.success('eSIM 切换请求已发送')
    window.setTimeout(() => void refreshAndroidAgent(true), 1500)
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : 'eSIM 切换失败')
  } finally {
    subscriptionActionLoading.value = false
  }
}

function metric(value: unknown, unit = '') {
  return value === undefined || value === null || value === '' ? '--' : String(value) + unit
}

function subscriptionLabel(item: AndroidSubscription | null) {
  if (!item) return '未检测到 SIM'
  const type = item.embedded ? 'eSIM' : 'SIM'
  const carrier = item.carrier_name || item.display_name || '未知运营商'
  return type + ' · ' + carrier
}
</script>

<template>
  <div>
    <div class="config-heading">
      <div class="flex items-center gap-3">
        <div class="heading-icon">
          <el-icon size="22"><Phone24Regular v-if="isAndroid" /><Key24Regular v-else /></el-icon>
        </div>
        <div>
          <div class="text-lg font-bold text-gray-900 dark:text-white">{{ isAndroid ? 'Android 手机' : '设备配置' }}</div>
          <div class="text-xs text-gray-500">{{ isAndroid ? '日常状态、SIM 与功能入口' : '设备绑定与运行后端配置' }}</div>
        </div>
      </div>
      <div class="flex items-center gap-2">
        <el-button type="danger" plain :loading="deleting" @click="emit('delete')"><el-icon><Delete24Regular /></el-icon>删除设备</el-button>
        <el-button v-if="!isAndroid" type="primary" :loading="saving" @click="emit('save')"><el-icon><Save24Regular /></el-icon>保存配置</el-button>
      </div>
    </div>

    <template v-if="editConfig && isAndroid">
      <section class="android-hero" :class="{ online: agentStatus?.online }">
        <div class="status-orb"><span></span><i></i></div>
        <div class="min-w-0">
          <p class="hero-kicker">{{ agentStatus?.online ? 'CONNECTED' : 'OFFLINE' }}</p>
          <h3>{{ agentStatus?.online ? '设备工作正常' : '等待 Agent 连接' }}</h3>
          <p>{{ agentStatus?.online ? subscriptionLabel(selectedSubscription) : '打开 Agent 本地网页检查配对或网络。' }}</p>
        </div>
        <div class="hero-metrics">
          <div><span>信号</span><strong>{{ metric(agentStatus?.snapshot?.signal_dbm, ' dBm') }}</strong></div>
          <div><span>电量</span><strong>{{ metric(agentStatus?.snapshot?.battery_pct, '%') }}</strong></div>
          <div><span>网络</span><strong>{{ metric(agentStatus?.snapshot?.network_mode) }}</strong></div>
        </div>
      </section>

      <div class="name-row">
        <label>设备名称</label>
        <el-input v-model="editConfig.name" placeholder="给这台手机起个名字" />
        <span>{{ saving ? '保存中…' : '自动保存' }}</span>
      </div>

      <section class="result-grid">
        <article :class="{ ready: smsReady }">
          <span class="result-code">SMS</span>
          <strong>{{ smsReady ? '可用' : '需要授权' }}</strong>
          <small>{{ smsReady ? '短信收发已就绪' : '请在部署阶段授予短信权限' }}</small>
          <router-link to="/sms">打开短信中心 →</router-link>
        </article>
        <article :class="{ ready: proxyReady }">
          <span class="result-code">NET</span>
          <strong>{{ proxyReady ? '可用' : agentStatus?.online ? '等待蜂窝网络' : '等待连接' }}</strong>
          <small>{{ proxyReady ? 'HTTP 与 SOCKS5 出口已就绪' : '上线后自动恢复代理' }}</small>
          <router-link to="/proxy">查看代理地址 →</router-link>
        </article>
        <article :class="{ ready: esimSupported }">
          <span class="result-code">eSIM</span>
          <strong>{{ !esimSupported ? '设备不支持' : esimPrivileged ? '可用' : '需手机确认' }}</strong>
          <small>{{ esimPrivileged ? '支持无交互切换' : esimSupported ? '切换时由 Android 确认' : '未检测到 eUICC' }}</small>
        </article>
      </section>

      <section class="sim-panel">
        <div>
          <p class="panel-kicker">ACTIVE SUBSCRIPTION</p>
          <h3>{{ subscriptionLabel(selectedSubscription) }}</h3>
          <span v-if="selectedSubscription">
            {{ selectedSubscription.msisdn || '未读取手机号' }}
            <template v-if="showSubscriptionControls"> · 共 {{ activeSubscriptions.length }} 张活动卡</template>
          </span>
        </div>
        <div v-if="showSubscriptionControls" class="sim-control">
          <el-select v-model="selectedSubscriptionID" placeholder="选择 SIM">
            <el-option
              v-for="item in activeSubscriptions"
              :key="item.subscription_id"
              :value="item.subscription_id"
              :label="subscriptionLabel(item)"
            />
          </el-select>
          <el-button type="primary" :loading="subscriptionActionLoading" @click="selectSubscription">切换</el-button>
        </div>
        <el-tag v-else-if="selectedSubscription" type="success" effect="plain">已自动选择</el-tag>
      </section>

      <section v-if="esimSupported || subscriptions.length > 1" class="esim-panel">
        <div class="panel-title">
          <div><p class="panel-kicker">SIM &amp; eSIM</p><h3>可用号码</h3></div>
          <span>只有多卡设备才需要手动选择</span>
        </div>
        <div class="subscription-list">
          <article v-for="item in subscriptions" :key="item.subscription_id" :class="{ selected: item.selected }">
            <div><strong>{{ subscriptionLabel(item) }}</strong><small>{{ item.msisdn || '无号码' }} · Slot {{ item.slot_index }}</small></div>
            <el-tag v-if="item.selected" type="success" size="small">当前</el-tag>
            <el-button v-else-if="item.embedded" link type="primary" :loading="subscriptionActionLoading" @click="switchESIM(item)">切换 eSIM</el-button>
          </article>
        </div>
      </section>

      <el-collapse class="advanced-collapse">
        <el-collapse-item title="高级诊断与设备标识" name="diagnostics">
          <div class="diagnostic-grid">
            <div><span>设备 ID</span><strong>{{ editConfig.id }}</strong></div>
            <div><span>Agent ID</span><strong>{{ editConfig.android_agent_id || '--' }}</strong></div>
            <div><span>IMEI</span><strong>{{ metric(agentStatus?.snapshot?.imei) }}</strong></div>
            <div><span>IMSI</span><strong>{{ metric(agentStatus?.snapshot?.imsi) }}</strong></div>
            <div><span>ICCID</span><strong>{{ metric(agentStatus?.snapshot?.iccid) }}</strong></div>
            <div><span>蜂窝 IP</span><strong>{{ metric(agentStatus?.snapshot?.private_ip || agentStatus?.snapshot?.private_ipv6) }}</strong></div>
            <div><span>RSRP</span><strong>{{ metric(agentStatus?.snapshot?.signal_rsrp, ' dBm') }}</strong></div>
            <div><span>RSRQ / SINR</span><strong>{{ metric(agentStatus?.snapshot?.signal_rsrq, ' dB') }} / {{ metric(agentStatus?.snapshot?.signal_sinr, ' dB') }}</strong></div>
            <div><span>固件</span><strong>{{ metric(agentStatus?.snapshot?.firmware) }}</strong></div>
            <div><span>基带</span><strong>{{ metric(agentStatus?.snapshot?.baseband) }}</strong></div>
          </div>
          <el-table v-if="agentStatus?.snapshot?.registration_details?.length" :data="agentStatus.snapshot.registration_details" size="small" class="mt-4">
            <el-table-column prop="domain" label="域" width="70" />
            <el-table-column prop="transport" label="承载" width="75" />
            <el-table-column label="注册" width="75"><template #default="{ row }">{{ row.registered ? '已注册' : row.searching ? '搜索中' : '未注册' }}</template></el-table-column>
            <el-table-column prop="registered_plmn" label="PLMN" width="90" />
            <el-table-column prop="network_mode" label="制式" width="80" />
            <el-table-column prop="reject_cause" label="拒绝原因" width="90" />
            <el-table-column prop="cell_identity" label="小区身份" min-width="260" show-overflow-tooltip />
          </el-table>
        </el-collapse-item>
      </el-collapse>
    </template>

    <div v-else-if="editConfig" class="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <div class="space-y-1"><label class="text-xs font-bold text-gray-500">ID</label><el-input v-model="editConfig.id" disabled /></div>
      <div class="space-y-1"><label class="text-xs font-bold text-gray-500">名称</label><el-input v-model="editConfig.name" /></div>
      <div class="space-y-1"><label class="text-xs font-bold text-gray-500">IMEI 绑定</label><el-input v-model="editConfig.modem_imei" disabled /></div>
      <div class="space-y-1"><label class="text-xs font-bold text-gray-500">设备路径</label><el-input :model-value="activeUsbPath || ''" disabled /></div>
      <div class="space-y-1"><label class="text-xs font-bold text-gray-500">网卡接口</label><el-input :model-value="activeInterface || ''" disabled /></div>
      <div class="space-y-1"><label class="text-xs font-bold text-gray-500">AT 端口</label><el-input :model-value="activeATPort || ''" disabled /></div>
      <div class="space-y-1"><label class="text-xs font-bold text-gray-500">控制设备</label><el-input :model-value="activeControlDevice || ''" disabled /></div>
      <div class="ui-panel-muted p-3">
        <div class="text-sm font-bold mb-2">设备运行模式</div>
        <el-select v-model="editConfig.device_backend" class="w-full" :disabled="isQMIBackendOnly || isMBIMBackendOnly">
          <el-option v-if="!isMBIMBackendOnly" label="AT" value="at" :disabled="isQMIBackendOnly" />
          <el-option v-if="!isMBIMBackendOnly" label="QMI" value="qmi" :disabled="!activeControlDevice && editConfig.device_backend !== 'qmi'" />
          <el-option v-if="isMBIMBackendOnly" label="MBIM" value="mbim" />
        </el-select>
      </div>
    </div>
  </div>
</template>

<style scoped>
.config-heading { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 20px; }
.heading-icon { display: grid; width: 40px; height: 40px; place-items: center; border-radius: 12px; color: #0f766e; background: #ccfbf1; }
.android-hero { display: grid; grid-template-columns: auto 1fr auto; gap: 20px; align-items: center; padding: 25px; border: 1px solid rgba(148,163,184,.22); border-radius: 20px; background: linear-gradient(125deg, rgba(15,23,42,.035), rgba(15,118,110,.06)); }
.status-orb { position: relative; display: grid; width: 50px; height: 50px; place-items: center; }
.status-orb span { width: 13px; height: 13px; border-radius: 50%; background: #94a3b8; }
.status-orb i { position: absolute; inset: 7px; border: 1px solid rgba(148,163,184,.35); border-radius: 50%; }
.android-hero.online .status-orb span { background: #10b981; box-shadow: 0 0 18px rgba(16,185,129,.55); }
.android-hero.online .status-orb i { border-color: rgba(16,185,129,.3); animation: breathe 2s infinite; }
.hero-kicker, .panel-kicker { margin: 0; color: #0f766e; font: 800 9px/1.4 'Fira Code', monospace; letter-spacing: .14em; }
.android-hero h3 { margin: 4px 0; color: #0f172a; font-size: 21px; font-weight: 850; }
.android-hero p:last-child { margin: 0; color: #64748b; font-size: 12px; }
.hero-metrics { display: grid; grid-template-columns: repeat(3, auto); gap: 22px; }
.hero-metrics span, .hero-metrics strong { display: block; }
.hero-metrics span { color: #94a3b8; font-size: 9px; }
.hero-metrics strong { margin-top: 4px; color: #334155; font-size: 13px; }
.name-row { display: grid; grid-template-columns: 90px 1fr auto; gap: 12px; align-items: center; margin: 17px 0; padding: 12px 15px; border-radius: 13px; background: rgba(148,163,184,.075); }
.name-row label { color: #64748b; font-size: 12px; font-weight: 750; }
.name-row span { color: #94a3b8; font-size: 10px; }
.result-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; }
.result-grid article { display: flex; min-height: 155px; flex-direction: column; padding: 19px; border: 1px solid rgba(148,163,184,.2); border-radius: 16px; background: rgba(148,163,184,.045); }
.result-code { color: #94a3b8; font: 800 9px 'Fira Code', monospace; letter-spacing: .16em; }
.result-grid strong { margin: 17px 0 5px; color: #b45309; font-size: 18px; }
.result-grid article.ready strong { color: #059669; }
.result-grid small { color: #64748b; font-size: 10px; line-height: 1.5; }
.result-grid a { margin-top: auto; padding-top: 15px; color: #0f766e; font-size: 11px; font-weight: 750; text-decoration: none; }
.sim-panel, .esim-panel { display: flex; align-items: center; justify-content: space-between; gap: 20px; margin-top: 16px; padding: 21px; border: 1px solid rgba(148,163,184,.2); border-radius: 16px; }
.sim-panel h3, .panel-title h3 { margin: 4px 0; color: #1e293b; font-size: 16px; }
.sim-panel > div > span, .panel-title > span { color: #64748b; font-size: 11px; }
.sim-control { display: flex; width: min(360px, 100%); gap: 8px; }
.sim-control .el-select { flex: 1; }
.esim-panel { display: block; }
.panel-title { display: flex; align-items: end; justify-content: space-between; margin-bottom: 13px; }
.subscription-list { display: grid; gap: 8px; }
.subscription-list article { display: flex; align-items: center; justify-content: space-between; padding: 12px 13px; border-radius: 11px; background: rgba(148,163,184,.07); }
.subscription-list article.selected { background: rgba(16,185,129,.09); }
.subscription-list strong, .subscription-list small { display: block; }
.subscription-list strong { color: #334155; font-size: 12px; }
.subscription-list small { margin-top: 3px; color: #94a3b8; font-size: 10px; }
.advanced-collapse { margin-top: 18px; border: 0; }
.diagnostic-grid { display: grid; grid-template-columns: repeat(2, 1fr); gap: 10px; }
.diagnostic-grid > div { min-width: 0; padding: 12px; border-radius: 10px; background: rgba(148,163,184,.07); }
.diagnostic-grid span, .diagnostic-grid strong { display: block; }
.diagnostic-grid span { color: #94a3b8; font-size: 9px; }
.diagnostic-grid strong { overflow: hidden; margin-top: 4px; color: #475569; font: 600 11px 'Fira Code', monospace; text-overflow: ellipsis; white-space: nowrap; }
@keyframes breathe { 50% { transform: scale(1.17); opacity: .35; } }
:global(.dark) .heading-icon { color: #5eead4; background: rgba(13,148,136,.16); }
:global(.dark) .android-hero h3, :global(.dark) .sim-panel h3, :global(.dark) .panel-title h3 { color: #f1f5f9; }
:global(.dark) .hero-metrics strong, :global(.dark) .subscription-list strong, :global(.dark) .diagnostic-grid strong { color: #cbd5e1; }
@media (max-width: 760px) {
  .config-heading { align-items: flex-start; }
  .android-hero { grid-template-columns: auto 1fr; }
  .hero-metrics { grid-column: 1 / -1; grid-template-columns: repeat(3, 1fr); }
  .result-grid { grid-template-columns: 1fr; }
  .sim-panel { align-items: flex-start; flex-direction: column; }
  .sim-control { width: 100%; }
  .name-row { grid-template-columns: 1fr; }
}
</style>
