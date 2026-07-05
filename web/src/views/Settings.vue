<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { storeToRefs } from 'pinia'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useSettingsStore } from '../stores/settings'
import { copyToClipboard } from '../utils/clipboard'
import PageHeader from '../components/PageHeader.vue'
import FieldRow from '../components/FieldRow.vue'
import { 
  Key24Regular, 
  Save24Regular,
  Server24Regular,
  Add20Regular,
  Delete20Regular,
  DocumentText24Regular
} from '@vicons/fluent'

const settingsStore = useSettingsStore()
const {
  systemInfo,
  mcpKey,
  generatedMCPKey,
  loadingMCPKey,
  generatingMCPKey,
  revokingMCPKey,
  loadingNotifications,
  savingNotifications,
  testingTelegram,
  testingFeishu,
  testingQQ,
  testingPushplus,
  testingWebhook,
  testingBark,
  testingEmail,
  changingPassword,
  passwordForm,
  telegramForm,
  feishuForm,
  qqForm,
  webhookSettings,
  barkSettings,
  emailForm,
  pushplusForm
} = storeToRefs(settingsStore)
const activeNotifyTab = ref('telegram')
const notificationBaseline = ref('')
const mcpEndpoint = computed(() => {
  if (typeof window === 'undefined') return '/mcp'
  return `${window.location.origin}/mcp`
})
const mcpCurlTemplate = computed(() => `curl -X POST '${mcpEndpoint.value}' \\
  -H 'Authorization: Bearer <MCP Key>' \\
  -H 'Content-Type: application/json' \\
  -H 'Accept: application/json, text/event-stream' \\
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'`)
const mcpCreatedAtText = computed(() => {
  const raw = String(mcpKey.value.created_at || '').trim()
  if (!raw) return '--'
  const d = new Date(raw)
  if (Number.isNaN(d.getTime())) return raw
  return d.toLocaleString()
})

function sortedRecord(input: Record<string, string> | undefined) {
  const out: Record<string, string> = {}
  for (const key of Object.keys(input || {}).sort()) {
    out[key] = String(input?.[key] ?? '')
  }
  return out
}

function notificationsSnapshot() {
  return JSON.stringify({
    telegram: telegramForm.value,
    feishu: feishuForm.value,
    qq: qqForm.value,
    webhook: {
      ...webhookSettings.value,
      urls: Array.isArray(webhookSettings.value.urls) ? [...webhookSettings.value.urls] : [],
      headers: sortedRecord(webhookSettings.value.headers)
    },
    bark: {
      ...barkSettings.value,
      urls: Array.isArray(barkSettings.value.urls) ? [...barkSettings.value.urls] : []
    },
    email: emailForm.value,
    pushplus: pushplusForm.value
  })
}

function markNotificationsClean() {
  notificationBaseline.value = notificationsSnapshot()
}

const notificationsDirty = computed(() => {
  if (!notificationBaseline.value || loadingNotifications.value) return false
  return notificationsSnapshot() !== notificationBaseline.value
})

const hasValidTelegramConfig = computed(() => {
  return !!(telegramForm.value.bot_token && telegramForm.value.chat_id)
})

const hasValidFeishuConfig = computed(() => {
  return !!(feishuForm.value.app_id && feishuForm.value.app_secret && feishuForm.value.chat_ids)
})

const hasValidQQConfig = computed(() => {
  return !!(
    qqForm.value.app_id &&
    qqForm.value.app_secret &&
    (qqForm.value.group_ids || qqForm.value.direct_ids)
  )
})

const hasValidPushplusConfig = computed(() => {
  return !!pushplusForm.value.token
})

const hasValidWebhookURLs = computed(() => {
  if (!Array.isArray(webhookSettings.value.urls)) {
    return false
  }
  return webhookSettings.value.urls.some((u) => String(u || '').trim().length > 0)
})

const hasValidBarkURLs = computed(() => {
  if (!Array.isArray(barkSettings.value.urls)) {
    return false
  }
  return barkSettings.value.urls.some((u) => String(u || '').trim().length > 0)
})

const hasValidEmailConfig = computed(() => {
  return !!(
    emailForm.value.smtp_host &&
    emailForm.value.smtp_port &&
    emailForm.value.username &&
    emailForm.value.password &&
    emailForm.value.from_address &&
    emailForm.value.to_addresses
  )
})


async function changePassword() {
  if (passwordForm.value.new_password !== passwordForm.value.confirm_password) {
    ElMessage.error('两次输入的新密码不一致')
    return
  }
  
  try {
     const result = await settingsStore.changePasswordFromForm()
     if (!result.ok) throw new Error(result.error.message || '更新失败')
     ElMessage.success('密码已更新')
     settingsStore.resetPasswordForm()
  } catch {
     ElMessage.error('失败：后端尚未实现该功能或请求失败')
  }
}

async function loadSystemInfo() {
  const result = await settingsStore.fetchSystemInfo()
  if (!result.ok) {
    console.error('系统信息读取失败', result.error)
  }
}

async function loadMCPKey() {
  const result = await settingsStore.fetchMCPKey()
  if (!result.ok) {
    ElMessage.error(result.error.message || 'MCP Key 状态读取失败')
  }
}

async function loadNotifications() {
  try {
    const result = await settingsStore.fetchNotifications()
    if (!result.ok) throw new Error(result.error.message || '通知配置加载失败')
    syncWebhookHeaderRowsFromSettings()
    markNotificationsClean()
  } catch {
    ElMessage.error('通知配置加载失败')
  }
}

function openAPIDocs() {
  const docsURL = String(systemInfo.value.docs?.swagger_ui || '').trim()
  if (!docsURL) {
    ElMessage.warning('API 文档入口暂不可用')
    return
  }
  window.open(docsURL, '_blank', 'noopener,noreferrer')
}

async function copyMcpEndpoint() {
  await copyToClipboard(mcpEndpoint.value, 'MCP 地址已复制')
}

async function copyMcpCurlTemplate() {
  await copyToClipboard(mcpCurlTemplate.value, 'MCP 调用模板已复制')
}

async function copyGeneratedMCPKey() {
  if (!generatedMCPKey.value) return
  await copyToClipboard(generatedMCPKey.value, 'MCP Key 已复制')
}

async function generateMCPKey() {
  const confirmed = await ElMessageBox.confirm(
    mcpKey.value.enabled
      ? '生成新的 MCP Key 会立即替换旧 Key，旧客户端会失去访问权限。确定继续？'
      : '生成后只会显示一次明文，请及时复制并保存到可信客户端。确定生成？',
    mcpKey.value.enabled ? '轮换 MCP Key' : '生成 MCP Key',
    {
      confirmButtonText: mcpKey.value.enabled ? '生成新 Key' : '生成',
      cancelButtonText: '取消',
      type: 'warning'
    }
  ).then(() => true).catch(() => false)
  if (!confirmed) return

  const result = await settingsStore.generateMCPKey()
  if (!result.ok) {
    ElMessage.error(result.error.message || 'MCP Key 生成失败')
    return
  }
  ElMessage.success('MCP Key 已生成，请复制保存')
}

async function revokeMCPKey() {
  const confirmed = await ElMessageBox.confirm(
    '撤销后所有使用当前 MCP Key 的客户端会立即失去访问权限。确定撤销？',
    '撤销 MCP Key',
    {
      confirmButtonText: '撤销',
      cancelButtonText: '取消',
      type: 'warning'
    }
  ).then(() => true).catch(() => false)
  if (!confirmed) return

  const result = await settingsStore.revokeMCPKey()
  if (!result.ok) {
    ElMessage.error(result.error.message || 'MCP Key 撤销失败')
    return
  }
  ElMessage.success('MCP Key 已撤销')
}

async function saveNotifications() {
  try {
    const result = await settingsStore.saveNotificationsFromForms()
    if (!result.ok) throw new Error(result.error.message || '通知配置保存失败')
    const applied = result.data.applied
    const warning = result.data.warning
    if (applied === false && warning) {
      ElMessage.warning(warning)
    } else {
      ElMessage.success('通知配置已保存')
    }
    markNotificationsClean()
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '通知配置保存失败')
  }
}

function showSimpleTestResult(channel: string, data: { ok: boolean; message: string }) {
  if (data.ok) {
    ElMessage.success(data.message || '测试通知已发送')
    return
  }
  ElMessage.error(data.message || `${channel} 测试失败`)
}

async function testTelegramNotification() {
  try {
    const result = await settingsStore.testTelegramFromForm()
    if (!result.ok) {
      throw new Error(result.error.message || 'Telegram 测试失败')
    }
    showSimpleTestResult('Telegram', result.data)
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : 'Telegram 测试失败')
  }
}

async function testFeishuNotification() {
  try {
    const result = await settingsStore.testFeishuFromForm()
    if (!result.ok) {
      throw new Error(result.error.message || '飞书测试失败')
    }
    showSimpleTestResult('飞书', result.data)
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : '飞书测试失败')
  }
}

async function testQQNotification() {
  try {
    const result = await settingsStore.testQQFromForm()
    if (!result.ok) {
      throw new Error(result.error.message || 'QQ 测试失败')
    }
    showSimpleTestResult('QQ', result.data)
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : 'QQ 测试失败')
  }
}

async function testPushplusNotification() {
  try {
    const result = await settingsStore.testPushplusFromForm()
    if (!result.ok) {
      throw new Error(result.error.message || 'Pushplus 测试失败')
    }
    showSimpleTestResult('Pushplus', result.data)
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : 'Pushplus 测试失败')
  }
}

async function testWebhookNotification() {
  try {
    const result = await settingsStore.testWebhookFromForm()
    if (!result.ok) {
      throw new Error(result.error.message || 'Webhook 测试失败')
    }
    const data = result.data
    if (data.ok) {
      ElMessage.success(data.message || '测试通知已发送')
      return
    }
    if (Array.isArray(data.failed_urls) && data.failed_urls.length > 0) {
      ElMessage.error(`${data.message}\n失败 URL: ${data.failed_urls.join(', ')}`)
      return
    }
    ElMessage.error(data.message || 'Webhook 测试失败')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : 'Webhook 测试失败')
  }
}

function addWebhookUrl() {
  if (!webhookSettings.value.urls) {
     webhookSettings.value.urls = []
  }
  webhookSettings.value.urls.push('')
}

function removeWebhookUrl(index: number) {
  webhookSettings.value.urls.splice(index, 1)
}

// 自定义请求头以「行」形式编辑（rows 为唯一编辑源），保存时单向回写为 map。
// 受保护的系统头由后端强制覆盖。
const PROTECTED_WEBHOOK_HEADERS = new Set(['content-type', 'x-vohive-signature'])
// 常用请求头预设，下拉可选；filterable + allow-create 也允许自行输入其它名称
const COMMON_WEBHOOK_HEADERS = [
  'Authorization',
  'X-Api-Key',
  'X-Auth-Token',
  'X-Webhook-Token',
  'X-Signature',
  'X-Request-Id',
  'Accept',
  'User-Agent'
]
// 每行带稳定 id，避免用数组下标作 v-for key 时，删除中间行后 el-select 复用实例残留选项
let webhookHeaderUid = 0
const webhookHeaderRows = ref<{ id: number; key: string; value: string }[]>([])

// 加载完成后调用，把已保存的 headers map 转换为可编辑的行
function syncWebhookHeaderRowsFromSettings() {
  const headers = webhookSettings.value.headers || {}
  webhookHeaderRows.value = Object.entries(headers).map(([key, value]) => ({
    id: webhookHeaderUid++,
    key,
    value: String(value ?? '')
  }))
}

// 行变化时单向回写为 map（丢弃空 key 与受保护头）。无反向 watch，故不会回环。
watch(
  webhookHeaderRows,
  (rows) => {
    const map: Record<string, string> = {}
    for (const row of rows) {
      const key = String(row.key || '').trim()
      if (!key || PROTECTED_WEBHOOK_HEADERS.has(key.toLowerCase())) continue
      map[key] = String(row.value ?? '')
    }
    webhookSettings.value.headers = map
  },
  { deep: true }
)

function addWebhookHeader() {
  webhookHeaderRows.value.push({ id: webhookHeaderUid++, key: '', value: '' })
}

function removeWebhookHeader(index: number) {
  webhookHeaderRows.value.splice(index, 1)
}

async function testBarkNotification() {
  try {
    const result = await settingsStore.testBarkFromForm()
    if (!result.ok) {
      throw new Error(result.error.message || 'Bark 测试失败')
    }
    const data = result.data
    if (data.ok) {
      ElMessage.success(data.message || '测试通知已发送')
      return
    }
    if (Array.isArray(data.failed_urls) && data.failed_urls.length > 0) {
      ElMessage.error(`${data.message}\n失败 URL: ${data.failed_urls.join(', ')}`)
      return
    }
    ElMessage.error(data.message || 'Bark 测试失败')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : 'Bark 测试失败')
  }
}

async function testEmailNotification() {
  try {
    const result = await settingsStore.testEmailFromForm()
    if (!result.ok) {
      throw new Error(result.error.message || 'Email 测试失败')
    }
    const data = result.data
    if (data.ok) {
      ElMessage.success(data.message || '测试邮件已发送')
      return
    }
    ElMessage.error(data.message || 'Email 测试失败')
  } catch (e: unknown) {
    ElMessage.error(e instanceof Error ? e.message : 'Email 测试失败')
  }
}

function addBarkUrl() {
  if (!barkSettings.value.urls) {
     barkSettings.value.urls = []
  }
  barkSettings.value.urls.push('')
}

function removeBarkUrl(index: number) {
  barkSettings.value.urls.splice(index, 1)
}



watch(() => emailForm.value.smtp_port, (newPort) => {
  if (Number(newPort) === 465) {
    emailForm.value.use_ssl = true
  }
})

onMounted(() => {
  loadNotifications()
  loadSystemInfo()
  loadMCPKey()
})

onBeforeUnmount(() => {
})

onBeforeRouteLeave(async () => {
  if (!notificationsDirty.value) return true
  const confirmed = await ElMessageBox.confirm(
    '通知配置有未保存的修改，离开后这些修改会丢失。确定离开？',
    '未保存的通知配置',
    {
      confirmButtonText: '离开',
      cancelButtonText: '继续编辑',
      type: 'warning'
    }
  ).then(() => true).catch(() => false)
  return confirmed
})
</script>

<template>
  <div class="max-w-5xl mx-auto">
    <PageHeader title="系统设置" subtitle="管理网关参数与运行信息" />

    <div class="grid grid-cols-1 lg:grid-cols-2 gap-8">
      <!-- Security Card -->
      <div class="ui-card p-8 relative overflow-hidden group">
         <div class="absolute top-0 right-0 w-40 h-40 bg-indigo-500/5 rounded-bl-full -mr-10 -mt-10 transition-transform group-hover:scale-110"></div>
         
         <div class="flex items-center gap-3 mb-6 relative z-10">
            <div class="w-12 h-12 rounded-xl bg-indigo-50 dark:bg-indigo-500/10 flex items-center justify-center text-indigo-600 dark:text-indigo-400">
               <el-icon size="24"><Key24Regular /></el-icon>
            </div>
            <div>
               <h3 class="text-lg font-bold text-gray-800 dark:text-gray-100">安全</h3>
               <p class="text-xs text-gray-500">更新访问凭证</p>
            </div>
         </div>

         <div class="space-y-4 relative z-10">
             <div class="space-y-1">
                <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">当前密码</label>
                <el-input v-model="passwordForm.old_password" type="password" show-password placeholder="••••••••" size="large" />
             </div>
             <div class="space-y-1">
                <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">新密码</label>
                <el-input v-model="passwordForm.new_password" type="password" show-password placeholder="••••••••" size="large" />
             </div>
             <div class="space-y-1">
                <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">确认新密码</label>
                <el-input v-model="passwordForm.confirm_password" type="password" show-password placeholder="••••••••" size="large" />
             </div>
             
             <div class="pt-4">
                 <el-button type="primary" :loading="changingPassword" @click="changePassword" size="large" class="w-full !border-0">
                   <el-icon><Save24Regular /></el-icon>
                   更新凭证
                 </el-button>
             </div>
         </div>
      </div>

      <!-- System Info Card -->
      <div class="ui-card p-8 relative overflow-hidden group">
         <div class="absolute top-0 right-0 w-40 h-40 bg-green-500/5 rounded-bl-full -mr-10 -mt-10 transition-transform group-hover:scale-110"></div>

         <div class="flex items-center gap-3 mb-6 relative z-10">
            <div class="w-12 h-12 rounded-xl bg-green-50 dark:bg-green-500/10 flex items-center justify-center text-green-600 dark:text-green-400">
               <el-icon size="24"><Server24Regular /></el-icon>
            </div>
            <div>
               <h3 class="text-lg font-bold text-gray-800 dark:text-gray-100">系统信息</h3>
               <p class="text-xs text-gray-500">运行环境</p>
            </div>
         </div>

         <div class="space-y-4 text-sm relative z-10">
            <div class="p-3 bg-gray-50 dark:bg-white/5 rounded-lg">
              <FieldRow label="版本" :value="systemInfo.version" monospace>
                <div class="flex items-center justify-end gap-3">
                  <span>{{ systemInfo.version || 'Unknown' }}</span>
                </div>
              </FieldRow>
            </div>
            <div class="p-3 bg-gray-50 dark:bg-white/5 rounded-lg">
              <FieldRow label="构建时间" :value="systemInfo.build_time" monospace />
            </div>
            <div class="p-3 bg-gray-50 dark:bg-white/5 rounded-lg">
              <FieldRow label="配置路径" :value="systemInfo.config" monospace copyable />
            </div>
            <div class="ui-panel-muted px-4 py-4">
              <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
                <div class="min-w-0">
                  <div class="flex items-center gap-3">
                    <div class="w-9 h-9 rounded-xl bg-blue-50 dark:bg-blue-500/10 flex items-center justify-center text-blue-600 dark:text-blue-400">
                      <el-icon size="18"><DocumentText24Regular /></el-icon>
                    </div>
                    <div>
                      <div class="text-sm font-bold text-gray-800 dark:text-gray-100">API 文档</div>
                      <div class="text-xs text-gray-500">打开后端直出的 OpenAPI 页面</div>
                    </div>
                  </div>

                </div>
                <el-button
                  type="primary"
                  class="self-start sm:self-center shrink-0 !border-0"
                  :disabled="!systemInfo.docs?.swagger_ui"
                  @click="openAPIDocs"
                >
                  <el-icon><DocumentText24Regular /></el-icon>
                  打开 API 文档
                </el-button>
              </div>
            </div>
            <div class="ui-panel-muted px-4 py-4">
              <div class="flex flex-col gap-4">
                <div class="flex flex-col sm:flex-row sm:items-start sm:justify-between gap-4">
                  <div class="min-w-0">
                    <div class="flex items-center gap-3">
                      <div class="w-9 h-9 rounded-xl bg-cyan-50 dark:bg-cyan-500/10 flex items-center justify-center text-cyan-600 dark:text-cyan-400">
                        <el-icon size="18"><Server24Regular /></el-icon>
                      </div>
                      <div>
                        <div class="text-sm font-bold text-gray-800 dark:text-gray-100">MCP 服务</div>
                        <div class="text-xs text-gray-500">Streamable HTTP，用于 Agent 调用短信和 eSIM 操作</div>
                      </div>
                    </div>
                  </div>
                  <div class="flex items-center gap-2 shrink-0">
                    <el-tag :type="mcpKey.enabled ? 'success' : 'info'" effect="light">
                      {{ mcpKey.enabled ? 'Key 已启用' : '未生成 Key' }}
                    </el-tag>
                    <el-tag type="warning" effect="light">高风险工具</el-tag>
                  </div>
                </div>

                <div class="space-y-2">
                  <div class="p-3 bg-gray-50 dark:bg-white/5 rounded-lg">
                    <FieldRow label="地址" :value="mcpEndpoint" monospace>
                      <div class="flex items-center justify-end gap-3 min-w-0">
                        <span class="font-mono text-xs truncate">{{ mcpEndpoint }}</span>
                        <el-button text type="primary" class="!px-1 shrink-0" @click="copyMcpEndpoint">复制</el-button>
                      </div>
                    </FieldRow>
                  </div>
                  <div v-if="generatedMCPKey" class="p-3 border border-amber-200 dark:border-amber-500/30 bg-amber-50/80 dark:bg-amber-500/10 rounded-lg">
                    <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
                      <div class="min-w-0">
                        <div class="text-xs font-bold text-amber-700 dark:text-amber-300 mb-1">新 MCP Key 只显示一次</div>
                        <div class="font-mono text-xs text-amber-900 dark:text-amber-100 break-all">{{ generatedMCPKey }}</div>
                      </div>
                      <div class="flex items-center gap-2 shrink-0">
                        <el-button type="primary" class="!border-0" @click="copyGeneratedMCPKey">复制 Key</el-button>
                        <el-button text @click="settingsStore.clearGeneratedMCPKey()">隐藏</el-button>
                      </div>
                    </div>
                  </div>
                  <div class="grid grid-cols-1 sm:grid-cols-2 gap-2 text-xs text-gray-600 dark:text-gray-300">
                    <div class="p-3 bg-gray-50 dark:bg-white/5 rounded-lg">
                      <div class="font-bold text-gray-700 dark:text-gray-100 mb-1">鉴权</div>
                      <div>使用独立 MCP Key，放在 <span class="font-mono">Authorization: Bearer</span> 请求头。</div>
                    </div>
                    <div class="p-3 bg-gray-50 dark:bg-white/5 rounded-lg">
                      <div class="font-bold text-gray-700 dark:text-gray-100 mb-1">当前 Key</div>
                      <div v-if="loadingMCPKey">正在读取...</div>
                      <div v-else-if="mcpKey.enabled">尾号 {{ mcpKey.key_suffix || '----' }}，创建于 {{ mcpCreatedAtText }}</div>
                      <div v-else>尚未生成，MCP 仅接受 Web 登录 token。</div>
                    </div>
                    <div class="p-3 bg-gray-50 dark:bg-white/5 rounded-lg">
                      <div class="font-bold text-gray-700 dark:text-gray-100 mb-1">工具</div>
                      <div><span class="font-mono">send_sms</span>、<span class="font-mono">switch_esim_profile</span></div>
                    </div>
                    <div class="p-3 bg-gray-50 dark:bg-white/5 rounded-lg">
                      <div class="font-bold text-gray-700 dark:text-gray-100 mb-1">传输</div>
                      <div>单端点 JSON-RPC；POST 返回 JSON，GET 不开启 SSE。</div>
                    </div>
                  </div>
                </div>

                <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 border-t border-gray-100 dark:border-white/10 pt-4">
                  <div class="text-xs text-gray-500 dark:text-gray-400">
                    MCP Key 会真实发送短信或切换 eSIM，建议只保存到可信客户端；轮换会立刻替换旧 Key。
                  </div>
                  <div class="flex flex-wrap gap-2">
                    <el-button class="!border-0" @click="copyMcpCurlTemplate">复制调用模板</el-button>
                    <el-button type="primary" class="!border-0" :loading="generatingMCPKey" @click="generateMCPKey">
                      {{ mcpKey.enabled ? '轮换 Key' : '生成 Key' }}
                    </el-button>
                    <el-button
                      v-if="mcpKey.enabled"
                      type="danger"
                      class="!border-0"
                      :loading="revokingMCPKey"
                      @click="revokeMCPKey"
                    >
                      撤销
                    </el-button>
                  </div>
                </div>
              </div>
            </div>
         </div>
      </div>

      <div class="notify-card ui-card p-8 relative overflow-hidden group lg:col-span-2">
         <div class="absolute top-0 right-0 w-40 h-40 bg-purple-500/5 rounded-bl-full -mr-10 -mt-10 transition-transform group-hover:scale-110"></div>

         <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 mb-6 relative z-10">
            <div class="flex items-center gap-3">
               <div class="w-12 h-12 rounded-xl bg-purple-50 dark:bg-purple-500/10 flex items-center justify-center text-purple-600 dark:text-purple-400">
                  <el-icon size="24"><Alert24Regular /></el-icon>
               </div>
               <div>
                  <h3 class="text-lg font-bold text-gray-800 dark:text-gray-100">通知</h3>
                  <p class="text-xs text-gray-500">Telegram / 飞书 / QQ / Webhook</p>
               </div>
            </div>
            <div class="flex items-center gap-2">
              <el-tag v-if="notificationsDirty" type="warning" effect="light">未保存</el-tag>
              <el-button
                type="primary"
                :loading="savingNotifications"
                :disabled="loadingNotifications || !notificationsDirty"
                @click="saveNotifications"
                class="!border-0"
              >
              <el-icon><Save24Regular /></el-icon>
                {{ notificationsDirty ? '保存通知配置' : '已保存' }}
              </el-button>
            </div>
         </div>

         <div v-if="loadingNotifications" class="p-6 text-sm text-gray-500 dark:text-gray-400">正在加载通知配置…</div>

         <div v-else class="relative z-10 w-full overflow-hidden">
            <el-tabs v-model="activeNotifyTab" class="settings-notify-tabs">
              <!-- Telegram -->
              <el-tab-pane label="Telegram 机器人" name="telegram" class="pt-2">
                <div class="flex items-center justify-between mb-4">
                  <div class="flex items-center gap-2">
                    <div class="font-bold text-gray-800 dark:text-gray-100">启用 Telegram 机器人</div>
                  </div>
                  <div class="flex items-center gap-2">
                    <el-button
                      size="small"
                      type="primary"
                      plain
                      :loading="testingTelegram"
                      :disabled="!telegramForm.enabled || !hasValidTelegramConfig"
                      @click="testTelegramNotification"
                    >
                      测试通知
                    </el-button>
                    <el-switch v-model="telegramForm.enabled" />
                  </div>
                </div>

                <div class="space-y-4">
                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">机器人令牌（Bot Token）</label>
                    <el-input v-model="telegramForm.bot_token" :disabled="!telegramForm.enabled" placeholder="xxxx:yyyy" />
                  </div>
                  <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
                    <div class="space-y-1">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">会话 ID（Chat ID）</label>
                      <el-input v-model="telegramForm.chat_id" :disabled="!telegramForm.enabled" type="number" inputmode="numeric" placeholder="例如 123456" />
                    </div>
                    <div class="space-y-1">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">管理员 ID（Admin ID）</label>
                      <el-input v-model="telegramForm.admin_id" :disabled="!telegramForm.enabled" type="number" inputmode="numeric" placeholder="例如 123456" />
                    </div>
                  </div>
                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">TG API 反代（可选）</label>
                    <el-input v-model="telegramForm.base_url" :disabled="!telegramForm.enabled" placeholder="留空直连 api.telegram.org；需要反代时填写" />
                    <div class="text-[10px] text-gray-400 mt-1">反向代理地址 (例如 https://api.telegram.org/bot%s/%s)</div>
                  </div>
                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">HTTP 代理（可选）</label>
                    <el-input v-model="telegramForm.proxy" :disabled="!telegramForm.enabled" placeholder="例如 http://127.0.0.1:7890" />
                    <div class="text-[10px] text-gray-400 mt-1">用于连接 API 服务器的 HTTP 代理</div>
                  </div>
                </div>
              </el-tab-pane>

              <!-- 飞书 -->
              <el-tab-pane label="飞书机器人" name="feishu" class="pt-2">
                <div class="flex items-center justify-between mb-4">
                  <div class="flex items-center gap-2">
                    <div class="font-bold text-gray-800 dark:text-gray-100">启用飞书机器人</div>
                  </div>
                  <div class="flex items-center gap-2">
                    <el-button
                      size="small"
                      type="primary"
                      plain
                      :loading="testingFeishu"
                      :disabled="!feishuForm.enabled || !hasValidFeishuConfig"
                      @click="testFeishuNotification"
                    >
                      测试通知
                    </el-button>
                    <el-switch v-model="feishuForm.enabled" />
                  </div>
                </div>

                <div class="space-y-4">
                  <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
                    <div class="space-y-1">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">应用 ID（App ID）</label>
                      <el-input v-model="feishuForm.app_id" :disabled="!feishuForm.enabled" placeholder="cli_xxxx" />
                    </div>
                    <div class="space-y-1">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">应用密钥（App Secret）</label>
                      <el-input v-model="feishuForm.app_secret" :disabled="!feishuForm.enabled" type="password" show-password placeholder="••••••••" />
                    </div>
                  </div>
                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">会话 ID 列表（Chat IDs）</label>
                    <el-input v-model="feishuForm.chat_ids" :disabled="!feishuForm.enabled" placeholder="多个群组用英文逗号分隔" />
                    <div class="text-[10px] text-gray-400 mt-1">飞书群聊的 Chat ID (oc_xxxx)，可通过飞书开放平台 API 获取，支持逗号分隔多个群组。</div>
                  </div>
                  <div class="p-3 rounded-xl bg-blue-50/50 dark:bg-blue-500/5 text-xs text-blue-600 dark:text-blue-400/80 leading-relaxed border border-blue-100/50 dark:border-blue-500/10">
                    <strong>配置说明：</strong>
                    <ol class="list-decimal ml-4 mt-1 space-y-1">
                      <li>在<a href="https://open.feishu.cn" target="_blank" class="underline hover:text-blue-700">飞书开放平台</a>创建自建应用，启用「机器人」能力</li>
                      <li>在「事件与回调 → 事件配置」中选择「使用长连接接收事件」</li>
                      <li>添加 <code>im:message</code> 和 <code>im:message:send_as_bot</code> 权限</li>
                    </ol>
                  </div>
                </div>
              </el-tab-pane>

              <!-- QQ -->
              <el-tab-pane label="QQ 机器人" name="qq" class="pt-2">
                <div class="flex items-center justify-between mb-4">
                  <div class="flex items-center gap-2">
                    <div class="font-bold text-gray-800 dark:text-gray-100">启用 QQ 机器人</div>
                  </div>
                  <div class="flex items-center gap-2">
                    <el-button
                      size="small"
                      type="primary"
                      plain
                      :loading="testingQQ"
                      :disabled="!qqForm.enabled || !hasValidQQConfig"
                      @click="testQQNotification"
                    >
                      测试通知
                    </el-button>
                    <el-switch v-model="qqForm.enabled" />
                  </div>
                </div>

                <div class="space-y-4">
                  <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
                    <div class="space-y-1">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">应用 ID（App ID）</label>
                      <el-input v-model="qqForm.app_id" :disabled="!qqForm.enabled" placeholder="QQ Bot App ID" />
                    </div>
                    <div class="space-y-1">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">应用密钥（App Secret）</label>
                      <el-input v-model="qqForm.app_secret" :disabled="!qqForm.enabled" type="password" show-password placeholder="••••••••" />
                    </div>
                  </div>
                  <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
                    <div class="space-y-1">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">群聊 ID 列表（Group IDs）</label>
                      <el-input v-model="qqForm.group_ids" :disabled="!qqForm.enabled" placeholder="群聊 OpenID，多个使用逗号分隔" />
                    </div>
                    <div class="space-y-1">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">私聊用户 ID 列表（User IDs）</label>
                      <el-input v-model="qqForm.direct_ids" :disabled="!qqForm.enabled" placeholder="用户 OpenID，多个使用逗号分隔" />
                    </div>
                  </div>
                  <div class="p-3 rounded-xl bg-amber-50/50 dark:bg-amber-500/5 text-xs text-amber-700 dark:text-amber-400/80 leading-relaxed border border-amber-100/50 dark:border-amber-500/10">
                    <ol class="list-decimal ml-4 mt-1 space-y-1">
                      <li>QQbot申请地址：<a href="https://q.qq.com/qqbot/openclaw/index.html" target="_blank" class="underline hover:text-amber-800">官方控制台</a></li>
                      <li>向机器人发送消息后，去系统日志查看 OpenID，填入后 Bot 只对匹配的会话进行回复和推送。</li>
                    </ol>
                  </div>
                </div>
              </el-tab-pane>

                            <!-- Bark -->
              <el-tab-pane label="Bark" name="bark" class="pt-2">
                <div class="flex items-center justify-between mb-4">
                  <div class="flex items-center gap-2">
                    <div class="font-bold text-gray-800 dark:text-gray-100">启用 Bark 推送</div>
                  </div>
                  <div class="flex items-center gap-2">
                    <el-button
                      size="small"
                      type="primary"
                      plain
                      :loading="testingBark"
                      :disabled="!barkSettings.enabled || !hasValidBarkURLs"
                      @click="testBarkNotification"
                    >
                      测试通知
                    </el-button>
                    <el-switch v-model="barkSettings.enabled" />
                  </div>
                </div>

                <div class="space-y-4">
                  <div class="space-y-2">
                    <div class="flex items-center justify-between">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">目标 URL 列表</label>
                      <el-button size="small" type="primary" plain @click="addBarkUrl" :disabled="!barkSettings.enabled">
                         <el-icon><Add20Regular /></el-icon>
                         <span class="ml-1">添加 URL</span>
                      </el-button>
                    </div>
                    
                    <div v-if="barkSettings.urls && barkSettings.urls.length === 0" class="text-xs text-gray-400 py-2 border border-dashed border-gray-200 dark:border-white/10 rounded-lg text-center bg-gray-50/30 dark:bg-white/5">
                      尚未配置任何 Bark URL，点击右侧添加按钮。
                    </div>

                    <div v-for="(url, index) in barkSettings.urls" :key="index" class="flex items-center gap-2">
                       <el-input v-model="barkSettings.urls[index]" :disabled="!barkSettings.enabled" placeholder="https://api.day.app/YOUR_KEY/" class="flex-1" />
                       <el-button type="danger" plain @click="removeBarkUrl(index)" :disabled="!barkSettings.enabled">
                          <el-icon><Delete20Regular /></el-icon>
                       </el-button>
                    </div>
                  </div>

                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">分组（Group）</label>
                    <el-input v-model="barkSettings.group" :disabled="!barkSettings.enabled" placeholder="例如 vohive" />
                    <div class="text-[10px] text-gray-400 mt-1">iOS 设备上的通知分组。</div>
                  </div>

                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">通知级别（Level）</label>
                    <el-select v-model="barkSettings.level" :disabled="!barkSettings.enabled" placeholder="选择通知级别" class="w-full">
                      <el-option label="时效性 (timeSensitive)" value="timeSensitive" />
                      <el-option label="积极 (active)" value="active" />
                      <el-option label="被动 (passive)" value="passive" />
                    </el-select>
                    <div class="text-[10px] text-gray-400 mt-1">iOS 的专注模式/打扰规则会根据此级别决定是否亮屏。</div>
                  </div>

                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">图标（Icon）</label>
                    <el-input v-model="barkSettings.icon" :disabled="!barkSettings.enabled" placeholder="图标 URL，可选" />
                  </div>
                </div>
              </el-tab-pane>

              <!-- Email -->
              <el-tab-pane label="邮件" name="email" class="pt-2">
                <div class="flex items-center justify-between mb-4">
                  <div class="flex items-center gap-2">
                    <div class="font-bold text-gray-800 dark:text-gray-100">启用邮件推送</div>
                  </div>
                  <div class="flex items-center gap-2">
                    <el-button
                      size="small"
                      type="primary"
                      plain
                      :loading="testingEmail"
                      :disabled="!emailForm.enabled || !hasValidEmailConfig"
                      @click="testEmailNotification"
                    >
                      测试通知
                    </el-button>
                    <el-switch v-model="emailForm.enabled" />
                  </div>
                </div>

                <div class="space-y-4">
                  <div class="grid grid-cols-1 sm:grid-cols-10 gap-4">
                    <div class="space-y-1 sm:col-span-5">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">SMTP 主机</label>
                      <el-input v-model="emailForm.smtp_host" :disabled="!emailForm.enabled" placeholder="smtp.example.com" />
                    </div>
                    <div class="space-y-1 sm:col-span-3">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">SMTP 端口</label>
                      <el-input v-model="emailForm.smtp_port" :disabled="!emailForm.enabled" type="number" inputmode="numeric" placeholder="465 / 587" />
                    </div>
                    <div class="space-y-1 sm:col-span-2">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider block">使用 SSL/TLS </label>
                      <div class="h-10 flex items-center">
                        <el-switch v-model="emailForm.use_ssl" :disabled="!emailForm.enabled" />
                      </div>
                    </div>
                  </div>
                  <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
                    <div class="space-y-1">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">用户名</label>
                      <el-input v-model="emailForm.username" :disabled="!emailForm.enabled" placeholder="邮箱账号" />
                    </div>
                    <div class="space-y-1">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">密码</label>
                      <el-input v-model="emailForm.password" :disabled="!emailForm.enabled" type="password" show-password placeholder="邮箱密码或授权码" />
                    </div>
                  </div>
                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">发件人地址</label>
                    <el-input v-model="emailForm.from_address" :disabled="!emailForm.enabled" placeholder="例如 noreply@example.com" />
                  </div>
                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">收件人地址</label>
                    <el-input v-model="emailForm.to_addresses" :disabled="!emailForm.enabled" placeholder="多个收件人请用英文逗号分隔" />
                  </div>
                </div>
              </el-tab-pane>

              <!-- Pushplus -->
              <el-tab-pane label="Pushplus" name="pushplus" class="pt-2">
                <div class="flex items-center justify-between mb-4">
                  <div class="flex items-center gap-2">
                    <div class="font-bold text-gray-800 dark:text-gray-100">启用 Pushplus 推送</div>
                  </div>
                  <div class="flex items-center gap-2">
                    <el-button
                      size="small"
                      type="primary"
                      plain
                      :loading="testingPushplus"
                      :disabled="!pushplusForm.enabled || !hasValidPushplusConfig"
                      @click="testPushplusNotification"
                    >
                      测试通知
                    </el-button>
                    <el-switch v-model="pushplusForm.enabled" />
                  </div>
                </div>

                <div class="space-y-4">
                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">令牌（Token）</label>
                    <el-input v-model="pushplusForm.token" :disabled="!pushplusForm.enabled" placeholder="Pushplus 用户的 Token" />
                  </div>
                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">群组编码（Topic）</label>
                    <el-input v-model="pushplusForm.topic" :disabled="!pushplusForm.enabled" placeholder="群组编码，不填则发给个人" />
                  </div>
                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">渠道（Channel）</label>
                    <el-select v-model="pushplusForm.channel" :disabled="!pushplusForm.enabled" placeholder="选择渠道" class="w-full">
                      <el-option label="微信 (wechat)" value="wechat" />
                      <el-option label="Webhook (webhook)" value="webhook" />
                      <el-option label="企业微信 (cp)" value="cp" />
                      <el-option label="邮件 (mail)" value="mail" />
                    </el-select>
                  </div>
                </div>
              </el-tab-pane>

              <!-- Webhook -->
              <el-tab-pane label="Webhook" name="webhook" class="pt-2">
                <div class="flex items-center justify-between mb-4">
                  <div class="flex items-center gap-2">
                    <div class="font-bold text-gray-800 dark:text-gray-100">启用 Webhook 推送</div>
                  </div>
                  <div class="flex items-center gap-2">
                    <el-button
                      size="small"
                      type="primary"
                      plain
                      :loading="testingWebhook"
                      :disabled="!webhookSettings.enabled || !hasValidWebhookURLs"
                      @click="testWebhookNotification"
                    >
                      测试通知
                    </el-button>
                    <el-switch v-model="webhookSettings.enabled" />
                  </div>
                </div>

                <div class="space-y-4">
                  <div class="space-y-2">
                    <div class="flex items-center justify-between">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">目标 URL 列表</label>
                      <el-button size="small" type="primary" plain @click="addWebhookUrl" :disabled="!webhookSettings.enabled">
                         <el-icon><Add20Regular /></el-icon>
                         <span class="ml-1">添加 URL</span>
                      </el-button>
                    </div>
                    
                    <div v-if="webhookSettings.urls && webhookSettings.urls.length === 0" class="text-xs text-gray-400 py-2 border border-dashed border-gray-200 dark:border-white/10 rounded-lg text-center bg-gray-50/30 dark:bg-white/5">
                      尚未配置任何 Webhook URL，点击右侧添加按钮。
                    </div>

                    <div v-for="(url, index) in webhookSettings.urls" :key="index" class="flex items-center gap-2">
                       <!-- 注意：el-input v-model="webhookSettings.urls[index]" 处理基本类型数组在 Vue3 中可能会有失去焦点问题。
                            但在这里作为简单的响应式数组依然可用，或者用更复杂的方式包裹。 -->
                       <el-input v-model="webhookSettings.urls[index]" :disabled="!webhookSettings.enabled" placeholder="https://..." class="flex-1" />
                       <el-button type="danger" plain @click="removeWebhookUrl(index)" :disabled="!webhookSettings.enabled">
                          <el-icon><Delete20Regular /></el-icon>
                       </el-button>
                    </div>
                  </div>

                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">数字签名密钥（Secret）</label>
                    <el-input v-model="webhookSettings.secret" :disabled="!webhookSettings.enabled" placeholder="用于 HMAC-SHA256 签名，选填" />
                    <div class="text-[10px] text-gray-400 mt-1">若配置，将通过请求头 X-Vohive-Signature 提供 payload 验证。</div>
                  </div>

                  <div class="space-y-2">
                    <div class="flex items-center justify-between">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">自定义请求头（Headers）</label>
                      <el-button size="small" type="primary" plain @click="addWebhookHeader" :disabled="!webhookSettings.enabled">
                        <el-icon><Add20Regular /></el-icon>
                        <span class="ml-1">添加 Header</span>
                      </el-button>
                    </div>

                    <div v-if="webhookHeaderRows.length === 0" class="text-xs text-gray-400 py-2 border border-dashed border-gray-200 dark:border-white/10 rounded-lg text-center bg-gray-50/30 dark:bg-white/5">
                      尚未配置自定义请求头，例如 Authorization、X-Api-Key 等。
                    </div>

                    <div v-for="(row, index) in webhookHeaderRows" :key="row.id" class="flex items-center gap-2">
                      <el-select
                        v-model="row.key"
                        :disabled="!webhookSettings.enabled"
                        filterable
                        allow-create
                        default-first-option
                        placeholder="选择或输入 Header 名"
                        class="flex-1"
                      >
                        <el-option v-for="name in COMMON_WEBHOOK_HEADERS" :key="name" :label="name" :value="name" />
                      </el-select>
                      <el-input v-model="row.value" :disabled="!webhookSettings.enabled" placeholder="值，如 Bearer xxx" class="flex-1" />
                      <el-button type="danger" plain @click="removeWebhookHeader(index)" :disabled="!webhookSettings.enabled">
                        <el-icon><Delete20Regular /></el-icon>
                      </el-button>
                    </div>
                    <div class="text-[10px] text-gray-400 mt-1">
                      Content-Type 与 X-Vohive-Signature 为系统保留头，自定义同名头会被忽略。
                    </div>
                  </div>

                  <div class="space-y-1">
                    <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">文本模板（Text Template）</label>
                    <el-input
                      v-model="webhookSettings.text_template"
                      :disabled="!webhookSettings.enabled"
                      type="textarea"
                      :rows="2"
                      placeholder="{{device_label}} {{text}}"
                    />
                    <div class="text-[10px] text-gray-400 mt-1">
                      支持占位符：<code v-pre>{{text}}</code>、<code v-pre>{{event}}</code>、<code v-pre>{{timestamp}}</code>、<code v-pre>{{device_id}}</code>、<code v-pre>{{device_name}}</code>、<code v-pre>{{device_label}}</code>。留空则直接发送原始 text。
                    </div>
                  </div>
                  
                  <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
                    <div class="space-y-1">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">请求超时（毫秒）</label>
                      <el-input-number v-model="webhookSettings.timeout_ms" :min="1000" :max="60000" :disabled="!webhookSettings.enabled" class="w-full !w-full" controls-position="right" />
                    </div>
                    <div class="space-y-1">
                      <label class="text-xs font-bold text-gray-500 uppercase tracking-wider">最大重试次数</label>
                      <el-input-number v-model="webhookSettings.retry_max" :min="0" :max="10" :disabled="!webhookSettings.enabled" class="w-full !w-full" controls-position="right" />
                    </div>
                  </div>
                </div>
              </el-tab-pane>
            </el-tabs>
         </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
:deep(.notify-card .el-input-number) {
  width: 100%;
}
:deep(.settings-notify-tabs) {
  border: none;
  background: transparent;
}
:deep(.settings-notify-tabs .el-tabs__header) {
  margin-bottom: 24px;
  background-color: var(--el-fill-color-light);
  border-radius: 12px;
  border-bottom: none;
  display: inline-flex;
  padding: 4px;
}
:deep(.settings-notify-tabs .el-tabs__nav-wrap::after) {
  display: none;
}
:deep(.settings-notify-tabs .el-tabs__active-bar) {
  display: none;
}
:deep(.settings-notify-tabs .el-tabs__item) {
  height: 38px;
  line-height: 38px;
  padding: 0 20px !important;
  border-radius: 8px;
  margin-right: 4px;
  color: var(--el-text-color-regular);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  font-weight: 500;
}
:deep(.settings-notify-tabs .el-tabs__item:last-child) {
  margin-right: 0;
}
:deep(.settings-notify-tabs .el-tabs__item:hover) {
  color: var(--el-color-primary);
}
:deep(.settings-notify-tabs .el-tabs__item.is-active) {
  background-color: var(--el-bg-color);
  color: var(--el-color-primary);
  font-weight: 600;
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.05), 0 2px 8px rgba(0, 0, 0, 0.03);
}
</style>
