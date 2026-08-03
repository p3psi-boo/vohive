<template>
  <div class="toast" :class="{ show: toastState.visible, error: toastState.error }" role="status" aria-live="polite">
    {{ toastState.message }}
  </div>
</template>

<script setup lang="ts">
import { toastState } from '../toast'
</script>
