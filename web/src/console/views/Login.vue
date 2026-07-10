<script setup>
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { api, markSession } from '@/api'

const router = useRouter()
const route = useRoute()

const form = ref({ username: '', password: '' })
const loading = ref(false)
const needRegister = ref(false) // 系统尚无用户 -> 显示注册 (首位即管理员)
const turnstileSiteKey = ref('')
const turnstileToken = ref('')
const turnstileEl = ref(null)
const turnstileReady = ref(false)
let turnstileWidgetId = null

function loadTurnstileScript() {
  if (window.turnstile) return Promise.resolve()
  if (window.__focusbiTurnstileLoading) return window.__focusbiTurnstileLoading

  window.__focusbiTurnstileLoading = new Promise((resolve, reject) => {
    const existing = document.querySelector('script[data-focusbi-turnstile]')
    if (existing) {
      existing.addEventListener('load', () => resolve(), { once: true })
      existing.addEventListener('error', reject, { once: true })
      return
    }
    const script = document.createElement('script')
    script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit'
    script.defer = true
    script.dataset.focusbiTurnstile = '1'
    script.onload = () => resolve()
    script.onerror = () => reject(new Error('人机验证加载失败'))
    document.head.appendChild(script)
  })
  return window.__focusbiTurnstileLoading
}

async function renderTurnstile() {
  if (!turnstileSiteKey.value || !turnstileEl.value || turnstileWidgetId !== null) return
  try {
    await loadTurnstileScript()
    turnstileWidgetId = window.turnstile.render(turnstileEl.value, {
      sitekey: turnstileSiteKey.value,
      theme: 'light',
      size: 'flexible',
      callback: token => {
        turnstileToken.value = token
        turnstileReady.value = true
      },
      'expired-callback': () => {
        turnstileToken.value = ''
        turnstileReady.value = false
      },
      'error-callback': () => {
        turnstileToken.value = ''
        turnstileReady.value = false
      }
    })
  } catch (e) {
    ElMessage.error(e.message || '人机验证加载失败')
  }
}

function resetTurnstile() {
  turnstileToken.value = ''
  turnstileReady.value = false
  if (window.turnstile && turnstileWidgetId !== null) {
    window.turnstile.reset(turnstileWidgetId)
  }
}

onMounted(async () => {
  try {
    const d = await api.bootstrap()
    needRegister.value = !!d.need_register
    turnstileSiteKey.value = d.turnstile?.enabled ? d.turnstile.site_key : ''
    if (turnstileSiteKey.value) {
      await nextTick()
      renderTurnstile()
    }
  } catch { /* 忽略, 默认登录 */ }
})

onBeforeUnmount(() => {
  if (window.turnstile && turnstileWidgetId !== null) {
    window.turnstile.remove(turnstileWidgetId)
    turnstileWidgetId = null
  }
})

async function submit() {
  const { username, password } = form.value
  if (!username.trim() || !password) {
    ElMessage.warning('请输入用户名和密码')
    return
  }
  if (turnstileSiteKey.value && !turnstileToken.value) {
    ElMessage.warning('请先完成人机验证')
    return
  }
  loading.value = true
  try {
    needRegister.value
      ? await api.register(username.trim(), password, turnstileToken.value)
      : await api.login(username.trim(), password, turnstileToken.value)
    markSession()
    ElMessage.success(needRegister.value ? '注册成功, 已登录' : '登录成功')
    router.push(route.query.redirect || '/')
  } catch (e) {
    ElMessage.error(e.message)
    resetTurnstile()
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-page">
    <div class="card">
      <div class="header">
        <div class="brand-mark">F</div>
        <div>
          <div class="brand">FocusBI</div>
          <div class="title">{{ needRegister ? '初始化管理员账号' : '登录控制台' }}</div>
        </div>
      </div>
      <p v-if="needRegister" class="hint">系统首次启动, 注册的第一个账号将成为超级管理员。</p>
      <div class="form">
        <el-input v-model="form.username" placeholder="用户名" size="large" class="field" @keyup.enter="submit" />
        <el-input v-model="form.password" type="password" placeholder="密码" size="large" show-password
          class="field" @keyup.enter="submit" />
        <div v-if="turnstileSiteKey" class="turnstile-wrap">
          <div ref="turnstileEl" class="turnstile-box"></div>
        </div>
        <el-button type="primary" size="large" :loading="loading" :disabled="!!turnstileSiteKey && !turnstileReady" class="submit" @click="submit">
          {{ needRegister ? '注册并登录' : '登录' }}
        </el-button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.login-page { min-height: 100vh; display: flex; align-items: center; justify-content: center; padding: 24px; box-sizing: border-box; background: var(--el-bg-color-page); }
.card { width: 380px; max-width: 100%; background: var(--el-bg-color); border: 1px solid var(--el-border-color-light); border-radius: 8px; padding: 32px; box-sizing: border-box; box-shadow: var(--el-box-shadow-light); }
.header { display: flex; align-items: center; gap: 12px; margin-bottom: 24px; }
.brand-mark { width: 40px; height: 40px; flex: none; display: flex; align-items: center; justify-content: center; border-radius: 8px; background: #1f2430; color: #fff; font-size: 18px; font-weight: 700; }
.brand { font-size: 22px; font-weight: 700; color: var(--el-text-color-primary); line-height: 1.2; }
.title { margin-top: 4px; font-size: 13px; color: var(--el-text-color-secondary); line-height: 1.3; }
.hint { padding: 10px 12px; border-radius: 6px; background: var(--el-color-warning-light-9); color: var(--el-color-warning); font-size: 12px; line-height: 1.5; margin: 0 0 16px; }
.form { display: flex; flex-direction: column; gap: 14px; }
.field { width: 100%; }
.turnstile-wrap { width: 100%; min-height: 65px; overflow: hidden; }
.turnstile-box { width: 100%; min-height: 65px; }
.submit { width: 100%; }
</style>
