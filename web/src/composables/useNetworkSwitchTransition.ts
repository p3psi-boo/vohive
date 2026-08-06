import { onBeforeUnmount, ref } from 'vue'

export function useNetworkSwitchTransition() {
  const isSwitching = ref(false)
  const targetName = ref('')
  const currentStep = ref<1 | 2 | 3>(1)
  const countdown = ref(12)
  const stepText = ref('')
  let countdownTimer: number | null = null
  let pollTimer: number | null = null

  function clearTimers() {
    if (countdownTimer !== null) {
      window.clearInterval(countdownTimer)
      countdownTimer = null
    }
    if (pollTimer !== null) {
      window.clearInterval(pollTimer)
      pollTimer = null
    }
  }

  function startSwitch(name: string, opts?: { durationSeconds?: number; onPoll?: () => Promise<boolean> }) {
    clearTimers()
    const duration = opts?.durationSeconds || 12
    isSwitching.value = true
    targetName.value = name
    currentStep.value = 1
    stepText.value = '正在向设备下发网络切换指令...'
    countdown.value = duration

    // 1.2 秒后进入 Step 2（基站与网络注册）
    window.setTimeout(() => {
      if (!isSwitching.value) return
      currentStep.value = 2
      stepText.value = '正在重新发起蜂窝基站注册与 IP 获取...'
    }, 1200)

    countdownTimer = window.setInterval(() => {
      if (countdown.value > 1) {
        countdown.value--
        if (countdown.value <= 4 && currentStep.value === 2) {
          currentStep.value = 3
          stepText.value = '正在验证代理与通信链路连通性...'
        }
      } else {
        finishSwitch('网络重连完成，数据已恢复')
      }
    }, 1000)

    if (opts?.onPoll) {
      pollTimer = window.setInterval(async () => {
        if (!isSwitching.value) return
        try {
          const online = await opts.onPoll!()
          if (online && countdown.value <= duration - 3) {
            currentStep.value = 3
            stepText.value = '网络已成功重连！'
            window.setTimeout(() => {
              finishSwitch('网络已成功重连，代理服务恢复正常')
            }, 800)
          }
        } catch {
          // 切流期间忽略轮询错误
        }
      }, 2000)
    }
  }

  function finishSwitch(msg?: string) {
    clearTimers()
    currentStep.value = 3
    countdown.value = 0
    stepText.value = msg || '切换完成'
    window.setTimeout(() => {
      isSwitching.value = false
    }, 1500)
  }

  onBeforeUnmount(clearTimers)

  return {
    isSwitching,
    targetName,
    currentStep,
    countdown,
    stepText,
    startSwitch,
    finishSwitch
  }
}
