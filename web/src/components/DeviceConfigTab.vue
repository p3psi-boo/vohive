<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Copy24Regular,
  Delete24Regular,
  Key24Regular,
  Phone24Regular,
  Save24Regular,
  ArrowSync24Regular
} from '@vicons/fluent'
import type { AndroidAgentStatus, AndroidSMSMessage, AndroidSubscription, DeviceConfigDTO, DeviceOverviewItem } from '../types/api'
import { devicesService } from '../services/devices'
import { copyToClipboard } from '../utils/clipboard'
import { isWwanQmiControlPath } from '../utils/deviceBackend'

const props = defineProps<{
  editConfig: DeviceConfigDTO | null
  deviceStatus?: DeviceOverviewItem | null
  saving: boolean
  deleting: boolean
}>()

const emit = defineEmits<{ save: []; delete: [] }>()

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
const pairingLoading = ref(false)
const pairingToken = ref('')
const pairingExpiresAt = ref('')
const selectedSubscriptionID = ref<number | null>(null)
const subscriptionActionLoading = ref(false)
const androidMessages = ref<AndroidSMSMessage[]>([])
const smsLoading = ref(false)

watch(isQMIBackendOnly, (locked) => {
  if (locked && props.editConfig) props.editConfig.device_backend = 'qmi'
}, { immediate: true })

watch(
  () => [isAndroid.value, props.editConfig?.id],
  ([android]) => {
    pairingToken.value = ''
    pairingExpiresAt.value = ''
    agentStatus.value = null
    subscriptions.value = []
    androidMessages.value = []
    if (android) void refreshAndroidAgent()
  },
  { immediate: true }
)

async function refreshAndroidAgent() {
  const id = String(props.editConfig?.id || '').trim()
  if (!id || !isAndroid.value) return
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
    ElMessage.error(error instanceof Error ? error.message : 'Android Agent 状态读取失败')
  } finally {
    androidLoading.value = false
  }
}

async function issuePairingToken() {
  const id = String(props.editConfig?.id || '').trim()
  if (!id) return
  pairingLoading.value = true
  try {
    const result = await devicesService.issueAndroidPairingToken(id)
    if (!result.ok) throw new Error(result.error.message)
    pairingToken.value = result.data.token
    pairingExpiresAt.value = result.data.expires_at
    ElMessage.success('配对 Token 已生成，5 分钟内有效')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '生成失败')
  } finally {
    pairingLoading.value = false
  }
}

async function copyPairingToken() {
  if (!pairingToken.value) return
  await copyToClipboard(pairingToken.value, 'Token 已复制')
}

async function selectSubscription() {
  const id = String(props.editConfig?.id || '').trim()
  if (!id || selectedSubscriptionID.value == null) return
  subscriptionActionLoading.value = true
  try {
    const result = await devicesService.selectAndroidSubscription(id, selectedSubscriptionID.value)
    if (!result.ok) throw new Error(result.error.message)
    ElMessage.success('Agent 使用订阅已切换')
    await refreshAndroidAgent()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '订阅切换失败')
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
    ElMessage.success('eSIM 切换请求已下发；需要用户确认时请查看手机')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : 'eSIM 切换失败')
  } finally {
    subscriptionActionLoading.value = false
  }
}

async function openESIMSettings() {
  const id = String(props.editConfig?.id || '').trim()
  if (!id) return
  const result = await devicesService.openAndroidESIMSettings(id)
  if (!result.ok) ElMessage.error(result.error.message)
  else ElMessage.success('已请求手机打开 eSIM 管理')
}

async function refreshAndroidSMS() {
  const id = String(props.editConfig?.id || '').trim()
  if (!id || !agentStatus.value?.online) return
  smsLoading.value = true
  try {
    const result = await devicesService.listAndroidSMS(id)
    if (!result.ok) throw new Error(result.error.message)
    androidMessages.value = result.data
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '手机短信查询失败')
  } finally {
    smsLoading.value = false
  }
}

async function deleteAndroidSMS(index: number) {
  const id = String(props.editConfig?.id || '').trim()
  if (!id) return
  const confirmed = await ElMessageBox.confirm(
    '将从 Android 系统短信库删除这条短信。',
    '删除手机短信',
    { type: 'warning' }
  ).then(() => true).catch(() => false)
  if (!confirmed) return
  const result = await devicesService.deleteAndroidSMS(id, index)
  if (!result.ok) {
    ElMessage.error(result.error.message)
    return
  }
  androidMessages.value = androidMessages.value.filter(item => item.index !== index)
  ElMessage.success('手机短信已删除')
}

async function deleteAllAndroidSMS() {
  const id = String(props.editConfig?.id || '').trim()
  if (!id) return
  const confirmed = await ElMessageBox.confirm(
    '将清空 Android 系统短信库中的全部短信。',
    '清空手机短信',
    { type: 'warning', confirmButtonText: '清空' }
  ).then(() => true).catch(() => false)
  if (!confirmed) return
  const result = await devicesService.deleteAllAndroidSMS(id)
  if (!result.ok) {
    ElMessage.error(result.error.message)
    return
  }
  androidMessages.value = []
  ElMessage.success('手机短信已清空')
}

function smsPeer(item: AndroidSMSMessage) {
  return item.sender || item.recipient || '--'
}

function smsDirection(item: AndroidSMSMessage) {
  return item.type === 1 ? '接收' : item.type === 2 ? '发送' : '其他'
}

function metric(value: unknown, unit = '') {
  return value === undefined || value === null || value === '' ? '--' : `${value}${unit}`
}
</script>

<template>
  <div>
    <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 mb-5">
      <div class="flex items-center gap-3">
        <div class="w-10 h-10 rounded-xl bg-indigo-50 flex items-center justify-center text-indigo-600">
          <el-icon size="22"><Phone24Regular v-if="isAndroid" /><Key24Regular v-else /></el-icon>
        </div>
        <div>
          <div class="text-lg font-bold text-gray-900 dark:text-white">{{ isAndroid ? 'Android Agent' : '设备配置' }}</div>
          <div class="text-xs text-gray-500">{{ isAndroid ? '配对、订阅、eSIM 与实时 Telephony 状态' : '设备绑定与运行后端配置' }}</div>
        </div>
      </div>
      <div class="flex items-center gap-2">
        <el-button type="danger" :loading="deleting" @click="emit('delete')"><el-icon><Delete24Regular /></el-icon>删除设备</el-button>
        <el-button type="primary" :loading="saving" @click="emit('save')"><el-icon><Save24Regular /></el-icon>保存配置</el-button>
      </div>
    </div>

    <template v-if="editConfig && isAndroid">
      <div class="grid grid-cols-1 lg:grid-cols-2 gap-4 mb-5">
        <div class="space-y-1"><label class="text-xs font-bold text-gray-500">ID</label><el-input v-model="editConfig.id" disabled /></div>
        <div class="space-y-1"><label class="text-xs font-bold text-gray-500">名称</label><el-input v-model="editConfig.name" /></div>
        <div class="space-y-1 lg:col-span-2"><label class="text-xs font-bold text-gray-500">Agent ID</label><el-input v-model="editConfig.android_agent_id" /></div>
      </div>

      <div class="grid grid-cols-1 lg:grid-cols-2 gap-4 mb-5">
        <div class="ui-panel-muted p-4">
          <div class="flex items-center justify-between mb-3">
            <div>
              <div class="font-bold">连接与配对</div>
              <div class="text-xs text-gray-500">状态：<span :class="agentStatus?.online ? 'text-emerald-600' : 'text-gray-500'">{{ agentStatus?.online ? '在线' : '离线' }}</span></div>
            </div>
            <el-button :loading="androidLoading" circle @click="refreshAndroidAgent"><el-icon><ArrowSync24Regular /></el-icon></el-button>
          </div>
          <el-button type="primary" :loading="pairingLoading" @click="issuePairingToken"><el-icon><Key24Regular /></el-icon>生成配对 Token</el-button>
          <div v-if="pairingToken" class="mt-3 flex gap-2">
            <el-input :model-value="pairingToken" readonly />
            <el-button @click="copyPairingToken"><el-icon><Copy24Regular /></el-icon></el-button>
          </div>
          <div v-if="pairingExpiresAt" class="text-xs text-gray-500 mt-1">有效期至 {{ new Date(pairingExpiresAt).toLocaleString() }}</div>
          <div v-if="agentStatus?.snapshot?.access" class="mt-3 flex flex-wrap gap-1">
            <el-tag size="small" :type="agentStatus.snapshot.access.default_sms_app ? 'success' : 'warning'">默认短信 {{ agentStatus.snapshot.access.default_sms_app ? '是' : '否' }}</el-tag>
            <el-tag size="small" :type="agentStatus.snapshot.access.carrier_privileges ? 'success' : 'info'">运营商特权 {{ agentStatus.snapshot.access.carrier_privileges ? '有' : '无' }}</el-tag>
            <el-tag size="small" :type="agentStatus.snapshot.access.read_privileged_phone_state ? 'success' : 'info'">完整标识符 {{ agentStatus.snapshot.access.read_privileged_phone_state ? '可用' : '受限' }}</el-tag>
            <el-tag size="small" :type="agentStatus.snapshot.access.write_embedded_subscriptions ? 'success' : 'info'">eSIM 特权 {{ agentStatus.snapshot.access.write_embedded_subscriptions ? '可用' : '需确认' }}</el-tag>
          </div>
        </div>

        <div class="ui-panel-muted p-4">
          <div class="font-bold mb-3">设备信息</div>
          <div class="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
            <div class="text-gray-500">IMEI</div><div class="font-mono truncate">{{ metric(agentStatus?.snapshot?.imei) }}</div>
            <div class="text-gray-500">IMSI</div><div class="font-mono truncate">{{ metric(agentStatus?.snapshot?.imsi) }}</div>
            <div class="text-gray-500">ICCID</div><div class="font-mono truncate">{{ metric(agentStatus?.snapshot?.iccid) }}</div>
            <div class="text-gray-500">手机号</div><div>{{ metric(agentStatus?.snapshot?.msisdn) }}</div>
            <div class="text-gray-500">电池</div><div>{{ metric(agentStatus?.snapshot?.battery_pct, '%') }} {{ agentStatus?.snapshot?.battery_charging ? '· 充电中' : '' }}</div>
            <div class="text-gray-500">固件</div><div class="truncate" :title="agentStatus?.snapshot?.firmware">{{ metric(agentStatus?.snapshot?.firmware) }}</div>
            <div class="text-gray-500">基带</div><div class="truncate" :title="agentStatus?.snapshot?.baseband">{{ metric(agentStatus?.snapshot?.baseband) }}</div>
          </div>
        </div>
      </div>

      <div class="ui-panel-muted p-4 mb-5">
        <div class="font-bold mb-3">无线与注册状态</div>
        <div class="grid grid-cols-2 sm:grid-cols-4 lg:grid-cols-8 gap-3">
          <div><div class="text-xs text-gray-500">网络</div><div class="font-bold">{{ metric(agentStatus?.snapshot?.network_mode) }}</div></div>
          <div><div class="text-xs text-gray-500">RSSI</div><div class="font-bold">{{ metric(agentStatus?.snapshot?.signal_dbm, ' dBm') }}</div></div>
          <div><div class="text-xs text-gray-500">RSRP</div><div class="font-bold">{{ metric(agentStatus?.snapshot?.signal_rsrp, ' dBm') }}</div></div>
          <div><div class="text-xs text-gray-500">RSRQ</div><div class="font-bold">{{ metric(agentStatus?.snapshot?.signal_rsrq, ' dB') }}</div></div>
          <div><div class="text-xs text-gray-500">SINR</div><div class="font-bold">{{ metric(agentStatus?.snapshot?.signal_sinr, ' dB') }}</div></div>
          <div><div class="text-xs text-gray-500">注册</div><div class="font-bold">{{ metric(agentStatus?.snapshot?.reg_status_text) }}</div></div>
          <div><div class="text-xs text-gray-500">运营商</div><div class="font-bold truncate">{{ metric(agentStatus?.snapshot?.operator) }}</div></div>
          <div><div class="text-xs text-gray-500">蜂窝 IP</div><div class="font-bold truncate">{{ metric(agentStatus?.snapshot?.private_ip || agentStatus?.snapshot?.private_ipv6) }}</div></div>
        </div>
        <el-table v-if="agentStatus?.snapshot?.registration_details?.length" :data="agentStatus.snapshot.registration_details" size="small" class="mt-4">
          <el-table-column prop="domain" label="域" width="70" />
          <el-table-column prop="transport" label="承载" width="75" />
          <el-table-column label="注册" width="75"><template #default="{ row }">{{ row.registered ? '已注册' : (row.searching ? '搜索中' : '未注册') }}</template></el-table-column>
          <el-table-column prop="registered_plmn" label="PLMN" width="90" />
          <el-table-column prop="network_mode" label="制式" width="80" />
          <el-table-column prop="reject_cause" label="拒绝原因" width="90" />
          <el-table-column prop="cell_identity" label="小区身份" min-width="260" show-overflow-tooltip />
        </el-table>
      </div>

      <div class="ui-panel-muted p-4">
        <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 mb-4">
          <div>
            <div class="font-bold">多 SIM / eSIM 选择</div>
            <div class="text-xs text-gray-500">选择后短信和蜂窝代理都使用该 subscriptionId</div>
          </div>
          <div class="flex gap-2">
            <el-select v-model="selectedSubscriptionID" placeholder="选择订阅" style="width: 260px">
              <el-option v-for="item in subscriptions" :key="item.subscription_id" :value="item.subscription_id" :disabled="!item.active" :label="`${item.embedded ? 'eSIM' : 'SIM'} · ${item.carrier_name || item.display_name || '--'} · ${item.subscription_id}${item.active ? '' : ' · 未激活'}`" />
            </el-select>
            <el-button type="primary" :loading="subscriptionActionLoading" @click="selectSubscription">应用</el-button>
          </div>
        </div>
        <el-table :data="subscriptions" size="small" empty-text="Agent 离线或没有可访问订阅">
          <el-table-column label="类型" width="75"><template #default="{ row }"><el-tag size="small">{{ row.embedded ? 'eSIM' : 'SIM' }}</el-tag></template></el-table-column>
          <el-table-column prop="slot_index" label="Slot" width="65" />
          <el-table-column prop="carrier_name" label="运营商" min-width="120" />
          <el-table-column prop="imei" label="IMEI" min-width="155" show-overflow-tooltip />
          <el-table-column prop="imsi" label="IMSI" min-width="155" show-overflow-tooltip />
          <el-table-column prop="iccid" label="ICCID" min-width="180" />
          <el-table-column prop="msisdn" label="手机号" min-width="120" />
          <el-table-column label="状态" width="110"><template #default="{ row }"><span>{{ row.selected ? 'Agent 当前' : (row.active ? '活动' : '未激活') }}</span></template></el-table-column>
          <el-table-column label="操作" width="100"><template #default="{ row }"><el-button v-if="row.embedded && !row.selected" link type="primary" :loading="subscriptionActionLoading" @click="switchESIM(row)">切换 eSIM</el-button></template></el-table-column>
        </el-table>
        <div class="mt-3 flex items-center justify-between">
          <div class="text-xs text-gray-500">
            EID：{{ metric(agentStatus?.snapshot?.eid) }}
            <span v-if="agentStatus?.snapshot?.esim_operation?.state">
              · 最近切换：{{ agentStatus.snapshot.esim_operation.state }}
              (subId {{ agentStatus.snapshot.esim_operation.subscription_id ?? '--' }},
              code {{ agentStatus.snapshot.esim_operation.detailed_code ?? agentStatus.snapshot.esim_operation.result_code ?? '--' }})
              <span v-if="agentStatus.snapshot.esim_operation.error">· {{ agentStatus.snapshot.esim_operation.error }}</span>
            </span>
          </div>
          <el-button v-if="agentStatus?.snapshot?.esim_supported" @click="openESIMSettings">打开手机 eSIM 管理</el-button>
        </div>
      </div>

      <div class="ui-panel-muted p-4 mt-5">
        <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 mb-4">
          <div>
            <div class="font-bold">Android 系统短信库</div>
            <div class="text-xs text-gray-500">查询、查看并删除手机本地短信；删除操作需要默认短信应用权限</div>
          </div>
          <div class="flex gap-2">
            <el-button :loading="smsLoading" :disabled="!agentStatus?.online" @click="refreshAndroidSMS">查询短信</el-button>
            <el-button type="danger" plain :disabled="!androidMessages.length" @click="deleteAllAndroidSMS">清空</el-button>
          </div>
        </div>
        <el-table :data="androidMessages" v-loading="smsLoading" size="small" max-height="420" empty-text="点击查询短信读取手机短信库">
          <el-table-column label="方向" width="70"><template #default="{ row }">{{ smsDirection(row) }}</template></el-table-column>
          <el-table-column label="号码" min-width="125"><template #default="{ row }"><span class="font-mono">{{ smsPeer(row) }}</span></template></el-table-column>
          <el-table-column prop="content" label="内容" min-width="260" show-overflow-tooltip />
          <el-table-column label="时间" width="170"><template #default="{ row }">{{ row.timestamp ? new Date(row.timestamp).toLocaleString() : '--' }}</template></el-table-column>
          <el-table-column prop="subscription_id" label="订阅" width="72" />
          <el-table-column label="操作" width="70"><template #default="{ row }"><el-button link type="danger" @click="deleteAndroidSMS(row.index)">删除</el-button></template></el-table-column>
        </el-table>
      </div>
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
