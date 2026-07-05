<script setup lang="ts">
import type { Component } from 'vue'
import { SignOut24Regular } from '@vicons/fluent'

defineProps<{
  collapsed: boolean
  activePath: string
  menuItems: Array<{
    index: string
    label: string
    icon: Component
  }>
  badges?: Record<string, number>
}>()

const emit = defineEmits<{
  logout: []
}>()
</script>

<template>
  <div class="h-full relative sidebar-shell">
    <div class="h-14 px-4 flex items-center" :class="collapsed ? 'justify-center px-0' : ''">
      <div class="sidebar-brand-icon">V</div>
      <div v-if="!collapsed" class="ml-3">
        <div class="sidebar-brand-title">VoHive</div>
      </div>
    </div>

    <el-menu
      :collapse="collapsed"
      :collapse-transition="false"
      :default-active="activePath"
      class="sidebar-menu !border-0 !border-r-0 !bg-transparent mt-2"
      router
    >
      <el-menu-item v-for="item in menuItems" :key="item.index" :index="item.index">
        <span class="sidebar-menu-icon-wrap">
          <el-icon><component :is="item.icon" /></el-icon>
          <span v-if="badges?.[item.index]" class="sidebar-menu-badge-dot" />
        </span>
        <template #title>
          <span class="sidebar-menu-label">{{ item.label }}</span>
          <span v-if="badges?.[item.index]" class="sidebar-menu-badge">
            {{ badges[item.index] > 99 ? '99+' : badges[item.index] }}
          </span>
        </template>
      </el-menu-item>
    </el-menu>

    <div v-if="!collapsed" class="absolute bottom-4 w-full px-3">
      <el-button class="!w-full" type="danger" plain @click="emit('logout')">
        <el-icon><SignOut24Regular /></el-icon>
        <span>退出登录</span>
      </el-button>
    </div>
  </div>
</template>

<style scoped>
.sidebar-shell {
  font-family: "Inter", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  -webkit-font-smoothing: antialiased;
  text-rendering: optimizeLegibility;
  --sidebar-menu-text: #475569;
  --sidebar-menu-hover-bg: rgba(6, 182, 212, 0.08);
  --sidebar-menu-active-bg: linear-gradient(135deg, rgba(6, 182, 212, 0.14), rgba(20, 184, 166, 0.1));
  --sidebar-menu-active-color: #0f766e;
  --sidebar-menu-active-ring: rgba(6, 182, 212, 0.16);
}

.sidebar-brand-title {
  font-family: "Space Grotesk", "Inter", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  font-size: 1.62rem;
  font-weight: 600;
  letter-spacing: 0;
  line-height: 1;
  display: flex;
  align-items: center;
  min-height: 1.75rem;
  background: linear-gradient(135deg, #06b6d4, #8b5cf6);
  background-clip: text;
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  filter: drop-shadow(0 2px 8px rgba(6, 182, 212, 0.18));
  white-space: nowrap;
  padding-right: 4px;
}

.sidebar-brand-icon {
  width: 1.62rem;
  height: 1.62rem;
  border-radius: 0.5rem;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  background: linear-gradient(135deg, #06b6d4, #14b8a6);
  color: #fff;
  font-family: "Inter", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  font-size: 0.84rem;
  font-weight: 700;
  box-shadow: 0 6px 14px rgba(6, 182, 212, 0.18);
}

.sidebar-menu-label {
  font-family: "Inter", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  font-weight: 500;
  letter-spacing: 0;
}

.sidebar-menu-icon-wrap {
  position: relative;
  display: inline-grid;
  place-items: center;
}

.sidebar-menu-badge-dot {
  position: absolute;
  top: -4px;
  right: -5px;
  width: 7px;
  height: 7px;
  border-radius: 999px;
  background: #ef4444;
  box-shadow: 0 0 0 2px var(--el-bg-color);
}

.sidebar-menu-badge {
  margin-left: auto;
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  border-radius: 999px;
  background: #ef4444;
  color: #fff;
  font-size: 10px;
  font-weight: 700;
  line-height: 18px;
  text-align: center;
}

:global(html.dark) .sidebar-shell {
  --sidebar-menu-text: rgba(255, 255, 255, 0.72);
  --sidebar-menu-hover-bg: rgba(45, 212, 191, 0.1);
  --sidebar-menu-active-bg: linear-gradient(135deg, rgba(34, 211, 238, 0.18), rgba(45, 212, 191, 0.12));
  --sidebar-menu-active-color: #99f6e4;
  --sidebar-menu-active-ring: rgba(103, 232, 249, 0.18);
}

:deep(.sidebar-menu) {
  border-right: 0 !important;
  --el-menu-hover-bg-color: var(--sidebar-menu-hover-bg);
  --el-menu-active-color: var(--sidebar-menu-active-color);
  --el-menu-text-color: var(--sidebar-menu-text);
}

:deep(.sidebar-menu .el-menu-item) {
  height: 40px;
  min-height: 40px;
  line-height: 40px;
  margin: 2px 8px;
  border-radius: 10px;
  padding-left: 13px !important;
  padding-right: 13px !important;
  font-size: 0.94rem;
  font-weight: 400;
  letter-spacing: 0;
  color: var(--sidebar-menu-text);
  transition: background-color 160ms ease, color 160ms ease, box-shadow 160ms ease;
}

:deep(.sidebar-menu .el-menu-item .el-icon) {
  margin-right: 8px !important;
  font-size: 1.18rem;
}

:deep(.sidebar-menu .el-menu-item .sidebar-menu-icon-wrap > .el-icon) {
  margin-right: 8px !important;
}

:deep(.sidebar-menu .el-menu-item .el-icon svg) {
  width: 1.18rem;
  height: 1.18rem;
}

:deep(.sidebar-menu .el-menu-item:hover) {
  background: var(--sidebar-menu-hover-bg);
}

:deep(.sidebar-menu .el-menu-item.is-active) {
  background: var(--sidebar-menu-active-bg);
  color: var(--sidebar-menu-active-color);
  box-shadow: inset 0 0 0 1px var(--sidebar-menu-active-ring);
}

:deep(.sidebar-menu .el-menu-item.is-active .el-icon),
:deep(.sidebar-menu .el-menu-item.is-active .sidebar-menu-label) {
  color: inherit;
}

:deep(.sidebar-menu .el-menu-item::after) {
  display: none !important;
}

:deep(.sidebar-menu.el-menu--collapse) {
  width: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
}

:deep(.sidebar-menu.el-menu--collapse .el-menu-item) {
  width: 36px;
  height: 36px;
  min-height: 36px;
  line-height: 36px;
  margin: 3px auto;
  border-radius: 10px;
  display: grid;
  place-items: center;
  padding: 0 !important;
}

:deep(.sidebar-menu.el-menu--collapse .el-menu-item .el-icon) {
  width: 1.18rem;
  height: 1.18rem;
  margin: 0 !important;
  font-size: 1.18rem;
  line-height: 1;
  display: grid;
  place-items: center;
}

:deep(.sidebar-menu.el-menu--collapse .el-menu-item .sidebar-menu-icon-wrap > .el-icon) {
  margin: 0 !important;
}

:deep(.sidebar-menu.el-menu--collapse .el-menu-item .el-icon svg) {
  width: 1.18rem;
  height: 1.18rem;
  display: block;
}

:deep(.sidebar-menu.el-menu--collapse .el-menu-item .el-menu-tooltip__trigger) {
  position: static;
  inset: auto;
  width: 100%;
  height: 100%;
  padding: 0 !important;
  display: grid;
  place-items: center;
}

:deep(.sidebar-menu.el-menu--collapse > .el-menu-item [class^=el-icon]) {
  width: 1.18rem !important;
}

:deep(.sidebar-menu.el-menu--collapse .el-tooltip) {
  width: 36px;
  display: grid;
  place-items: center;
}

:deep(.sidebar-menu.el-menu--collapse .el-tooltip__trigger) {
  width: 36px;
  height: 36px;
  display: grid;
  place-items: center;
}
</style>
