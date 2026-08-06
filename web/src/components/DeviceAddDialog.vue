<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { AndroidEnrollmentCode, DeviceConfigDTO, DiscoveredAndroidAgent, DiscoveredDevice } from '../types/api'
import { isWwanQmiControlPath } from '../utils/deviceBackend'
import QrcodeVue from 'qrcode.vue'
import {
  ArrowSync24Regular,
  CheckmarkCircle24Regular,
  Key24Regular,
  PhoneAdd24Regular,
  QrCode24Regular,
  Save24Regular
} from '@vicons/fluent'

const props = defineProps<{
  modelValue: boolean
  discovering: boolean
  unconfiguredDiscovered: DiscoveredDevice[]
  addSelected: DiscoveredDevice | null
  addConfig: DeviceConfigDTO
  addSaving: boolean
  androidAgents: DiscoveredAndroidAgent[]
  androidDiscovering: boolean
  androidPairingLoading: boolean
  androidPairingCode: AndroidEnrollmentCode | null
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  'select-device': [device: DiscoveredDevice]
  'refresh-android': []
  'approve-android': [agent: DiscoveredAndroidAgent, name: string]
  'create-pairing-code': [name: string]
  save: []
}>()

const isAndroid = computed(() => props.addConfig.device_kind === 'android' || props.addConfig.device_backend === 'android')
const isQMIBackendOnly = computed(() => !isAndroid.value && isWwanQmiControlPath(props.addSelected?.control_path || props.addConfig?.control_device))
const isMBIMBackendOnly = computed(() => !isAndroid.value && String(props.addSelected?.mode || '').toLowerCase() === 'mbim')
const androidName = ref('')
const showFallback = ref(false)

const pairingQrValue = computed(() => {
  if (!props.androidPairingCode) return ''
  return JSON.stringify({
    server_url: props.androidPairingCode.server_url,
    code: props.androidPairingCode.code
  })
})

function setKind(kind: 'modem' | 'android') {
  props.addConfig.device_kind = kind
  if (kind === 'android') {
    props.addConfig.device_backend = 'android'
    emit('refresh-android')
  } else {
    props.addConfig.device_backend = 'at'
    props.addConfig.android_agent_id = ''
    props.addConfig.modem_imei = ''
  }
}

function discoveryIdentity(d: DiscoveredDevice | null | undefined): string {
  if (!d) return ''
  return String(d.discovery_key || (String(d.usb_path || '') + '|' + String(d.at_port || '')))
}

function discoveryModeText(d: DiscoveredDevice | null | undefined): string {
  const mode = String(d?.mode || 'unknown').toLowerCase()
  if (mode === 'qmi') return 'QMI'
  if (mode === 'mbim') return 'MBIM'
  if (mode === 'ecm') return 'ECM'
  if (mode === 'rndis') return 'RNDIS'
  if (mode === 'ncm') return 'NCM'
  return 'UNKNOWN'
}

function shortAddress(agent: DiscoveredAndroidAgent) {
  return agent.address + ' · Agent ' + agent.agent_id.slice(-6)
}

watch(isQMIBackendOnly, (locked) => {
  if (locked) props.addConfig.device_backend = 'qmi'
}, { immediate: true })

watch(isMBIMBackendOnly, (locked) => {
  if (locked) props.addConfig.device_backend = 'mbim'
}, { immediate: true })

watch(() => props.modelValue, (open) => {
  if (!open) return
  androidName.value = ''
  showFallback.value = false
})
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    @update:model-value="emit('update:modelValue', $event)"
    title="添加设备"
    width="min(760px, 94vw)"
    class="glass-modal"
  >
    <div class="kind-switch">
      <button type="button" :class="{ active: !isAndroid }" @click="setKind('modem')">USB 模组</button>
      <button type="button" :class="{ active: isAndroid }" @click="setKind('android')">
        <el-icon><PhoneAdd24Regular /></el-icon>Android 手机
      </button>
    </div>

    <template v-if="!isAndroid">
      <div class="text-sm text-gray-500 mb-3">选择一个未配置的 USB 设备，识别信息会自动填充。</div>
      <div class="max-h-[250px] overflow-auto space-y-2 pr-1">
        <div v-if="discovering" class="py-10 flex flex-col items-center text-gray-400">
          <el-icon class="is-loading mb-3" size="32"><ArrowSync24Regular /></el-icon>
          <div class="text-xs">正在探测设备...</div>
        </div>
        <template v-else>
          <button
            v-for="d in unconfiguredDiscovered"
            :key="discoveryIdentity(d)"
            type="button"
            class="w-full text-left p-3 rounded-xl border"
            :class="[
              d.degraded ? 'border-amber-200 bg-amber-50 cursor-not-allowed opacity-85' : '',
              !d.degraded && discoveryIdentity(addSelected) === discoveryIdentity(d) ? 'border-indigo-300 bg-indigo-50' : 'border-gray-200 hover:bg-gray-50'
            ]"
            @click="emit('select-device', d)"
          >
            <div class="font-bold text-gray-800 flex items-center gap-2">
              <span>{{ d.net_interface || '--' }} · {{ d.driver_name || '--' }}</span>
              <el-tag size="small">{{ discoveryModeText(d) }}</el-tag>
            </div>
            <div class="text-xs text-gray-500 mt-1 truncate">
              {{ d.control_path }} · AT: {{ d.at_port || '--' }} · IMEI: {{ d.imei || '--' }}
            </div>
          </button>
          <div v-if="unconfiguredDiscovered.length === 0" class="text-sm text-gray-500 p-3">暂无可添加模组</div>
        </template>
      </div>

      <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 mt-4">
        <div class="space-y-1"><label class="field-label">ID</label><el-input v-model="addConfig.id" placeholder="例如 ec20_3" /></div>
        <div class="space-y-1"><label class="field-label">名称</label><el-input v-model="addConfig.name" placeholder="显示名称（可选）" /></div>
        <div class="space-y-1"><label class="field-label">IMEI 绑定</label><el-input v-model="addConfig.modem_imei" disabled /></div>
        <div class="space-y-1"><label class="field-label">USB 路径</label><el-input v-model="addConfig.usb_path" disabled /></div>
        <div class="space-y-1"><label class="field-label">网卡接口</label><el-input v-model="addConfig.interface" disabled /></div>
        <div class="space-y-1"><label class="field-label">AT 端口</label><el-input v-model="addConfig.at_port" disabled /></div>
        <div class="space-y-1"><label class="field-label">控制设备</label><el-input v-model="addConfig.control_device" disabled /></div>
        <div class="p-3 rounded-xl border border-gray-200 bg-gray-50 dark:bg-gray-800 dark:border-gray-700">
          <div class="text-sm font-bold mb-2">设备后端模式</div>
          <el-select v-model="addConfig.device_backend" class="w-full" :disabled="isQMIBackendOnly || isMBIMBackendOnly">
            <el-option v-if="!isMBIMBackendOnly" label="AT" value="at" :disabled="isQMIBackendOnly" />
            <el-option v-if="!isMBIMBackendOnly" label="QMI" value="qmi" :disabled="!addConfig.control_device" />
            <el-option v-if="isMBIMBackendOnly" label="MBIM" value="mbim" />
          </el-select>
        </div>
      </div>
    </template>

    <template v-else>
      <div class="pairing-hero">
        <div class="pairing-orbit"><span></span><i></i><i></i></div>
        <div>
          <p class="pairing-kicker">LAN DISCOVERY</p>
          <h3>打开 Agent，设备会出现在这里</h3>
          <p>无需填写地址、UUID 或 Token。确认一次后，VoHive 会自动完成配对并创建 HTTP 与 SOCKS5 代理。</p>
        </div>
        <el-button circle :loading="androidDiscovering" @click="emit('refresh-android')">
          <el-icon><ArrowSync24Regular /></el-icon>
        </el-button>
      </div>

      <div class="space-y-3 mt-4">
        <article v-for="agent in androidAgents" :key="agent.agent_id" class="agent-candidate">
          <div class="agent-glyph"><PhoneAdd24Regular /></div>
          <div class="min-w-0 flex-1">
            <strong>{{ agent.model || 'Android 设备' }}</strong>
            <span>{{ shortAddress(agent) }} · v{{ agent.app_version || '--' }}</span>
          </div>
          <el-button type="primary" :loading="androidPairingLoading" @click="emit('approve-android', agent, androidName)">
            <el-icon><CheckmarkCircle24Regular /></el-icon>允许接入
          </el-button>
        </article>
        <div v-if="androidDiscovering && !androidAgents.length" class="discovery-empty">
          <el-icon class="is-loading"><ArrowSync24Regular /></el-icon>
          <span>正在监听局域网设备…</span>
        </div>
        <div v-else-if="!androidAgents.length" class="discovery-empty">
          <span class="pulse-dot"></span>
          <span>尚未发现 Agent，保持此窗口打开即可自动刷新。</span>
        </div>
      </div>

      <div class="name-line">
        <label>设备名称（可选）</label>
        <el-input v-model="androidName" placeholder="例如：书房备用机" />
      </div>

      <button class="fallback-toggle" type="button" @click="showFallback = !showFallback">
        <el-icon><Key24Regular /></el-icon>
        {{ showFallback ? '收起扫码/手动方式' : '没有自动发现设备？使用扫码或六位配对码' }}
      </button>
      <div v-if="showFallback" class="fallback-panel">
        <template v-if="androidPairingCode">
          <div class="qr-pairing-layout">
            <div class="qr-box">
              <QrcodeVue :value="pairingQrValue" :size="140" level="M" render-as="svg" background="#ffffff" foreground="#0f766e" />
              <span class="qr-hint"><el-icon><QrCode24Regular /></el-icon>使用 App 扫码</span>
            </div>
            <div class="qr-info">
              <p>在 Agent App 扫码，或在 Agent 本地网页输入服务器地址和配对码：</p>
              <code class="server-url">{{ androidPairingCode.server_url }}</code>
              <strong class="pair-code">{{ androidPairingCode.code }}</strong>
              <small>配对码五分钟内有效，Agent ID 和设备 ID 会自动绑定。</small>
            </div>
          </div>
        </template>
        <template v-else>
          <p>自动发现不可用时，生成临时配对二维码与六位码，在 Agent App 扫码或在 Agent 本地网页中输入即可。</p>
          <el-button type="primary" plain :loading="androidPairingLoading" @click="emit('create-pairing-code', androidName)">生成配对二维码 / 六位码</el-button>
        </template>
      </div>
    </template>

    <template #footer>
      <div class="flex justify-end gap-2">
        <el-button @click="emit('update:modelValue', false)">{{ isAndroid ? '完成' : '取消' }}</el-button>
        <el-button v-if="!isAndroid" type="primary" :loading="addSaving" @click="emit('save')" class="!border-0">
          <el-icon><Save24Regular /></el-icon>保存
        </el-button>
      </div>
    </template>
  </el-dialog>
</template>

<style scoped>
.kind-switch { display: grid; grid-template-columns: 1fr 1fr; gap: 4px; padding: 4px; margin-bottom: 20px; border-radius: 14px; background: rgba(100,116,139,.1); }
.kind-switch button { display: flex; align-items: center; justify-content: center; gap: 8px; padding: 11px; border-radius: 10px; color: #64748b; font-size: 13px; font-weight: 750; transition: .2s ease; }
.kind-switch button.active { color: #0f766e; background: white; box-shadow: 0 4px 14px rgba(15,23,42,.08); }
.field-label { color: #64748b; font-size: 11px; font-weight: 800; letter-spacing: .08em; text-transform: uppercase; }
.pairing-hero { display: grid; grid-template-columns: auto 1fr auto; gap: 18px; align-items: center; padding: 22px; border: 1px solid rgba(13,148,136,.2); border-radius: 18px; background: linear-gradient(135deg, rgba(20,184,166,.12), rgba(240,253,250,.55)); }
.pairing-hero h3 { margin: 2px 0 5px; color: #134e4a; font-size: 18px; font-weight: 800; }
.pairing-hero p { margin: 0; color: #47716d; font-size: 12px; line-height: 1.6; }
.pairing-kicker { color: #0f766e !important; font-size: 9px !important; font-weight: 900; letter-spacing: .18em; }
.pairing-orbit { position: relative; display: grid; place-items: center; width: 48px; height: 48px; border: 1px solid rgba(13,148,136,.3); border-radius: 50%; }
.pairing-orbit span { width: 13px; height: 13px; border-radius: 50%; background: #0d9488; box-shadow: 0 0 20px #2dd4bf; }
.pairing-orbit i { position: absolute; inset: 6px; border: 1px solid rgba(13,148,136,.28); border-radius: 50%; animation: orbit 2.5s ease-in-out infinite; }
.pairing-orbit i:last-child { inset: -2px; animation-delay: .5s; }
.agent-candidate { display: flex; align-items: center; gap: 13px; padding: 14px; border: 1px solid rgba(148,163,184,.25); border-radius: 15px; background: rgba(255,255,255,.72); }
.agent-candidate strong, .agent-candidate span { display: block; }
.agent-candidate strong { color: #172554; font-size: 14px; }
.agent-candidate span { overflow: hidden; margin-top: 3px; color: #64748b; font-family: 'Fira Code', monospace; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.agent-glyph { display: grid; place-items: center; width: 36px; height: 36px; border-radius: 10px; color: #0f766e; background: #ccfbf1; }
.discovery-empty { display: flex; align-items: center; justify-content: center; gap: 10px; min-height: 70px; border: 1px dashed rgba(100,116,139,.3); border-radius: 14px; color: #64748b; font-size: 12px; }
.pulse-dot { width: 8px; height: 8px; border-radius: 50%; background: #14b8a6; box-shadow: 0 0 0 5px rgba(20,184,166,.12); animation: pulse 1.8s infinite; }
.name-line { display: grid; grid-template-columns: 140px 1fr; gap: 12px; align-items: center; margin-top: 16px; }
.name-line label { color: #64748b; font-size: 12px; font-weight: 700; }
.fallback-toggle { display: flex; align-items: center; gap: 7px; margin-top: 20px; color: #64748b; font-size: 12px; }
.fallback-toggle:hover { color: #0f766e; }
.fallback-panel { display: grid; gap: 10px; margin-top: 10px; padding: 16px; border-radius: 14px; background: rgba(15,23,42,.04); color: #475569; font-size: 12px; }
.qr-pairing-layout { display: flex; gap: 18px; align-items: center; }
.qr-box { display: flex; flex-direction: column; align-items: center; gap: 6px; padding: 10px; background: white; border-radius: 12px; border: 1px solid rgba(13,148,136,.3); box-shadow: 0 4px 12px rgba(15,23,42,.05); }
.qr-hint { display: flex; align-items: center; gap: 4px; color: #0f766e; font-size: 11px; font-weight: 700; }
.qr-info { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 6px; }
.server-url { padding: 8px 10px; border-radius: 8px; background: rgba(15,23,42,.08); color: #334155; word-break: break-all; }
.pair-code { color: #0f766e; font-family: 'Fira Code', monospace; font-size: 30px; letter-spacing: .18em; }
.fallback-panel small { color: #94a3b8; }
@keyframes pulse { 50% { opacity: .45; transform: scale(.75); } }
@keyframes orbit { 50% { transform: scale(1.15); opacity: .35; } }
:global(.dark) .kind-switch button.active { color: #5eead4; background: #1e293b; }
:global(.dark) .pairing-hero { background: linear-gradient(135deg, rgba(13,148,136,.18), rgba(15,23,42,.65)); }
:global(.dark) .pairing-hero h3, :global(.dark) .agent-candidate strong { color: #ccfbf1; }
:global(.dark) .pairing-hero p { color: #94a3b8; }
:global(.dark) .agent-candidate { background: rgba(15,23,42,.58); }
:global(.dark) .server-url { color: #cbd5e1; background: rgba(255,255,255,.06); }
@media (max-width: 640px) { .pairing-hero { grid-template-columns: auto 1fr; } .pairing-hero > :last-child { display: none; } .name-line { grid-template-columns: 1fr; } .agent-candidate { align-items: flex-start; flex-wrap: wrap; } .agent-candidate .el-button { width: 100%; } }
</style>
