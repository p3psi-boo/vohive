<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Loading } from '@element-plus/icons-vue'
import type { CardPolicy } from '../types/api'
import { cardsService } from '../services/cards'
import { ArrowRight24Regular } from '@vicons/fluent'

const props = defineProps<{
  deviceId: string
  iccid: string
  isActiveCard: boolean
  deviceOnline: boolean
}>()

const emit = defineEmits<{
  editPolicy: []
}>()

const policy = ref<CardPolicy | null>(null)
const loadFailed = ref(false)
const loading = ref(false)

const sourceLabel = computed(() => {
  if (!policy.value) return ''
  return policy.value.source === 'user' ? '手动设置' : '自动默认'
})

const policyItems = computed(() => [
  { label: '网络', enabled: policy.value?.network_enabled === true },
  { label: 'VoWiFi', enabled: policy.value?.vowifi_enabled === true },
  { label: '飞行', enabled: policy.value?.airplane_enabled === true }
])

async function loadPolicy() {
  loading.value = true
  loadFailed.value = false
  const r = await cardsService.getPolicy(props.iccid)
  loading.value = false
  if (r.ok) {
    policy.value = r.data
  } else {
    loadFailed.value = true
  }
}

onMounted(loadPolicy)
</script>

<template>
  <div class="px-4 py-3 bg-gray-50/60 dark:bg-white/5 rounded-lg space-y-3">
    <div v-if="loading" class="text-xs text-gray-400 flex items-center gap-1">
      <el-icon class="animate-spin"><Loading /></el-icon> 正在加载策略...
    </div>
    <div v-else-if="loadFailed" class="text-xs text-orange-500 flex items-center gap-2">
      策略加载失败
      <el-button size="small" text @click="loadPolicy">重试</el-button>
    </div>
    <template v-else>
      <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
        <div class="flex flex-wrap items-center gap-2 min-w-0">
          <span class="text-xs font-bold text-gray-500 dark:text-gray-400">卡策略</span>
          <el-tag v-if="sourceLabel" size="small" :type="policy?.source === 'user' ? 'primary' : 'info'">{{ sourceLabel }}</el-tag>
          <el-tag v-for="item in policyItems" :key="item.label" size="small" :type="item.enabled ? 'success' : 'info'">
            {{ item.label }}{{ item.enabled ? '开' : '关' }}
          </el-tag>
        </div>
        <el-button size="small" type="primary" plain @click="emit('editPolicy')">
          去卡策略页编辑
          <el-icon class="ml-1"><ArrowRight24Regular /></el-icon>
        </el-button>
      </div>
    </template>
  </div>
</template>
