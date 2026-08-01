<script setup lang="ts">
import { computed, watch } from 'vue'
import type { DeviceConfigDTO, DiscoveredDevice } from '../types/api'
import { isWwanQmiControlPath } from '../utils/deviceBackend'
import { ArrowSync24Regular, PhoneAdd24Regular, Save24Regular } from '@vicons/fluent'

const props = defineProps<{
  modelValue: boolean
  discovering: boolean
  unconfiguredDiscovered: DiscoveredDevice[]
  addSelected: DiscoveredDevice | null
  addConfig: DeviceConfigDTO
  addSaving: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  'select-device': [device: DiscoveredDevice]
  save: []
}>()

const isAndroid = computed(() => props.addConfig.device_kind === 'android' || props.addConfig.device_backend === 'android')
const isQMIBackendOnly = computed(() => !isAndroid.value && isWwanQmiControlPath(props.addSelected?.control_path || props.addConfig?.control_device))
const isMBIMBackendOnly = computed(() => !isAndroid.value && String(props.addSelected?.mode || '').toLowerCase() === 'mbim')

function setKind(kind: 'modem' | 'android') {
  props.addConfig.device_kind = kind
  if (kind === 'android') {
    props.addConfig.device_backend = 'android'
    props.addConfig.interface = ''
    props.addConfig.at_port = ''
    props.addConfig.control_device = ''
    props.addConfig.usb_path = ''
    props.addConfig.modem_imei = props.addConfig.android_agent_id || ''
  } else {
    props.addConfig.device_backend = 'at'
    props.addConfig.android_agent_id = ''
    props.addConfig.modem_imei = ''
  }
}

function discoveryIdentity(d: DiscoveredDevice | null | undefined): string {
  if (!d) return ''
  return String(d.discovery_key || `${d.usb_path || ''}|${d.at_port || ''}`)
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

watch(isQMIBackendOnly, (locked) => {
  if (locked) props.addConfig.device_backend = 'qmi'
}, { immediate: true })

watch(isMBIMBackendOnly, (locked) => {
  if (locked) props.addConfig.device_backend = 'mbim'
}, { immediate: true })

watch(() => props.addConfig.android_agent_id, (value) => {
  if (isAndroid.value) props.addConfig.modem_imei = String(value || '').trim()
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
    <div class="grid grid-cols-2 gap-2 p-1 rounded-xl bg-gray-100 dark:bg-gray-800 mb-5">
      <button
        type="button"
        class="py-2.5 rounded-lg text-sm font-bold transition"
        :class="!isAndroid ? 'bg-white text-indigo-600 shadow-sm dark:bg-gray-700' : 'text-gray-500'"
        @click="setKind('modem')"
      >USB 模组</button>
      <button
        type="button"
        class="py-2.5 rounded-lg text-sm font-bold transition flex items-center justify-center gap-2"
        :class="isAndroid ? 'bg-white text-indigo-600 shadow-sm dark:bg-gray-700' : 'text-gray-500'"
        @click="setKind('android')"
      >
        <el-icon><PhoneAdd24Regular /></el-icon>Android Agent
      </button>
    </div>

    <template v-if="!isAndroid">
      <div class="text-sm text-gray-500 mb-3">选择一个未配置的 USB 设备，系统自动填充端口和识别信息。</div>
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
    </template>

    <div v-else class="p-4 rounded-xl border border-indigo-100 bg-indigo-50/60 dark:bg-indigo-500/10 mb-4">
      <div class="font-bold text-indigo-900 dark:text-indigo-200">同局域网 Android 接入</div>
      <div class="text-xs text-indigo-700 dark:text-indigo-300 mt-1">
        保存后在设备配置中生成配对 Token，再填入 Android App。Agent ID 必须与 App 中一致。
      </div>
    </div>

    <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 mt-4">
      <div class="space-y-1">
        <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">ID</label>
        <el-input v-model="addConfig.id" :placeholder="isAndroid ? '例如 android_01' : '例如 ec20_3'" />
      </div>
      <div class="space-y-1">
        <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">名称</label>
        <el-input v-model="addConfig.name" placeholder="显示名称（可选）" />
      </div>

      <template v-if="isAndroid">
        <div class="space-y-1 sm:col-span-2">
          <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">Agent ID</label>
          <el-input v-model="addConfig.android_agent_id" placeholder="Android App 显示的 UUID" />
        </div>
      </template>

      <template v-else>
        <div class="space-y-1"><label class="text-xs font-bold text-gray-500">IMEI 绑定</label><el-input v-model="addConfig.modem_imei" disabled /></div>
        <div class="space-y-1"><label class="text-xs font-bold text-gray-500">USB 路径</label><el-input v-model="addConfig.usb_path" disabled /></div>
        <div class="space-y-1"><label class="text-xs font-bold text-gray-500">网卡接口</label><el-input v-model="addConfig.interface" disabled /></div>
        <div class="space-y-1"><label class="text-xs font-bold text-gray-500">AT 端口</label><el-input v-model="addConfig.at_port" disabled /></div>
        <div class="space-y-1"><label class="text-xs font-bold text-gray-500">控制设备</label><el-input v-model="addConfig.control_device" disabled /></div>
        <div class="p-3 rounded-xl border border-gray-200 bg-gray-50">
          <div class="text-sm font-bold mb-2">设备后端模式</div>
          <el-select v-model="addConfig.device_backend" class="w-full" :disabled="isQMIBackendOnly || isMBIMBackendOnly">
            <el-option v-if="!isMBIMBackendOnly" label="AT" value="at" :disabled="isQMIBackendOnly" />
            <el-option v-if="!isMBIMBackendOnly" label="QMI" value="qmi" :disabled="!addConfig.control_device" />
            <el-option v-if="isMBIMBackendOnly" label="MBIM" value="mbim" />
          </el-select>
        </div>
      </template>
    </div>

    <template #footer>
      <div class="flex justify-end gap-2">
        <el-button @click="emit('update:modelValue', false)">取消</el-button>
        <el-button type="primary" :loading="addSaving" @click="emit('save')" class="!border-0">
          <el-icon><Save24Regular /></el-icon>保存
        </el-button>
      </div>
    </template>
  </el-dialog>
</template>
