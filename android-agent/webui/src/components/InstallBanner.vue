<template>
  <section v-if="visible" class="install-banner">
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <path d="M12 3v12M7 10l5 5 5-5" />
      <path d="M5 21h14" />
    </svg>
    <p>{{ copy }}</p>
    <button v-if="installState.deferred" class="install-action" @click="promptInstall">安装</button>
    <button class="install-close" aria-label="关闭" @click="dismissInstallHint">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round">
        <path d="M5 5l14 14M19 5L5 19" />
      </svg>
    </button>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import {
  dismissInstallHint, installState, isIos, isStandalone, promptInstall
} from '../install'

const visible = computed(() => !isStandalone() && !installState.dismissed)

const copy = computed(() => {
  if (installState.deferred) return '添加到主屏幕，便于快速访问'
  if (isIos()) return '通过 Safari 分享菜单「添加到主屏幕」'
  return '在浏览器菜单中选择「添加到主屏幕」'
})
</script>
