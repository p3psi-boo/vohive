<template>
  <section class="login-stage">
    <main class="login-card">
      <div class="mark">VH</div>
      <h1>VoHive Agent</h1>
      <p class="lead">输入密码以管理设备连接</p>
      <form class="stack" @submit.prevent="submit">
        <label class="field">
          密码
          <input ref="passwordInput" v-model="password" type="password"
                 autocomplete="current-password" required>
        </label>
        <button class="primary" type="submit" :disabled="busy">{{ busy ? '登录中' : '登录' }}</button>
        <p class="form-error" role="alert">{{ error }}</p>
      </form>
    </main>
  </section>
  <AppToast />
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, type Session } from '../api'
import { applySession } from '../session'
import AppToast from '../components/AppToast.vue'

// 账号固定为 admin，页面只需输入密码
const USERNAME = 'admin'

const router = useRouter()
const password = ref('')
const error = ref('')
const busy = ref(false)
const passwordInput = ref<HTMLInputElement>()

async function submit() {
  error.value = ''
  busy.value = true
  try {
    const s = await api<Session>('/api/auth/login', {
      method: 'POST',
      body: { username: USERNAME, password: password.value }
    })
    applySession(s)
    router.replace({ name: 'home' })
  } catch (e) {
    error.value = e instanceof Error ? e.message : '登录失败'
  } finally {
    busy.value = false
  }
}

onMounted(() => passwordInput.value?.focus())
</script>
