import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import type { AppError } from '../types/domain'
import type { DeviceMgmtListItem } from '../types/api'
import type { SMSMessageDTO, SmsThreadVM } from '../types/view-model'
import { smsService, type SmsDeleteThreadPayload, type SmsSendPayload, type SmsThreadQueryParams } from '../services/sms'

export const useSMSStore = defineStore('sms', () => {
  const devices = ref<DeviceMgmtListItem[]>([])
  const threads = ref<SmsThreadVM[]>([])
  const threadMessages = ref<SMSMessageDTO[]>([])

  const loading = ref(false)
  const lastOkAt = ref<number | null>(null)
  const error = ref<AppError | null>(null)

  function getLastSeen(threadKey: string) {
    try {
      const v = localStorage.getItem(`sms_thread_last_seen:all:${threadKey}`)
      return v ? Number(v) || 0 : 0
    } catch {
      return 0
    }
  }

  const unreadCount = computed(() => {
    return threads.value.reduce((sum, thread) => {
      if (thread.lastType !== 1) return sum
      if (thread.lastTs <= getLastSeen(thread.key)) return sum
      return sum + Math.max(1, Number(thread.unreadCount || 0))
    }, 0)
  })

  async function fetchDevices() {
    const result = await smsService.listDevices()
    if (result.ok) devices.value = result.data
    return result
  }

  async function fetchThreads(deviceId?: string) {
    const result = await smsService.listContacts(deviceId)
    if (result.ok) {
      threads.value = result.data
      lastOkAt.value = Date.now()
      error.value = null
    } else {
      error.value = result.error
    }
    return result
  }

  async function fetchThread(params: SmsThreadQueryParams) {
    const result = await smsService.getThread(params)
    if (result.ok) threadMessages.value = result.data
    return result
  }

  async function send(payload: SmsSendPayload) {
    return smsService.send(payload)
  }

  async function deleteMessage(id: number) {
    return smsService.deleteMessage(id)
  }

  async function deleteThread(payload: SmsDeleteThreadPayload) {
    return smsService.deleteThread(payload)
  }

  return {
    devices,
    threads,
    threadMessages,
    loading,
    lastOkAt,
    error,
    unreadCount,
    fetchDevices,
    fetchThreads,
    fetchThread,
    send,
    deleteMessage,
    deleteThread
  }
})
