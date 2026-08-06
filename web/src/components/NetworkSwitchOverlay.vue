<script setup lang="ts">
import { computed } from 'vue'
import {
  ArrowSync24Regular,
  CellularData124Regular,
  CheckmarkCircle24Regular
} from '@vicons/fluent'

const props = defineProps<{
  visible: boolean
  targetName: string
  currentStep: number
  countdown: number
  stepText: string
}>()

const percentage = computed(() => {
  if (props.countdown <= 0) return 100
  const max = 12
  const elapsed = max - props.countdown
  return Math.min(100, Math.max(10, Math.round((elapsed / max) * 100)))
})
</script>

<template>
  <div v-if="visible" class="switch-banner-card">
    <div class="banner-top">
      <div class="flex items-center gap-2.5">
        <span class="icon-pulse">
          <el-icon v-if="countdown > 0" class="is-loading text-teal-600 dark:text-teal-400" size="20"><ArrowSync24Regular /></el-icon>
          <el-icon v-else class="text-emerald-500" size="20"><CheckmarkCircle24Regular /></el-icon>
        </span>
        <div>
          <strong class="text-sm font-bold text-gray-900 dark:text-white">
            {{ countdown > 0 ? '蜂窝网络切流与重连中...' : '蜂窝网络切流已完成' }}
          </strong>
          <div class="text-xs text-gray-600 dark:text-gray-300 mt-0.5 flex items-center gap-1">
            <el-icon class="text-teal-500"><CellularData124Regular /></el-icon>
            目标：<span class="font-bold text-teal-700 dark:text-teal-300">{{ targetName }}</span>
          </div>
        </div>
      </div>
      <span class="countdown-tag" :class="{ finished: countdown <= 0 }">
        {{ countdown > 0 ? `倒计时 ${countdown}s` : '完成' }}
      </span>
    </div>

    <!-- 步骤进度指示器 -->
    <div class="step-indicator-row">
      <div class="step-badge" :class="{ active: currentStep === 1, done: currentStep > 1 }">
        <span class="step-num">1</span>
        <span>下发指令</span>
      </div>
      <div class="step-divider" :class="{ active: currentStep >= 2 }"></div>
      <div class="step-badge" :class="{ active: currentStep === 2, done: currentStep > 2 }">
        <span class="step-num">2</span>
        <span>基站注册</span>
      </div>
      <div class="step-divider" :class="{ active: currentStep >= 3 }"></div>
      <div class="step-badge" :class="{ active: currentStep === 3, done: countdown <= 0 }">
        <span class="step-num">3</span>
        <span>恢复代理通信</span>
      </div>
    </div>

    <el-progress
      :percentage="percentage"
      :status="countdown <= 0 ? 'success' : undefined"
      :striped="countdown > 0"
      :striped-flow="countdown > 0"
      :stroke-width="8"
      :show-text="false"
      class="mt-2.5"
    />

    <div class="banner-bottom flex items-center justify-between text-xs text-gray-500 dark:text-gray-400 mt-2">
      <span class="font-medium text-teal-700 dark:text-teal-300">{{ stepText }}</span>
      <span class="text-[11px] opacity-80">ℹ️ 切换期间代理健康告警已临时挂起</span>
    </div>
  </div>
</template>

<style scoped>
.switch-banner-card {
  padding: 14px 16px;
  border-radius: 14px;
  background: linear-gradient(135deg, rgba(20, 184, 166, 0.08), rgba(240, 253, 250, 0.65));
  border: 1px solid rgba(13, 148, 136, 0.25);
  box-shadow: 0 4px 14px rgba(15, 23, 42, 0.04);
  margin-bottom: 16px;
}
.banner-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.countdown-tag {
  padding: 3px 10px;
  border-radius: 12px;
  background: rgba(13, 148, 136, 0.15);
  color: #0f766e;
  font-family: 'Fira Code', monospace;
  font-size: 11px;
  font-weight: 700;
}
.countdown-tag.finished {
  background: rgba(16, 185, 129, 0.2);
  color: #047857;
}
.step-indicator-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 10px;
}
.step-badge {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 11px;
  color: #64748b;
  font-weight: 600;
}
.step-badge .step-num {
  display: inline-grid;
  place-items: center;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: rgba(148, 163, 184, 0.2);
  color: #64748b;
  font-size: 10px;
}
.step-badge.active {
  color: #0f766e;
}
.step-badge.active .step-num {
  background: #0d9488;
  color: white;
}
.step-badge.done {
  color: #059669;
}
.step-badge.done .step-num {
  background: #10b981;
  color: white;
}
.step-divider {
  flex: 1;
  height: 2px;
  background: rgba(148, 163, 184, 0.25);
  border-radius: 1px;
}
.step-divider.active {
  background: #0d9488;
}

:global(.dark) .switch-banner-card {
  background: linear-gradient(135deg, rgba(13, 148, 136, 0.16), rgba(15, 23, 42, 0.65));
  border-color: rgba(20, 184, 166, 0.3);
}
:global(.dark) .countdown-tag {
  background: rgba(45, 212, 191, 0.2);
  color: #5eead4;
}
</style>
